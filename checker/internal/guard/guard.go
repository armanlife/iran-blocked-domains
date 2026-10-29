// Package guard pauses the checks while the firewall's connection tracking
// table is too full, so other services on the server keep working.
//
// Every check opens new connections, and each one stays in the Linux conntrack
// table for up to two minutes. When the table is full, the kernel drops new
// packets of every program, for example the calls of a VoIP server.
package guard

import (
	"context"
	"fmt"
	"iter"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	countFile = "/proc/sys/net/netfilter/nf_conntrack_count"
	maxFile   = "/proc/sys/net/netfilter/nf_conntrack_max"

	checkEvery = 100 // Domains between two reads of the table size
	pollEvery  = time.Second
)

// Usage returns the number of entries and the size of the conntrack table
type Usage func() (count, size int, ok bool)

// Conntrack reads the Linux conntrack table. ok is false when it is not available.
func Conntrack() (count, size int, ok bool) {
	count, errCount := readInt(countFile)
	size, errSize := readInt(maxFile)
	return count, size, errCount == nil && errSize == nil && size > 0
}

func readInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// Throttle passes on items, and waits while usage is above limitPercent.
// log is called when a pause starts. A limit of 0 disables it.
func Throttle[K, V any](ctx context.Context, items iter.Seq2[K, V], usage Usage, limitPercent int, log func(string)) iter.Seq2[K, V] {
	if limitPercent <= 0 {
		return items
	}
	if _, _, ok := usage(); !ok {
		return items
	}

	return func(yield func(K, V) bool) {
		i := 0
		for k, v := range items {
			if i%checkEvery == 0 && !wait(ctx, usage, limitPercent, log) {
				return
			}
			i++
			if !yield(k, v) {
				return
			}
		}
	}
}

// wait returns false if ctx is done while waiting
func wait(ctx context.Context, usage Usage, limitPercent int, log func(string)) bool {
	paused := false
	for {
		count, size, ok := usage()
		if !ok || count*100 < size*limitPercent {
			if paused {
				log(fmt.Sprintf("conntrack table at %d of %d, continuing", count, size))
			}
			return true
		}

		if !paused {
			paused = true
			log(fmt.Sprintf("conntrack table at %d of %d (over %d%%), pausing new checks", count, size, limitPercent))
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollEvery):
		}
	}
}
