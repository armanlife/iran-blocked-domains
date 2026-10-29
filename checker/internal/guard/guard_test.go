package guard

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"testing"
	"time"
)

func domainsN(n int) []string {
	domains := make([]string, n)
	for i := range domains {
		domains[i] = fmt.Sprintf("d%d.test", i)
	}
	return domains
}

func collect(items iter.Seq2[int, string]) []string {
	var domains []string
	for _, domain := range items {
		domains = append(domains, domain)
	}
	return domains
}

func TestThrottlePausesWhileFull(t *testing.T) {
	// The table is full for the first reads, then empties
	reads := 0
	usage := func() (int, int, bool) {
		reads++
		if reads <= 3 {
			return 900, 1000, true
		}
		return 100, 1000, true
	}

	var logs []string
	start := time.Now()
	got := collect(Throttle(context.Background(), slices.All(domainsN(250)), usage, 50, func(s string) { logs = append(logs, s) }))

	if len(got) != 250 {
		t.Errorf("passed %d domains, want 250", len(got))
	}
	if elapsed := time.Since(start); elapsed < pollEvery {
		t.Errorf("did not pause, finished in %s", elapsed)
	}
	if len(logs) != 2 {
		t.Errorf("logs = %v, want a pause and a continue", logs)
	}
}

func TestThrottleStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	full := func() (int, int, bool) { return 1000, 1000, true }
	got := collect(Throttle(ctx, slices.All(domainsN(10)), full, 50, func(string) {}))
	if len(got) != 0 {
		t.Errorf("passed %d domains while the table was full", len(got))
	}
}

func TestThrottleWithoutConntrack(t *testing.T) {
	missing := func() (int, int, bool) { return 0, 0, false }
	got := collect(Throttle(context.Background(), slices.All(domainsN(10)), missing, 50, nil))
	if len(got) != 10 {
		t.Errorf("passed %d domains, want 10", len(got))
	}
}
