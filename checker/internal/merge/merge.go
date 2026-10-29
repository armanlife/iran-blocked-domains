// Package merge builds one ranked list from a ranking, such as the host.io top
// 10 million, and a previous ranked list with domains the ranking misses.
//
// Domains of the ranking keep its order. A domain only in the previous list is
// placed right after its parent domain when the ranking has it, otherwise
// right after the nearest domain before it in the previous list that the
// ranking has. So the previous list keeps its own order around its domains:
// ytimg.com comes right after googleapis.com, not at the end.
package merge

import (
	"fmt"
	"os"
	"strings"

	"github.com/armanlife/iran-blocked-domains/checker/internal/domain"
)

// Adder receives the merged list in rank order, see store.Importer
type Adder interface {
	Add(domain, source string) error
}

type Stats struct {
	Ranking  int // Domains in the ranking
	Previous int // Domains in the previous list

	PreviousInRanking int // Already in the ranking, so placed by it
	AfterParent       int // Placed after their parent domain
	AfterNeighbor     int // Placed after the domain before them in the previous list
	AtTop             int // No domain before them is in the ranking
}

// Merge adds the merged list to add. Sources are named after the files.
// The ranking is read twice instead of being held in memory.
func Merge(rankingPath, previousPath string, add Adder, progress func(added int)) (Stats, error) {
	var stats Stats

	previous, err := readList(previousPath)
	if err != nil {
		return stats, err
	}
	stats.Previous = len(previous)

	// Which previous domains and parents of previous domains the ranking has
	inRanking := make(map[string]bool)
	for _, d := range previous {
		inRanking[d] = false
		for _, p := range parents(d) {
			inRanking[p] = false
		}
	}
	err = eachDomain(rankingPath, func(d string) error {
		stats.Ranking++
		if _, ok := inRanking[d]; ok {
			inRanking[d] = true
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	// Where each previous domain goes
	var top []string
	after := make(map[string][]string)
	anchor := ""
	for _, d := range previous {
		if inRanking[d] {
			stats.PreviousInRanking++
			anchor = d
			continue
		}
		if parent := rankedParent(d, inRanking); parent != "" {
			stats.AfterParent++
			after[parent] = append(after[parent], d)
			continue
		}
		if anchor == "" {
			stats.AtTop++
			top = append(top, d)
			continue
		}
		stats.AfterNeighbor++
		after[anchor] = append(after[anchor], d)
	}

	rankingSource, previousSource := sourceName(rankingPath), sourceName(previousPath)
	added := 0
	addOne := func(d, source string) error {
		if err := add.Add(d, source); err != nil {
			return err
		}
		if added++; progress != nil && added%500000 == 0 {
			progress(added)
		}
		return nil
	}

	for _, d := range top {
		if err := addOne(d, previousSource); err != nil {
			return stats, err
		}
	}
	err = eachDomain(rankingPath, func(d string) error {
		if err := addOne(d, rankingSource); err != nil {
			return err
		}
		for _, p := range after[d] {
			if err := addOne(p, previousSource); err != nil {
				return err
			}
		}
		return nil
	})
	return stats, err
}

// parents returns the parent domains of d, closest first: a.b.c.com gives b.c.com and c.com
func parents(d string) []string {
	var list []string
	for {
		_, rest, ok := strings.Cut(d, ".")
		if !ok || !strings.Contains(rest, ".") {
			return list
		}
		list = append(list, rest)
		d = rest
	}
}

func rankedParent(d string, inRanking map[string]bool) string {
	for _, p := range parents(d) {
		if inRanking[p] {
			return p
		}
	}
	return ""
}

// readList returns the domains of a list file without duplicates, in order
func readList(path string) ([]string, error) {
	seen := make(map[string]bool)
	var list []string
	err := eachDomain(path, func(d string) error {
		if !seen[d] {
			seen[d] = true
			list = append(list, d)
		}
		return nil
	})
	return list, err
}

func eachDomain(path string, fn func(string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := domain.NewReader(f)
	for d := range reader.All() {
		if err := fn(d); err != nil {
			return err
		}
	}
	if err := reader.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}

// sourceName names a source after its file: host10m.csv gives host10m
func sourceName(path string) string {
	name := path[strings.LastIndexAny(path, `/\`)+1:]
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	return name
}
