package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/domain"
	"github.com/armanlife/iran-blocked-domains/checker/internal/merge"
	"github.com/armanlife/iran-blocked-domains/checker/internal/session"
	"github.com/armanlife/iran-blocked-domains/checker/internal/store"
)

func runMerge(args []string) error {
	flags := newFlagSet("checker merge", "-dir <folder> -ranking <file> -previous <file>",
		"Builds the ranked list of a new run folder from a ranking, such as the host.io\n"+
			"top 10 million, and a previous ranked list. Domains of the ranking keep its order.\n"+
			"A domain only in the previous list goes right after its parent domain, or after\n"+
			"the nearest domain before it in the previous list that the ranking has.")
	var (
		dir      = flags.String("dir", "", "new run `folder` (required)")
		ranking  = flags.String("ranking", "", "ranking `file`, one domain per line or CSV (required)")
		previous = flags.String("previous", "", "previous ranked list `file` (required)")
	)
	if ok, err := parse(flags, args); !ok {
		return err
	}
	if *dir == "" || *ranking == "" || *previous == "" {
		flags.Usage()
		return errors.New("-dir, -ranking and -previous are required")
	}

	s, err := store.Open(*dir)
	if err != nil {
		return err
	}
	defer s.Close()
	if n, err := s.DomainCount(); err != nil {
		return err
	} else if n > 0 {
		return fmt.Errorf("%s already has a list of %d domains, use a new folder", *dir, n)
	}

	im, err := s.NewImporter()
	if err != nil {
		return err
	}
	start := time.Now()
	stats, err := merge.Merge(*ranking, *previous, im, func(added int) {
		logLine(fmt.Sprintf("  %d domains added", added))
	})
	if err := errors.Join(err, im.Close()); err != nil {
		return err
	}

	list := session.ListName(*ranking) + " + " + session.ListName(*previous)
	if err := s.SetMeta("list", list); err != nil {
		return err
	}

	logLine(fmt.Sprintf("merged %d domains in %s", im.Added, time.Since(start).Round(time.Second)))
	logLine(fmt.Sprintf("  ranking:        %d domains", stats.Ranking))
	logLine(fmt.Sprintf("  previous list:  %d domains", stats.Previous))
	logLine(fmt.Sprintf("    in the ranking:          %d", stats.PreviousInRanking))
	logLine(fmt.Sprintf("    after their parent:      %d", stats.AfterParent))
	logLine(fmt.Sprintf("    after their neighbor:    %d", stats.AfterNeighbor))
	logLine(fmt.Sprintf("    at the top:              %d", stats.AtTop))
	logLine(fmt.Sprintf("start checking with: checker scan -dir %s", *dir))
	return nil
}

func runExport(args []string) error {
	flags := newFlagSet("checker export", "-dir <folder>",
		"Writes the text lists and run.json of a run folder from its database. A running\n"+
			"check does this by itself; use it for example after copying a database.")
	dir := flags.String("dir", "", "run `folder` (required)")
	if ok, err := parse(flags, args); !ok {
		return err
	}
	if *dir == "" || !store.Exists(*dir) {
		flags.Usage()
		return errors.New("-dir must be a run folder with a database")
	}

	s, err := store.Open(*dir)
	if err != nil {
		return err
	}
	defer s.Close()

	status, err := statusFromDB(s)
	if err != nil {
		return err
	}
	if err := s.Export(context.Background(), status, true); err != nil {
		return err
	}
	logLine(fmt.Sprintf("exported %d results of %d domains to %s", status.Checked, status.Total, *dir))
	return nil
}

// statusFromDB rebuilds run.json for a run that is not running, keeping its saved state
func statusFromDB(s *store.Store) (store.Status, error) {
	var status store.Status
	settings, err := s.Settings()
	if err != nil {
		return status, err
	}
	total, err := s.DomainCount()
	if err != nil {
		return status, err
	}
	counts, err := s.Counts()
	if err != nil {
		return status, err
	}

	state := store.StateStopped
	if previous, err := store.ReadStatus(s.Dir); err == nil {
		state = previous.State
		status.Rate = previous.Rate
	}
	if counts.Total == total && total > 0 {
		state = store.StateCompleted
	}

	status.List, status.Total, status.Checked, status.State = settings.List, total, counts.Total, state
	status.DNS, status.ControlSNI, status.Timeout = settings.DNS, settings.ControlSNI, settings.Timeout.String()
	status.Started, status.Counts = settings.Started, counts.ByCategory
	return status, nil
}

func runData(args []string) error {
	flags := newFlagSet("checker data", "-from <blocked.txt> -to <folder>",
		"Writes a blocked list for releases, in rank order.")
	var (
		from = flags.String("from", "", "blocked list `file`, such as results/host10m/blocked.txt (required)")
		to   = flags.String("to", "", "output `folder`, such as ../data (required)")
		name = flags.String("name", "ir-blocked", "file `name` of the list")
	)
	if ok, err := parse(flags, args); !ok {
		return err
	}
	if *from == "" || *to == "" {
		flags.Usage()
		return errors.New("-from and -to are required")
	}

	domains, err := store.WriteData(*from, *to, *name)
	if err != nil {
		return err
	}
	logLine(fmt.Sprintf("wrote %d domains to %s", domains, filepath.Join(*to, *name)))
	return nil
}

func runFind(args []string) error {
	flags := newFlagSet("checker find", "-dir <folder> <domain ...>", "Shows the rank, the result and the previous results of domains.")
	dir := flags.String("dir", "", "run `folder` (required)")
	if ok, err := parse(flags, args); !ok {
		return err
	}
	if *dir == "" || !store.Exists(*dir) || flags.NArg() == 0 {
		flags.Usage()
		return errors.New("-dir must be a run folder with a database, followed by domains")
	}

	s, err := store.Open(*dir)
	if err != nil {
		return err
	}
	defer s.Close()

	for _, arg := range flags.Args() {
		name, ok := domain.Normalize(arg)
		if !ok {
			fmt.Printf("%s\n  invalid domain\n\n", arg)
			continue
		}
		f, err := s.Find(context.Background(), name)
		if err != nil {
			return err
		}
		fmt.Println(name)
		switch {
		case f == nil:
			fmt.Println("  not in the list")
		case f.Result == nil:
			fmt.Printf("  rank     %d (%s)\n  result   not checked yet\n", f.Rank, f.Source)
		default:
			r := f.Result
			fmt.Printf("  rank     %d (%s)\n", f.Rank, f.Source)
			fmt.Printf("  result   %s  %s  %s\n", r.Category, strings.Join(r.IPs, " "), r.Reason)
			fmt.Printf("  checked  %s\n", f.Checked.Format("2006-01-02 15:04"))
			for _, past := range f.History {
				fmt.Printf("  before   %s (%s)\n", past.Category, past.Checked.Format("2006-01-02 15:04"))
			}
		}
		fmt.Println()
	}
	return nil
}
