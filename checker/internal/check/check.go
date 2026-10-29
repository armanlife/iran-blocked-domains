// Package check detects domains blocked in Iran by DNS poisoning or SNI filtering.
package check

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"time"
)

type Status string

const (
	StatusBlocked    Status = "blocked"
	StatusNotBlocked Status = "not-blocked"
	StatusManual     Status = "manual" // Need to check manually
)

// Method is how a blocked domain is filtered
type Method string

const (
	MethodDNS Method = "dns"
	MethodSNI Method = "sni"
)

// Category is the detailed outcome of a check
type Category string

const (
	CategoryBlockedDNS    Category = "blocked-dns"
	CategoryBlockedSNI    Category = "blocked-sni"
	CategoryNotBlocked    Category = "not-blocked"
	CategoryDNSTimeout    Category = "dns-timeout"    // No answer: blackholed, or the domain is down
	CategoryDNSNotFound   Category = "dns-not-found"  // The domain does not exist or has no IPv4 address
	CategoryDNSError      Category = "dns-error"      // Other DNS errors
	CategoryConnectFailed Category = "connect-failed" // Cannot connect to the IP: IP may be blocked, or the server is down
	CategoryTLSFailed     Category = "tls-failed"     // TLS fails with every SNI: server error
)

// CategoryNetworkDown is returned when the network itself does not work, so
// the domain could not be checked. It is never saved: the domain must be
// checked again once the network is back.
const CategoryNetworkDown Category = "network-down"

// Categories lists every category in display order
var Categories = []Category{
	CategoryBlockedDNS, CategoryBlockedSNI, CategoryNotBlocked,
	CategoryDNSTimeout, CategoryDNSNotFound, CategoryDNSError, CategoryConnectFailed, CategoryTLSFailed,
}

// Status returns the status of results in category c
func (c Category) Status() Status {
	switch c {
	case CategoryBlockedDNS, CategoryBlockedSNI:
		return StatusBlocked
	case CategoryNotBlocked:
		return StatusNotBlocked
	default:
		return StatusManual
	}
}

// Method returns how domains of category c are blocked, if they are
func (c Category) Method() Method {
	switch c {
	case CategoryBlockedDNS:
		return MethodDNS
	case CategoryBlockedSNI:
		return MethodSNI
	default:
		return ""
	}
}

type Result struct {
	Domain   string   `json:"domain"`
	Status   Status   `json:"status"`
	Method   Method   `json:"method,omitempty"`
	Category Category `json:"category"`
	IPs      []string `json:"ips,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// FilteringIPs are returned by Iran's filtering system instead of the real IP
var FilteringIPs = map[string]bool{
	"10.10.34.34": true,
	"10.10.34.35": true,
	"10.10.34.36": true,
}

type Checker struct {
	// LookupA returns the IPv4 addresses of domain
	LookupA func(ctx context.Context, domain string) ([]string, error)
	// LookupATCP is tried when LookupA times out. Queries over TCP are not lost
	// like UDP packets under heavy load, and are poisoned the same way.
	LookupATCP func(ctx context.Context, domain string) ([]string, error)
	// Handshake performs a TLS handshake with ip:443 using sni
	Handshake func(ctx context.Context, ip, sni string) error
	// ControlSNI is an unblocked domain, used to tell SNI filtering apart from server errors
	ControlSNI string
	// Timeout of each DNS query and TLS handshake
	Timeout time.Duration
}

func New(dnsServer, controlSNI string, timeout time.Duration) *Checker {
	return &Checker{
		LookupA:    lookupA(dnsServer, ""),
		LookupATCP: lookupA(dnsServer, "tcp"),
		Handshake:  tlsHandshake,
		ControlSNI: controlSNI,
		Timeout:    timeout,
	}
}

// lookupA sends plain DNS queries directly to dnsServer, so the result does not
// depend on the system resolver (DoH, VPN, router, ...). An empty network uses
// UDP, falling back to TCP for truncated answers.
func lookupA(dnsServer, network string) func(ctx context.Context, domain string) ([]string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, defaultNetwork, _ string) (net.Conn, error) {
			dialNetwork := network
			if dialNetwork == "" {
				dialNetwork = defaultNetwork
			}
			var d net.Dialer
			return d.DialContext(ctx, dialNetwork, net.JoinHostPort(dnsServer, "53"))
		},
	}

	return func(ctx context.Context, domain string) ([]string, error) {
		ips, err := resolver.LookupIP(ctx, "ip4", domain)
		if err != nil {
			return nil, err
		}
		records := make([]string, 0, len(ips))
		for _, ip := range ips {
			records = append(records, ip.String())
		}
		return records, nil
	}
}

func tlsHandshake(ctx context.Context, ip, sni string) error {
	dialer := &tls.Dialer{Config: &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true, // Only the handshake matters, not the certificate
	}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return err
	}

	// Close with a reset instead of a normal close, so the connection leaves the
	// firewall's conntrack table after seconds instead of minutes (TIME_WAIT)
	if tcp, ok := conn.(*tls.Conn).NetConn().(*net.TCPConn); ok {
		tcp.SetLinger(0)
	}
	return conn.Close()
}

// Check detects whether domain is blocked by DNS poisoning or SNI filtering
func (c *Checker) Check(ctx context.Context, domain string) Result {
	// Step 1: DNS - the filtering system answers with one of FilteringIPs
	ips, err := c.lookup(ctx, c.LookupA, domain)
	if isTimeout(err) && c.LookupATCP != nil {
		ips, err = c.lookup(ctx, c.LookupATCP, domain)
	}
	if isTimeout(err) {
		if !c.Online(ctx) {
			return newResult(domain, nil, CategoryNetworkDown, "", "the DNS server does not answer for any domain")
		}
		// The filtering system may also drop queries of blocked domains (blackholing)
		return newResult(domain, nil, CategoryDNSTimeout, "", "no answer over UDP and TCP (blackholed, or the domain is down)")
	}
	if err != nil {
		category, reason := dnsError(err)
		return newResult(domain, nil, category, "", reason)
	}
	if len(ips) == 0 {
		return newResult(domain, nil, CategoryDNSNotFound, "", "no A record")
	}

	for _, ip := range ips {
		if FilteringIPs[ip] {
			return newResult(domain, ips, CategoryBlockedDNS, MethodDNS, "")
		}
	}

	// Step 2: SNI - the filtering system resets the TLS handshake of blocked domains
	ip := ips[0]
	err = c.handshake(ctx, ip, domain)
	if err == nil {
		return newResult(domain, ips, CategoryNotBlocked, "", "")
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return newResult(domain, ips, CategoryConnectFailed, "", "cannot connect to "+ip+":443 (IP may be blocked)")
	}

	// Same server accepts another SNI, so the domain name itself is blocked
	if c.handshake(ctx, ip, c.ControlSNI) == nil {
		return newResult(domain, ips, CategoryBlockedSNI, MethodSNI, "")
	}

	return newResult(domain, ips, CategoryTLSFailed, "", "TLS handshake failed: "+err.Error())
}

// Online reports whether the DNS server answers for ControlSNI, an unblocked
// domain, over UDP or TCP. If not, the network itself is down.
func (c *Checker) Online(ctx context.Context) bool {
	_, err := c.lookup(ctx, c.LookupA, c.ControlSNI)
	if isTimeout(err) && c.LookupATCP != nil {
		_, err = c.lookup(ctx, c.LookupATCP, c.ControlSNI)
	}
	return !isTimeout(err) && ctx.Err() == nil
}

func newResult(domain string, ips []string, category Category, method Method, reason string) Result {
	return Result{Domain: domain, Status: category.Status(), Method: method, Category: category, IPs: ips, Reason: reason}
}

func (c *Checker) lookup(ctx context.Context, lookup func(context.Context, string) ([]string, error), domain string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	return lookup(ctx, domain)
}

func (c *Checker) handshake(ctx context.Context, ip, sni string) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	return c.Handshake(ctx, ip, sni)
}

func isTimeout(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsTimeout
}

func dnsError(err error) (Category, string) {
	var dnsErr *net.DNSError
	switch {
	case !errors.As(err, &dnsErr):
		return CategoryDNSError, "DNS lookup failed: " + err.Error()
	case dnsErr.IsNotFound:
		return CategoryDNSNotFound, "domain not found"
	case dnsErr.IsTimeout:
		return CategoryDNSTimeout, "DNS timeout"
	default:
		return CategoryDNSError, "DNS lookup failed: " + dnsErr.Err
	}
}
