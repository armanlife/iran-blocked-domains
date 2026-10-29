package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/runner"
)

const (
	writeBatch = 500
	writeEvery = time.Second
)

// Writer saves results in batches. A result replaces the previous result of
// its domain, which is kept in the history.
type Writer struct {
	s         *Store
	mu        sync.Mutex
	summary   *runner.Summary
	pending   []pendingResult
	lastWrite time.Time
}

type pendingResult struct {
	rank int
	r    check.Result
	at   int64
}

func (s *Store) NewWriter() (*Writer, error) {
	summary, err := s.Counts()
	if err != nil {
		return nil, err
	}
	return &Writer{s: s, summary: summary, lastWrite: time.Now()}, nil
}

// Add saves the result of the domain at rank. Results are written at least
// every second; the last ones are written by Flush.
func (w *Writer) Add(rank int, r check.Result) error {
	w.pending = append(w.pending, pendingResult{rank: rank, r: r, at: time.Now().UnixMilli()})
	if len(w.pending) >= writeBatch || time.Since(w.lastWrite) >= writeEvery {
		return w.Flush()
	}
	return nil
}

// Flush writes the added results
func (w *Writer) Flush() error {
	w.lastWrite = time.Now()
	if len(w.pending) == 0 {
		return nil
	}

	tx, err := w.s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	previous, err := tx.Prepare(`SELECT category, checked_at FROM results WHERE rank = ?`)
	if err != nil {
		return err
	}
	defer previous.Close()
	history, err := tx.Prepare(`INSERT INTO history (rank, category, checked_at) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer history.Close()
	upsert, err := tx.Prepare(`INSERT INTO results (rank, category, ips, reason, checked_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (rank) DO UPDATE SET
			category = excluded.category, ips = excluded.ips, reason = excluded.reason, checked_at = excluded.checked_at`)
	if err != nil {
		return err
	}
	defer upsert.Close()

	// Applied to the summary once the transaction is saved
	var replaced []check.Category
	for _, p := range w.pending {
		var category string
		var checked int64
		err := previous.QueryRow(p.rank).Scan(&category, &checked)
		switch {
		case err == nil:
			if _, err := history.Exec(p.rank, category, checked); err != nil {
				return err
			}
			replaced = append(replaced, check.Category(category))
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		if _, err := upsert.Exec(p.rank, string(p.r.Category), strings.Join(p.r.IPs, " "), p.r.Reason, p.at); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	w.mu.Lock()
	for _, c := range replaced {
		w.summary.Remove(c)
	}
	for _, p := range w.pending {
		w.summary.Add(p.r)
	}
	w.mu.Unlock()

	w.pending = w.pending[:0]
	return nil
}

// Summary returns the counts of all saved results, including previous runs
func (w *Writer) Summary() *runner.Summary {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.summary.Clone()
}
