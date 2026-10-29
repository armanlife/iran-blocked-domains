// Package cli runs the checker with command line flags, for scripts and automation.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/domain"
	"github.com/armanlife/iran-blocked-domains/checker/internal/guard"
	"github.com/armanlife/iran-blocked-domains/checker/internal/report"
	"github.com/armanlife/iran-blocked-domains/checker/internal/runner"
)

const usage = `Usage:
  checker                         interactive mode
  checker [flags] [domain ...]    check domains and print the results
  checker scan   -dir <folder> ...  check a list in a run folder, resumable
  checker merge  -dir <folder> ...  build a ranked list from a ranking and a previous list
  checker export -dir <folder>      write the text lists of a run folder again
  checker data   -from <file> ...   write a blocked list for releases
  checker find   -dir <folder> <domain ...>   show the rank, result and history of domains

Run "checker <command> -h" for the flags of a command.
`

func Run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "scan":
			return runScan(args[1:])
		case "merge":
			return runMerge(args[1:])
		case "export":
			return runExport(args[1:])
		case "data":
			return runData(args[1:])
		case "find":
			return runFind(args[1:])
		case "help":
			fmt.Print(usage)
			return nil
		}
	}
	return runQuick(args)
}

// checkFlags are the flags of every command that checks domains
type checkFlags struct {
	rate, workers, guardLimit *int
	dns, control              *string
	timeout                   *time.Duration
}

func addCheckFlags(flags *flag.FlagSet) *checkFlags {
	return &checkFlags{
		rate:       flags.Int("rate", 10, "max domains checked per second (0 = unlimited)"),
		workers:    flags.Int("workers", 0, "max concurrent checks (default: based on -rate)"),
		dns:        flags.String("dns", "1.1.1.1", "DNS server to query"),
		control:    flags.String("control", "example.com", "unblocked domain used to confirm SNI filtering and to detect network outages"),
		timeout:    flags.Duration("timeout", 5*time.Second, "timeout of each DNS query and TLS handshake"),
		guardLimit: flags.Int("conntrack-limit", 50, "pause new checks while the Linux conntrack table is fuller than this `percent`, to protect other services (0 = off)"),
	}
}

func (f *checkFlags) validate() error {
	if *f.rate < 0 || *f.workers < 0 {
		return fmt.Errorf("-rate and -workers must be >= 0")
	}
	if *f.workers == 0 {
		*f.workers = runner.WorkersFor(*f.rate)
	}
	return nil
}

func newFlagSet(name, args, description string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s %s\n\n%s\n\n", name, args, description)
		flags.PrintDefaults()
	}
	return flags
}

// parse returns false when only help was asked
func parse(flags *flag.FlagSet, args []string) (bool, error) {
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// signalContext is canceled by the first Ctrl+C. A second one quits at once.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		<-ctx.Done()
		stop()
		logLine("stopping: finishing the running checks, press Ctrl+C again to quit now")
	}()
	return ctx, stop
}

func outageLog(down bool) {
	if down {
		logLine("network is down: the DNS server answers for no domain, waiting for it to come back")
	} else {
		logLine("network is back, continuing")
	}
}

func logLine(s string) {
	fmt.Fprintln(os.Stderr, s)
}

func rateString(rate int) string {
	if rate == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d/s", rate)
}

// runQuick checks domains from arguments, a file or stdin, and prints the results
func runQuick(args []string) error {
	flags := newFlagSet("checker", "[flags] [domain ...]", "Checks domains and prints the results. Run without arguments for interactive mode, or see \"checker help\".")
	var (
		inputPath   = flags.String("i", "", "list or CSV file with one domain per line (default: domains in arguments, or stdin)")
		outputPath  = flags.String("o", "", "write results to `file` (default: stdout)")
		format      = flags.String("format", "text", "output format: text, csv, json or list (blocked domains only)")
		blockedOnly = flags.Bool("blocked", false, "only output blocked domains")
		cf          = addCheckFlags(flags)
	)
	if ok, err := parse(flags, args); !ok {
		return err
	}
	if err := cf.validate(); err != nil {
		return err
	}

	domains, finish, err := readInput(*inputPath, flags.Args())
	if err != nil {
		return err
	}

	out := io.Writer(os.Stdout)
	if *outputPath != "" {
		f, err := os.Create(*outputPath)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	writer, err := report.New(out, *format)
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

	logLine(fmt.Sprintf("checking domains (rate: %s, workers: %d, dns: %s)", rateString(*cf.rate), *cf.workers, *cf.dns))
	checker := check.New(*cf.dns, *cf.control, *cf.timeout)
	items := guard.Throttle(ctx, domain.Numbered(domains), guard.Conntrack, *cf.guardLimit, logLine)
	start := time.Now()

	opts := runner.Options{Rate: *cf.rate, Workers: *cf.workers, Online: checker.Online, OnOutage: outageLog}
	summary, err := runner.Run(ctx, checker.Check, items, opts, func(_ int, r check.Result) error {
		if *blockedOnly && r.Status != check.StatusBlocked {
			return nil
		}
		return writer.Write(r)
	})
	if err != nil {
		return err
	}
	if err := finish(); err != nil {
		return err
	}

	logLine(fmt.Sprintf("%s in %s", summary, time.Since(start).Round(time.Millisecond)))
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("interrupted after %d domains", summary.Total)
	case summary.Total == 0:
		flags.Usage()
		return fmt.Errorf("no domains to check")
	}
	return nil
}

// readInput returns the domains to check, from a file, the arguments or stdin.
// finish reports skipped lines and read errors once all domains are consumed.
func readInput(path string, args []string) (domains iter.Seq[string], finish func() error, err error) {
	if path == "" && len(args) > 0 {
		list, invalid := domain.Parse(strings.Join(args, " "))
		for _, arg := range invalid {
			logLine("skipped invalid domain: " + arg)
		}
		return slices.Values(list), func() error { return nil }, nil
	}

	in := os.Stdin
	if path != "" {
		if in, err = os.Open(filepath.Clean(path)); err != nil {
			return nil, nil, err
		}
	}

	reader := domain.NewReader(in)
	finish = func() error {
		defer in.Close()
		if reader.Invalid > 0 {
			logLine(fmt.Sprintf("skipped %d lines without a valid domain", reader.Invalid))
		}
		return reader.Err()
	}
	return reader.All(), finish, nil
}
