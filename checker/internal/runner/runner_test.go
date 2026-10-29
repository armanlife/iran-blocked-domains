package runner

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
)

func domainsN(n int) []string {
	domains := make([]string, n)
	for i := range domains {
		domains[i] = fmt.Sprintf("d%d.test", i)
	}
	return domains
}

func noop(int, check.Result) error { return nil }

func TestRunChecksEveryDomain(t *testing.T) {
	fn := func(_ context.Context, domain string) check.Result {
		return check.Result{Domain: domain, Category: check.CategoryNotBlocked}
	}

	seen := make(map[string]bool)
	summary, err := Run(context.Background(), fn, slices.All(domainsN(100)), Options{Workers: 10}, func(key int, r check.Result) error {
		if want := fmt.Sprintf("d%d.test", key); r.Domain != want {
			t.Errorf("key %d has domain %s, want %s", key, r.Domain, want)
		}
		seen[r.Domain] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 100 || len(seen) != 100 {
		t.Errorf("checked %d domains (%d unique), want 100", summary.Total, len(seen))
	}
}

func TestRunStopKeepsRunningChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fn := func(ctx context.Context, domain string) check.Result {
		time.Sleep(50 * time.Millisecond)
		if ctx.Err() != nil {
			t.Error("running check was canceled")
		}
		return check.Result{Domain: domain}
	}

	var keys []int
	time.AfterFunc(20*time.Millisecond, cancel)
	summary, err := Run(ctx, fn, slices.All(domainsN(1000)), Options{Rate: 100, Workers: 10}, func(key int, _ check.Result) error {
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Every started check is emitted, so the results have no gap
	slices.Sort(keys)
	for i, key := range keys {
		if key != i {
			t.Fatalf("results %v have a gap", keys)
		}
	}
	if summary.Total == 0 || summary.Total == 1000 {
		t.Errorf("checked %d domains, want some but not all", summary.Total)
	}
}

func TestRunWaitsForNetwork(t *testing.T) {
	onlineEvery = 20 * time.Millisecond
	t.Cleanup(func() { onlineEvery = 15 * time.Second })

	// The network is down for the first checks, and back after two probes
	var down atomic.Bool
	down.Store(true)
	var probes atomic.Int32
	online := func(context.Context) bool {
		if probes.Add(1) >= 2 {
			down.Store(false)
		}
		return !down.Load()
	}
	fn := func(_ context.Context, domain string) check.Result {
		if down.Load() {
			return check.Result{Domain: domain, Category: check.CategoryNetworkDown}
		}
		return check.Result{Domain: domain, Category: check.CategoryNotBlocked}
	}

	var mu sync.Mutex
	var events []bool
	opts := Options{Workers: 5, Online: online, OnOutage: func(d bool) {
		mu.Lock()
		events = append(events, d)
		mu.Unlock()
	}}
	summary, err := Run(context.Background(), fn, slices.All(domainsN(20)), opts, func(_ int, r check.Result) error {
		if r.Category == check.CategoryNetworkDown {
			t.Error("network-down result emitted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 20 {
		t.Errorf("checked %d domains, want 20", summary.Total)
	}
	if !slices.Equal(events, []bool{true, false}) {
		t.Errorf("outage events = %v, want down then back", events)
	}
}

func TestRunRespectsRate(t *testing.T) {
	fn := func(_ context.Context, domain string) check.Result { return check.Result{Domain: domain} }

	start := time.Now()
	// 11 domains at 50/s: 10 intervals of 20ms
	if _, err := Run(context.Background(), fn, slices.All(domainsN(11)), Options{Rate: 50, Workers: 10}, noop); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 180*time.Millisecond {
		t.Errorf("finished in %s, rate limit not applied", elapsed)
	}
}

func TestRunRespectsWorkers(t *testing.T) {
	var running, peak atomic.Int32
	fn := func(_ context.Context, domain string) check.Result {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		running.Add(-1)
		return check.Result{Domain: domain}
	}

	if _, err := Run(context.Background(), fn, slices.All(domainsN(50)), Options{Workers: 5}, noop); err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p > 5 {
		t.Errorf("%d concurrent checks, want at most 5", p)
	}
}

func TestSummaryRemove(t *testing.T) {
	s := NewSummary()
	s.Add(check.Result{Category: check.CategoryDNSTimeout})
	s.Remove(check.CategoryDNSTimeout)
	s.Add(check.Result{Category: check.CategoryBlockedSNI})
	if s.Total != 1 || s.Manual != 0 || s.Blocked != 1 || s.ByCategory[check.CategoryDNSTimeout] != 0 {
		t.Errorf("summary = %+v", s)
	}
}
