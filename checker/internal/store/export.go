package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
)

const (
	StatusFile  = "run.json"
	BlockedFile = "blocked.txt"
)

// LocalFiles are not published: the database is large and binary, and the
// lists of unblocked domains and of failed connections are the largest ones
var LocalFiles = []string{
	DBFile, DBFile + "-*",
	string(check.CategoryNotBlocked) + ".txt",
	string(check.CategoryConnectFailed) + ".txt",
}

type State string

const (
	StateRunning   State = "running"
	StateStopped   State = "stopped"
	StateCompleted State = "completed"
)

// Status is saved as run.json, for people and for scripts like deploy/service.sh
type Status struct {
	List       string                 `json:"list"`
	Total      int                    `json:"total"`
	Checked    int                    `json:"checked"`
	State      State                  `json:"state"`
	DNS        string                 `json:"dns"`
	ControlSNI string                 `json:"control_sni"`
	Timeout    string                 `json:"timeout"`
	Rate       int                    `json:"rate"`
	Started    time.Time              `json:"started"`
	Updated    time.Time              `json:"updated"`
	Counts     map[check.Category]int `json:"counts"`
	Recheck    *RecheckStatus         `json:"recheck,omitempty"`
}

type RecheckStatus struct {
	Categories []check.Category `json:"categories"`
	Since      time.Time        `json:"since"`
	Selected   int              `json:"selected"`
	Rechecked  int              `json:"rechecked"`
}

// ReadStatus reads run.json of the run folder dir
func ReadStatus(dir string) (Status, error) {
	var status Status
	data, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		return status, err
	}
	return status, json.Unmarshal(data, &status)
}

// WriteStatus writes run.json
func (s *Store) WriteStatus(status Status) error {
	status.Updated = time.Now()
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, StatusFile), func(w *bufio.Writer) error {
		_, err := w.Write(append(data, '\n'))
		return err
	})
}

// Export writes run.json, .gitignore and one list per category, plus
// blocked.txt with every blocked domain. Every list is in rank order.
// Without full, not-blocked.txt is skipped: it is the largest list.
func (s *Store) Export(ctx context.Context, status Status, full bool) error {
	for _, c := range check.Categories {
		if c == check.CategoryNotBlocked && !full {
			continue
		}
		if err := s.exportList(ctx, string(c)+".txt", `WHERE r.category = ?`, string(c)); err != nil {
			return err
		}
	}

	err := s.exportList(ctx, BlockedFile, `WHERE r.category IN (?, ?)`,
		string(check.CategoryBlockedDNS), string(check.CategoryBlockedSNI))
	if err != nil {
		return err
	}

	if err := s.writeGitignore(); err != nil {
		return err
	}
	return s.WriteStatus(status)
}

func (s *Store) exportList(ctx context.Context, name, where string, args ...any) error {
	rows, err := s.db.QueryContext(ctx, `SELECT d.domain FROM results r JOIN domains d ON d.rank = r.rank `+
		where+` ORDER BY r.rank`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	return writeAtomic(filepath.Join(s.Dir, name), func(w *bufio.Writer) error {
		var domain string
		for rows.Next() {
			if err := rows.Scan(&domain); err != nil {
				return err
			}
			w.WriteString(domain)
			w.WriteByte('\n')
		}
		return rows.Err()
	})
}

func (s *Store) writeGitignore() error {
	return writeAtomic(filepath.Join(s.Dir, ".gitignore"), func(w *bufio.Writer) error {
		w.WriteString("# Written by checker: local files that are too large to publish\n")
		for _, name := range LocalFiles {
			w.WriteString(name + "\n")
		}
		return nil
	})
}

// writeAtomic writes a file through a temporary file, so readers never see
// a half written file
func writeAtomic(path string, write func(*bufio.Writer) error) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	w := bufio.NewWriterSize(f, 256<<10)
	err = write(w)
	if err == nil {
		err = w.Flush()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
