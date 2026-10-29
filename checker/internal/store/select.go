package store

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
)

const pageSize = 1000

// Selection is a set of domains to check, iterated by rank
type Selection struct {
	s     *Store
	query string // Selects rank and domain, after the rank given as the last argument
	count string
	args  []any
	err   error
}

// Pending selects the domains that have no result yet
func (s *Store) Pending() *Selection {
	return &Selection{
		s: s,
		query: `SELECT d.rank, d.domain FROM domains d
			WHERE NOT EXISTS (SELECT 1 FROM results r WHERE r.rank = d.rank) AND d.rank > ?
			ORDER BY d.rank LIMIT ?`,
		count: `SELECT (SELECT COUNT(*) FROM domains) - (SELECT COUNT(*) FROM results)`,
	}
}

// Recheck selects the domains of r's categories whose result is older than r.Since
func (s *Store) Recheck(r Recheck) *Selection {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(r.Categories)), ",")
	args := make([]any, 0, len(r.Categories)+1)
	for _, c := range r.Categories {
		args = append(args, string(c))
	}
	args = append(args, r.Since.UnixMilli())

	// One page of each category in rank order, merged. With "category IN" the
	// database would sort all the remaining results of the categories for
	// every page. The numbered parameters are the categories, then since,
	// after and the page size.
	n := len(r.Categories)
	pages := make([]string, n)
	for i := range n {
		pages[i] = fmt.Sprintf(`SELECT rank FROM (SELECT rank FROM results
			WHERE category = ?%d AND rank > ?%d AND checked_at < ?%d ORDER BY rank LIMIT ?%d)`, i+1, n+2, n+1, n+3)
	}

	return &Selection{
		s: s,
		query: `SELECT r.rank, d.domain FROM (` + strings.Join(pages, " UNION ALL ") + `) r
			JOIN domains d ON d.rank = r.rank ORDER BY r.rank LIMIT ?` + fmt.Sprint(n+3),
		count: `SELECT COUNT(*) FROM results WHERE category IN (` + placeholders + `) AND checked_at < ?`,
		args:  args,
	}
}

// Count returns the number of selected domains
func (sel *Selection) Count(ctx context.Context) (int, error) {
	var n int
	err := sel.s.db.QueryRowContext(ctx, sel.count, sel.args...).Scan(&n)
	return n, err
}

// All iterates over the selected domains by rank. It reads them a page at a
// time, so no query stays open while the results are written. Check Err after.
func (sel *Selection) All(ctx context.Context) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		last := 0
		for {
			page, err := sel.page(ctx, last)
			if err != nil {
				sel.err = err
				return
			}
			if len(page) == 0 {
				return
			}
			for _, item := range page {
				if !yield(item.rank, item.domain) {
					return
				}
				last = item.rank
			}
		}
	}
}

func (sel *Selection) Err() error {
	return sel.err
}

type item struct {
	rank   int
	domain string
}

func (sel *Selection) page(ctx context.Context, after int) ([]item, error) {
	args := append(append([]any{}, sel.args...), after, pageSize)
	rows, err := sel.s.db.QueryContext(ctx, sel.query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	page := make([]item, 0, pageSize)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.rank, &it.domain); err != nil {
			return nil, err
		}
		page = append(page, it)
	}
	return page, rows.Err()
}

// ParseCategories parses a comma separated list of category names
func ParseCategories(s string) ([]check.Category, error) {
	known := make(map[check.Category]bool)
	for _, c := range check.Categories {
		known[c] = true
	}

	var categories []check.Category
	for _, name := range strings.Split(s, ",") {
		c := check.Category(strings.TrimSpace(name))
		if c == "" {
			continue
		}
		if !known[c] {
			return nil, &UnknownCategoryError{Name: string(c)}
		}
		categories = append(categories, c)
	}
	if len(categories) == 0 {
		return nil, &UnknownCategoryError{}
	}
	return categories, nil
}

type UnknownCategoryError struct {
	Name string
}

func (e *UnknownCategoryError) Error() string {
	names := make([]string, len(check.Categories))
	for i, c := range check.Categories {
		names[i] = string(c)
	}
	if e.Name == "" {
		return "no category given, use: " + strings.Join(names, ", ")
	}
	return "unknown category " + e.Name + ", use: " + strings.Join(names, ", ")
}
