package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/session"
	"github.com/armanlife/iran-blocked-domains/checker/internal/store"
)

func runScan(args []string) error {
	flags := newFlagSet("checker scan", "-dir <folder> [-i <list>] [flags]",
		"Checks the domains of a run folder in rank order and saves every result in its database.\n"+
			"Run it again with the same folder to continue where it stopped. The text lists\n"+
			"(blocked.txt, ...) and run.json are kept up to date in the folder.")
	var (
		dir      = flags.String("dir", "", "run `folder` (required)")
		input    = flags.String("i", "", "list or CSV `file` to import into a new run folder")
		recheck  = flags.String("recheck", "", "check again the domains of these comma separated `categories`, keeping their rank, e.g. dns-timeout,connect-failed")
		since    = flags.String("since", "", "with -recheck: only results older than this RFC 3339 `time` (default: continue the unfinished recheck, or now)")
		progress = flags.Duration("progress", 10*time.Second, "how often the progress is printed")
		cf       = addCheckFlags(flags)
	)
	if ok, err := parse(flags, args); !ok {
		return err
	}
	if *dir == "" {
		flags.Usage()
		return errors.New("-dir is required")
	}
	if err := cf.validate(); err != nil {
		return err
	}

	opts := session.Options{
		Dir:      *dir,
		Input:    *input,
		Defaults: store.Settings{DNS: *cf.dns, ControlSNI: *cf.control, Timeout: *cf.timeout},
		Rate:     *cf.rate,
		Log:      logLine,
	}
	if *recheck != "" {
		categories, err := store.ParseCategories(*recheck)
		if err != nil {
			return err
		}
		opts.Recheck = categories
		if *since != "" {
			if opts.Since, err = time.Parse(time.RFC3339, *since); err != nil {
				return fmt.Errorf("-since: %w", err)
			}
		}
	}

	ctx, stop := signalContext()
	defer stop()

	ss, err := session.Prepare(ctx, opts)
	if err != nil {
		return err
	}
	defer ss.Close()

	if ss.Recheck == nil {
		if current, _ := ss.Store.CurrentRecheck(); current != nil {
			logLine(fmt.Sprintf("note: a recheck of %v is unfinished, continue it with -recheck", current.Categories))
		}
	}
	what := "domains"
	if ss.Recheck != nil {
		what = fmt.Sprintf("domains of %v to recheck", ss.Recheck.Categories)
	}
	logLine(fmt.Sprintf("checking %d %s of %d (rate: %s, workers: %d, dns: %s), saving to %s",
		ss.Todo, what, ss.Total, rateString(ss.Rate), *cf.workers, ss.Settings.DNS, *dir))

	start, lastProgress := time.Now(), time.Now()
	stopped, err := ss.Run(ctx, session.RunConfig{
		Workers:    *cf.workers,
		GuardLimit: *cf.guardLimit,
		OnOutage:   outageLog,
		Log:        logLine,
		OnResult: func(check.Result) {
			if time.Since(lastProgress) >= *progress {
				lastProgress = time.Now()
				printProgress(ss, start)
			}
		},
	})
	if err != nil {
		return err
	}

	s := ss.Writer.Summary()
	logLine(fmt.Sprintf("%s in %s", s, time.Since(start).Round(time.Millisecond)))
	for _, c := range check.Categories {
		if n := s.ByCategory[c]; n > 0 {
			logLine(fmt.Sprintf("  %-15s %d", c, n))
		}
	}
	logLine("results saved in " + *dir)

	if stopped {
		return fmt.Errorf("interrupted after %d of %d domains, run the same command again to continue", ss.Done(), ss.Todo)
	}
	return nil
}

func printProgress(ss *session.Session, start time.Time) {
	done := ss.Done()
	speed := float64(done) / time.Since(start).Seconds()

	eta := "-"
	if speed > 0 {
		eta = time.Duration(float64(ss.Todo-done) / speed * float64(time.Second)).Round(time.Minute).String()
	}

	s := ss.Writer.Summary()
	logLine(fmt.Sprintf("%d / %d (%.1f%%)  %.0f/s  eta %s  blocked %d  manual %d",
		done, ss.Todo, float64(done)*100/float64(max(ss.Todo, 1)), speed, eta, s.Blocked, s.Manual))
}
