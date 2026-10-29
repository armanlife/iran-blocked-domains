// Package store keeps a ranked domain list and the check results of its domains
// in an SQLite database, and exports them as plain text lists in rank order.
//
// The database is the source of truth: a check can stop at any time and continue
// with the domains that have no result yet, and the domains of some categories
// can be checked again, keeping their rank.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/runner"
)

// DBFile is the name of the database in a run folder
const DBFile = "checker.db"

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- The ranked list: rank 1 is the most important domain. Its unique index on
-- domain is built by Importer, after the domains are loaded.
CREATE TABLE IF NOT EXISTS domains (
	rank   INTEGER PRIMARY KEY,
	domain TEXT NOT NULL,
	source TEXT NOT NULL
);

-- The latest result of each checked domain
CREATE TABLE IF NOT EXISTS results (
	rank       INTEGER PRIMARY KEY,
	category   TEXT NOT NULL,
	ips        TEXT NOT NULL DEFAULT '',
	reason     TEXT NOT NULL DEFAULT '',
	checked_at INTEGER NOT NULL -- Unix milliseconds
);
-- Covers the selection of a recheck, so it reads no table rows
DROP INDEX IF EXISTS results_category;
CREATE INDEX IF NOT EXISTS results_recheck ON results (category, rank, checked_at);

-- Previous results of domains that were checked again
CREATE TABLE IF NOT EXISTS history (
	rank       INTEGER NOT NULL,
	category   TEXT NOT NULL,
	checked_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS history_rank ON history (rank);
`

type Store struct {
	Dir string
	db  *sql.DB
}

// Exists reports whether dir has a database
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, DBFile))
	return err == nil
}

// Open opens the database of the run folder dir, creating both if needed
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filepath.Join(dir, DBFile))
	if err != nil {
		return nil, err
	}

	// "file:C:/dir/checker.db" on Windows and "file:/dir/checker.db" elsewhere
	path := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23", " ", "%20").Replace(filepath.ToSlash(abs))
	dsn := "file:" + path +
		"?_pragma=journal_mode(wal)&_pragma=synchronous(normal)&_pragma=busy_timeout(10000)&_txlock=immediate"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s: %w", abs, err)
	}
	return &Store{Dir: dir, db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DomainCount returns the number of domains in the ranked list
func (s *Store) DomainCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&n)
	return n, err
}

// Counts returns the number of results of each category
func (s *Store) Counts() (*runner.Summary, error) {
	rows, err := s.db.Query(`SELECT category, COUNT(*) FROM results GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := runner.NewSummary()
	for rows.Next() {
		var category string
		var n int
		if err := rows.Scan(&category, &n); err != nil {
			return nil, err
		}
		summary.AddCount(check.Category(category), n)
	}
	return summary, rows.Err()
}

// Meta returns a value saved with SetMeta, or "" if there is none
func (s *Store) Meta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Settings are the check settings of a run. They are saved at the start, so
// all the results of a run are made the same way.
type Settings struct {
	List       string // Description of where the domains come from
	DNS        string
	ControlSNI string
	Timeout    time.Duration
	Started    time.Time
}

func (s *Store) Settings() (Settings, error) {
	var settings Settings
	values := map[string]*string{"list": &settings.List, "dns": &settings.DNS, "control_sni": &settings.ControlSNI}
	for key, value := range values {
		v, err := s.Meta(key)
		if err != nil {
			return settings, err
		}
		*value = v
	}

	timeout, err := s.Meta("timeout")
	if err != nil {
		return settings, err
	}
	if timeout != "" {
		if settings.Timeout, err = time.ParseDuration(timeout); err != nil {
			return settings, err
		}
	}

	started, err := s.Meta("started")
	if err != nil {
		return settings, err
	}
	if started != "" {
		if settings.Started, err = time.Parse(time.RFC3339, started); err != nil {
			return settings, err
		}
	}
	return settings, nil
}

func (s *Store) SaveSettings(settings Settings) error {
	values := map[string]string{
		"list":        settings.List,
		"dns":         settings.DNS,
		"control_sni": settings.ControlSNI,
		"timeout":     settings.Timeout.String(),
		"started":     settings.Started.Format(time.RFC3339),
	}
	for key, value := range values {
		if err := s.SetMeta(key, value); err != nil {
			return err
		}
	}
	return nil
}

// Recheck is a check again of the domains of some categories
type Recheck struct {
	Categories []check.Category
	// Since: only results older than this are checked again, so a recheck
	// that is stopped continues with the domains it did not check yet
	Since time.Time
}

// CurrentRecheck returns the unfinished recheck, or nil
func (s *Store) CurrentRecheck() (*Recheck, error) {
	categories, err := s.Meta("recheck_categories")
	if err != nil || categories == "" {
		return nil, err
	}
	since, err := s.Meta("recheck_since")
	if err != nil {
		return nil, err
	}

	ms, err := parseInt64(since)
	if err != nil {
		return nil, err
	}
	r := &Recheck{Since: time.UnixMilli(ms)}
	for _, c := range strings.Split(categories, ",") {
		r.Categories = append(r.Categories, check.Category(c))
	}
	return r, nil
}

// SetRecheck saves r as the unfinished recheck, or clears it if r is nil
func (s *Store) SetRecheck(r *Recheck) error {
	categories, since := "", ""
	if r != nil {
		names := make([]string, len(r.Categories))
		for i, c := range r.Categories {
			names[i] = string(c)
		}
		categories, since = strings.Join(names, ","), fmt.Sprint(r.Since.UnixMilli())
	}
	if err := s.SetMeta("recheck_categories", categories); err != nil {
		return err
	}
	return s.SetMeta("recheck_since", since)
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}

// Found is what the database knows about one domain
type Found struct {
	Rank    int
	Domain  string
	Source  string
	Result  *check.Result // Nil if not checked yet
	Checked time.Time
	History []Past
}

type Past struct {
	Category check.Category
	Checked  time.Time
}

// Find returns the rank, result and history of domain, or nil if it is not in the list
func (s *Store) Find(ctx context.Context, domain string) (*Found, error) {
	f := &Found{Domain: domain}
	err := s.db.QueryRowContext(ctx, `SELECT rank, source FROM domains WHERE domain = ?`, domain).Scan(&f.Rank, &f.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var category, ips, reason string
	var checked int64
	err = s.db.QueryRowContext(ctx, `SELECT category, ips, reason, checked_at FROM results WHERE rank = ?`, f.Rank).
		Scan(&category, &ips, &reason, &checked)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return f, nil
	case err != nil:
		return nil, err
	}
	c := check.Category(category)
	f.Result = &check.Result{Domain: domain, Category: c, Status: c.Status(), Method: c.Method(), IPs: strings.Fields(ips), Reason: reason}
	f.Checked = time.UnixMilli(checked)

	rows, err := s.db.QueryContext(ctx, `SELECT category, checked_at FROM history WHERE rank = ? ORDER BY checked_at`, f.Rank)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var past Past
		if err := rows.Scan(&category, &checked); err != nil {
			return nil, err
		}
		past.Category, past.Checked = check.Category(category), time.UnixMilli(checked)
		f.History = append(f.History, past)
	}
	return f, rows.Err()
}
