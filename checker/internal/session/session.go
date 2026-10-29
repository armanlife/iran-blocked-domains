// Package session runs a check of a run folder: it imports the list of a new
// folder, selects the domains to check, saves the results and keeps the text
// lists and run.json up to date. Both the command line and the interactive
// mode use it.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/domain"
	"github.com/armanlife/iran-blocked-domains/checker/internal/guard"
	"github.com/armanlife/iran-blocked-domains/checker/internal/runner"
	"github.com/armanlife/iran-blocked-domains/checker/internal/store"
)

const (
	statusEvery = time.Minute
	// Above this many domains the text lists are updated less often, and
	// not-blocked.txt, the largest one, only at the end
	bigList = 1_000_000
)

type Options struct {
	Dir      string
	Input    string         // List imported into a new run folder
	Defaults store.Settings // DNS, ControlSNI and Timeout of a new run folder

	Recheck []check.Category // Check again the domains of these categories
	Since   time.Time        // Recheck results older than this; zero continues the unfinished recheck, or starts now

	Rate int
	Log  func(string)
}

type Session struct {
	Store     *store.Store
	Writer    *store.Writer
	Settings  store.Settings
	Recheck   *store.Recheck // Nil for a normal check
	Total     int            // Domains in the list
	Todo      int            // Domains selected at the start
	Rate      int
	selection *store.Selection
	done      atomic.Int64
	finished  bool // The whole selection is checked
}

// Prepare opens the run folder, imports the list of a new folder and selects
// the domains to check
func Prepare(ctx context.Context, opts Options) (*Session, error) {
	log := opts.Log
	if log == nil {
		log = func(string) {}
	}

	s, err := store.Open(opts.Dir)
	if err != nil {
		return nil, err
	}
	session := &Session{Store: s, Rate: opts.Rate}
	if err := session.prepare(ctx, opts, log); err != nil {
		s.Close()
		return nil, err
	}
	return session, nil
}

func (ss *Session) prepare(ctx context.Context, opts Options, log func(string)) error {
	s := ss.Store
	total, err := s.DomainCount()
	if err != nil {
		return err
	}

	if total == 0 {
		if opts.Input == "" {
			return fmt.Errorf("%s has no domains yet: give a list, or build one with \"checker merge\"", opts.Dir)
		}
		if total, err = Import(s, opts.Input, log); err != nil {
			return err
		}
	} else if opts.Input != "" {
		log(fmt.Sprintf("%s already has its list of %d domains, it is used instead of %s", opts.Dir, total, opts.Input))
	}
	ss.Total = total

	// A run keeps its first settings, so all its results are comparable
	if ss.Settings, err = s.Settings(); err != nil {
		return err
	}
	if ss.Settings.DNS == "" {
		list := ss.Settings.List
		ss.Settings = opts.Defaults
		ss.Settings.List, ss.Settings.Started = list, time.Now()
		if err := s.SaveSettings(ss.Settings); err != nil {
			return err
		}
	}

	if len(opts.Recheck) > 0 {
		if ss.Recheck, err = resolveRecheck(s, opts.Recheck, opts.Since); err != nil {
			return err
		}
		if err := s.SetRecheck(ss.Recheck); err != nil {
			return err
		}
		ss.selection = s.Recheck(*ss.Recheck)
	} else {
		ss.selection = s.Pending()
	}

	if ss.Todo, err = ss.selection.Count(ctx); err != nil {
		return err
	}
	ss.Writer, err = s.NewWriter()
	return err
}

// resolveRecheck continues the unfinished recheck of the same categories, or starts a new one
func resolveRecheck(s *store.Store, categories []check.Category, since time.Time) (*store.Recheck, error) {
	if !since.IsZero() {
		return &store.Recheck{Categories: categories, Since: since}, nil
	}
	current, err := s.CurrentRecheck()
	if err != nil {
		return nil, err
	}
	if current != nil && sameCategories(current.Categories, categories) {
		return current, nil
	}
	return &store.Recheck{Categories: categories, Since: time.Now()}, nil
}

func sameCategories(a, b []check.Category) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Import adds the domains of a list file to a new run folder, in their order
func Import(s *store.Store, path string, log func(string)) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	im, err := s.NewImporter()
	if err != nil {
		return 0, err
	}
	name := ListName(path)
	reader := domain.NewReader(f)
	log("importing " + filepath.Base(path))
	for d := range reader.All() {
		if err := im.Add(d, name); err != nil {
			return 0, err
		}
		if n := im.Count(); n%1_000_000 == 0 {
			log(fmt.Sprintf("  %d domains imported", n))
		}
	}
	if err := reader.Err(); err != nil {
		return 0, err
	}
	log("building the index")
	if err := im.Close(); err != nil {
		return 0, err
	}

	log(fmt.Sprintf("imported %d domains (%d duplicates and %d invalid lines skipped)", im.Added, im.Duplicates, reader.Invalid))
	if err := s.SetMeta("list", name); err != nil {
		return 0, err
	}
	return im.Added, nil
}

// ListName names a list after its file: host10m.csv gives host10m
func ListName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

type RunConfig struct {
	Workers    int
	GuardLimit int                // See package guard
	OnResult   func(check.Result) // Called for each saved result
	OnOutage   func(down bool)
	Log        func(string)
}

// Run checks the selected domains until all are checked or ctx is canceled.
// stopped reports whether ctx was canceled before the end.
func (ss *Session) Run(ctx context.Context, cfg RunConfig) (stopped bool, err error) {
	log := cfg.Log
	if log == nil {
		log = func(string) {}
	}

	exportEvery := time.Minute
	if ss.Total >= bigList {
		exportEvery = 10 * time.Minute
	}

	checker := check.New(ss.Settings.DNS, ss.Settings.ControlSNI, ss.Settings.Timeout)
	items := guard.Throttle(ctx, ss.selection.All(ctx), guard.Conntrack, cfg.GuardLimit, log)
	opts := runner.Options{Rate: ss.Rate, Workers: cfg.Workers, Online: checker.Online, OnOutage: cfg.OnOutage}

	if err := ss.Store.WriteStatus(ss.Status(store.StateRunning)); err != nil {
		return false, err
	}
	lastStatus, lastExport := time.Now(), time.Now()

	_, err = runner.Run(ctx, checker.Check, items, opts, func(rank int, r check.Result) error {
		if err := ss.Writer.Add(rank, r); err != nil {
			return err
		}
		ss.done.Add(1)
		if cfg.OnResult != nil {
			cfg.OnResult(r)
		}

		switch now := time.Now(); {
		case now.Sub(lastExport) >= exportEvery:
			lastExport, lastStatus = now, now
			if err := ss.Writer.Flush(); err != nil {
				return err
			}
			return ss.Store.Export(context.Background(), ss.Status(store.StateRunning), ss.Total < bigList)
		case now.Sub(lastStatus) >= statusEvery:
			lastStatus = now
			return ss.Store.WriteStatus(ss.Status(store.StateRunning))
		}
		return nil
	})
	err = errors.Join(err, ss.Writer.Flush(), ss.selection.Err())

	// Stopped only if domains are left: a stop after the last check is a completed run
	stopped = ctx.Err() != nil && ss.Done() < ss.Todo
	state := store.StateCompleted
	if err != nil || stopped {
		state = store.StateStopped
	}
	if state == store.StateCompleted {
		ss.finished = true
		if ss.Recheck != nil {
			err = ss.Store.SetRecheck(nil)
		}
	}

	log("writing the text lists")
	return stopped, errors.Join(err, ss.Store.Export(context.Background(), ss.Status(state), true))
}

// Done returns the number of domains checked by this session
func (ss *Session) Done() int {
	return int(ss.done.Load())
}

// Status describes the run for run.json
func (ss *Session) Status(state store.State) store.Status {
	summary := ss.Writer.Summary()
	status := store.Status{
		List:       ss.Settings.List,
		Total:      ss.Total,
		Checked:    summary.Total,
		State:      state,
		DNS:        ss.Settings.DNS,
		ControlSNI: ss.Settings.ControlSNI,
		Timeout:    ss.Settings.Timeout.String(),
		Rate:       ss.Rate,
		Started:    ss.Settings.Started,
		Counts:     summary.ByCategory,
	}
	// A finished recheck is not shown, so run.json shows only work in progress
	if ss.Recheck != nil && !ss.finished {
		status.Recheck = &store.RecheckStatus{
			Categories: ss.Recheck.Categories,
			Since:      ss.Recheck.Since,
			Selected:   ss.Todo,
			Rechecked:  ss.Done(),
		}
	}
	return status
}

func (ss *Session) Close() error {
	return ss.Store.Close()
}
