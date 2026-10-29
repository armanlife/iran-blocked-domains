package check

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

var (
	errReset   = errors.New("connection reset by peer")
	errTimeout = &net.DNSError{Err: "i/o timeout", IsTimeout: true}
)

// fakeChecker answers ips for every domain but the control domain, which
// answers unless the network is offline
func fakeChecker(ips []string, udpErr, tcpErr error, handshake map[string]error, offline bool) *Checker {
	control := func(domain string) ([]string, error, bool) {
		switch {
		case domain != "example.com":
			return nil, nil, false
		case offline:
			return nil, errTimeout, true
		default:
			return []string{"93.184.216.34"}, nil, true
		}
	}

	return &Checker{
		LookupA: func(_ context.Context, domain string) ([]string, error) {
			if ips, err, ok := control(domain); ok {
				return ips, err
			}
			if udpErr != nil {
				return nil, udpErr
			}
			return ips, nil
		},
		// Answers when the UDP lookup timed out
		LookupATCP: func(_ context.Context, domain string) ([]string, error) {
			if ips, err, ok := control(domain); ok {
				return ips, err
			}
			if tcpErr != nil {
				return nil, tcpErr
			}
			return ips, nil
		},
		Handshake: func(_ context.Context, _, sni string) error {
			return handshake[sni]
		},
		ControlSNI: "example.com",
		Timeout:    time.Second,
	}
}

func TestCheck(t *testing.T) {
	dialErr := &net.OpError{Op: "dial", Err: errors.New("i/o timeout")}

	tests := []struct {
		name      string
		ips       []string
		udpErr    error
		tcpErr    error
		handshake map[string]error
		offline   bool
		want      Category
		method    Method
	}{
		{
			name:   "dns poisoned",
			ips:    []string{"10.10.34.36"},
			want:   CategoryBlockedDNS,
			method: MethodDNS,
		},
		{
			name: "not blocked",
			ips:  []string{"93.184.216.34"},
			want: CategoryNotBlocked,
		},
		{
			name:      "sni filtered",
			ips:       []string{"104.18.32.47"},
			handshake: map[string]error{"blocked.test": errReset},
			want:      CategoryBlockedSNI,
			method:    MethodSNI,
		},
		{
			name:      "server rejects every sni",
			ips:       []string{"104.18.32.47"},
			handshake: map[string]error{"blocked.test": errReset, "example.com": errReset},
			want:      CategoryTLSFailed,
		},
		{
			name:      "cannot connect",
			ips:       []string{"149.154.167.99"},
			handshake: map[string]error{"blocked.test": dialErr},
			want:      CategoryConnectFailed,
		},
		{
			name:   "domain not found",
			udpErr: &net.DNSError{Err: "no such host", IsNotFound: true},
			want:   CategoryDNSNotFound,
		},
		{
			name:   "server failure",
			udpErr: &net.DNSError{Err: "server misbehaving", IsTemporary: true},
			want:   CategoryDNSError,
		},
		{
			name: "no A record",
			want: CategoryDNSNotFound,
		},
		{
			name:   "udp query lost, tcp query poisoned",
			ips:    []string{"10.10.34.36"},
			udpErr: errTimeout,
			want:   CategoryBlockedDNS,
			method: MethodDNS,
		},
		{
			name:   "no answer over udp and tcp",
			udpErr: errTimeout,
			tcpErr: errTimeout,
			want:   CategoryDNSTimeout,
		},
		{
			name:    "network down",
			udpErr:  errTimeout,
			tcpErr:  errTimeout,
			offline: true,
			want:    CategoryNetworkDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakeChecker(tt.ips, tt.udpErr, tt.tcpErr, tt.handshake, tt.offline)
			got := c.Check(context.Background(), "blocked.test")
			if got.Category != tt.want || got.Method != tt.method || got.Status != tt.want.Status() {
				t.Errorf("got %s/%s/%s, want %s/%s (reason: %s)",
					got.Category, got.Method, got.Status, tt.want, tt.method, got.Reason)
			}
		})
	}
}
