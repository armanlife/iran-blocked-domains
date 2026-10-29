// Package runner checks many domains concurrently with a rate limit.
package runner

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"sync"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
)

type Summary struct {
	Total, Blocked, NotBlocked, Manual int
	ByCategory                         map[check.Category]int
}

func NewSummary() *Summary {
	return &Summary{ByCategory: make(map[check.Category]int)}
}

func (s *Summary) Add(r check.Result) {
	s.AddCount(r.Category, 1)
}

// AddCount counts n results of category c
func (s *Summary) AddCount(c check.Category, n int) {
	s.Total += n
	s.ByCategory[c] += n
	switch c.Status() {
	case check.StatusBlocked:
		s.Blocked += n
	case check.StatusNotBlocked:
		s.NotBlocked += n
	default:
		s.Manual += n
	}
}

// Remove uncounts a result of category c, replaced by a newer one
func (s *Summary) Remove(c check.Category) {
	s.AddCount(c, -1)
}

func (s *Summary) Clone() *Summary {
	clone := *s
	clone.ByCategory = maps.Clone(s.ByCategory)
	return &clone
}

func (s *Summary) String() string {
	return fmt.Sprintf("%d checked: %d blocked (dns: %d, sni: %d), %d not-blocked, %d manual",
		s.Total, s.Blocked, s.ByCategory[check.CategoryBlockedDNS], s.ByCategory[check.CategoryBlockedSNI], s.NotBlocked, s.Manual)
}

type CheckFunc func(ctx context.Context, domain string) check.Result

// EmitFunc receives each result with the key of its domain, such as its rank
type EmitFunc func(key int, r check.Result) error

type Options struct {
	Rate    int // Max checks started per second, 0 = unlimited
	Workers int // Max concurrent checks

	// Online reports whether the network works. When a check returns
	// check.CategoryNetworkDown, the checks wait until Online is true and
	// are done again. Without Online, such results are emitted as they are.
	Online func(ctx context.Context) bool
	// OnOutage is called when the network goes down and when it is back
	OnOutage func(down bool)
}

// onlineEvery is how often the network is checked during an outage
var onlineEvery = 15 * time.Second

type job struct {
	key    int
	domain string
}

type result struct {
	key int
	r   check.Result
}

// Run checks the domains of items, keyed for example by rank. Results are
// passed to emit as they finish, which is not the order of items. Items are
// consumed as they are checked, so a list is never held in memory.
//
// When ctx is canceled no new check starts, and the running checks finish so
// their results are not lost. Checks waiting for the network are dropped.
func Run(ctx context.Context, fn CheckFunc, items iter.Seq2[int, string], opts Options, emit EmitFunc) (*Summary, error) {
	checkCtx := context.WithoutCancel(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan job)
	results := make(chan result)
	outage := &outage{online: opts.Online, notify: opts.OnOutage}

	go func() {
		defer close(jobs)

		var tick <-chan time.Time
		if opts.Rate > 0 {
			ticker := time.NewTicker(time.Second / time.Duration(opts.Rate))
			defer ticker.Stop()
			tick = ticker.C
		}

		first := true
		for key, domain := range items {
			if tick != nil && !first {
				select {
				case <-ctx.Done():
					return
				case <-tick:
				}
			}
			first = false

			select {
			case <-ctx.Done():
				return
			case jobs <- job{key: key, domain: domain}:
			}
		}
	}()

	var wg sync.WaitGroup
	for range max(opts.Workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r := fn(checkCtx, j.domain)
				for r.Category == check.CategoryNetworkDown && opts.Online != nil {
					if !outage.wait(ctx) {
						break
					}
					r = fn(checkCtx, j.domain)
				}
				if r.Category == check.CategoryNetworkDown && opts.Online != nil {
					continue // Stopped while waiting, the domain stays unchecked
				}
				results <- result{key: j.key, r: r}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	summary := NewSummary()
	var err error
	for res := range results {
		summary.Add(res.r)
		if err == nil {
			if err = emit(res.key, res.r); err != nil {
				cancel()
			}
		}
	}
	return summary, err
}

// outage lets the checks wait together for the network to come back, with a
// single goroutine checking the network
type outage struct {
	online func(ctx context.Context) bool
	notify func(down bool)

	mu   sync.Mutex
	back chan struct{} // Closed when the network is back, nil while it works
}

// wait returns when the network is back, or false if ctx is done first
func (o *outage) wait(ctx context.Context) bool {
	o.mu.Lock()
	back := o.back
	if back == nil {
		back = make(chan struct{})
		o.back = back
		if o.notify != nil {
			o.notify(true)
		}
		go o.watch(ctx, back)
	}
	o.mu.Unlock()

	select {
	case <-back:
		return true
	case <-ctx.Done():
		return false
	}
}

func (o *outage) watch(ctx context.Context, back chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(onlineEvery):
		}
		if o.online(ctx) {
			break
		}
	}

	o.mu.Lock()
	o.back = nil
	close(back)
	o.mu.Unlock()
	if o.notify != nil {
		o.notify(false)
	}
}

// WorkersFor returns enough concurrent checks to keep up with rate,
// since a single check may take a few seconds
func WorkersFor(rate int) int {
	if rate <= 0 {
		return 500
	}
	return min(max(rate*5, 20), 2000)
}
