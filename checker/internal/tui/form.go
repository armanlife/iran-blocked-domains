package tui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/domain"
	"github.com/armanlife/iran-blocked-domains/checker/internal/runner"
	"github.com/armanlife/iran-blocked-domains/checker/internal/session"
	"github.com/armanlife/iran-blocked-domains/checker/internal/store"
)

type kind int

const (
	kindNew kind = iota
	kindResume
	kindRecheck
)

// options are the answers of the interactive questions
type options struct {
	Kind        kind
	Dir         string         // Run folder
	Run         *store.RunInfo // Existing run, for resume and recheck
	Path        string         // List of a new run
	Replace     bool           // Rename the existing folder of a new run
	Total       int            // Domains in the list
	Todo        int            // Domains to check, estimated for the summary
	Invalid     int            // Lines without a valid domain in Path
	Recheck     []check.Category
	Rate        int
	Workers     int
	DNS         string
	BlockedOnly bool
}

// errBack returns to the first question
var errBack = errors.New("back")

const (
	customRate = -1
	resultsDir = "results"
)

// projectList is the published list of the project: a data folder that has it
// is the project's data folder
const projectList = "ir-blocked"

// projectDepth is how many folders above the working and the program folder
// the project's data folder is looked for, such as from checker/dist
const projectDepth = 3

type listFile struct {
	Path string
	Size int64
}

// listDir is a folder searched for domain lists
type listDir struct {
	Path string
	Data bool // The project's data folder: its files have no extension
}

// listDirs returns the folders searched for domain lists: the working folder,
// the program's folder and the project's data folder
func listDirs() []listDir {
	starts := []string{"."}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}

	var dirs []listDir
	add := func(path string, data bool) {
		for _, d := range dirs {
			if sameDir(d.Path, path) {
				return
			}
		}
		dirs = append(dirs, listDir{Path: relative(path), Data: data})
	}
	for _, start := range starts {
		add(start, false)
	}
	for _, start := range starts {
		dir := start
		for range projectDepth + 1 {
			data := filepath.Join(dir, "data")
			if _, err := os.Stat(filepath.Join(data, projectList)); err == nil {
				add(data, true)
				break
			}
			dir = filepath.Join(dir, "..")
		}
	}
	return dirs
}

func sameDir(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// relative returns path relative to the working folder when it is shorter
func relative(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	wd, err := os.Getwd()
	if err != nil {
		return abs
	}
	if rel, err := filepath.Rel(wd, abs); err == nil && len(rel) < len(abs) {
		return rel
	}
	return abs
}

// findLists returns the list and CSV files in dirs
func findLists(dirs []listDir) []listFile {
	var files []listFile
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir.Path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			isList := ext == ".csv" || ext == ".txt" || ext == ".list" || (dir.Data && ext == "")
			if !entry.Type().IsRegular() || !isList {
				continue
			}

			info, err := entry.Info()
			if err != nil || info.Size() == 0 {
				continue
			}
			files = append(files, listFile{Path: filepath.Join(dir.Path, entry.Name()), Size: info.Size()})
		}
	}
	return files
}

// noListError explains what a domain list is and where it was looked for
func noListError(dirs []listDir) error {
	var b strings.Builder
	b.WriteString("No domain list found.\n\n")
	b.WriteString("   A list is a .txt or .csv file with one domain per line, such as:\n")
	b.WriteString("       google.com\n       x.com\n\n")
	b.WriteString("   Put a list in one of these folders and run the checker again:\n")
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir.Path)
		if err != nil {
			abs = dir.Path
		}
		b.WriteString("       " + abs + "\n")
	}
	return errors.New(strings.TrimSuffix(b.String(), "\n"))
}

type choice struct {
	kind kind
	run  *store.RunInfo
}

func ask() (*options, error) {
	runs := store.Runs(resultsDir)
	if len(runs) == 0 {
		return askNew()
	}

	var choices []huh.Option[choice]
	for i := range runs[:min(len(runs), 8)] {
		run := &runs[i]
		if unfinished(run.Status) {
			choices = append(choices, huh.NewOption(resumeLabel(run.Status), choice{kindResume, run}))
		} else {
			choices = append(choices, huh.NewOption(recheckLabel(run.Status), choice{kindRecheck, run}))
		}
	}
	choices = append(choices, huh.NewOption("Start a new check", choice{kind: kindNew}))

	picked := choices[0].Value
	err := newForm(huh.NewGroup(
		huh.NewSelect[choice]().
			Value(&picked).
			Title("What do you want to do?").
			Description("Checks in the results folder").
			Options(choices...),
	)).Run()
	if err != nil {
		return nil, err
	}

	switch picked.kind {
	case kindResume:
		return askResume(picked.run)
	case kindRecheck:
		return askRecheck(picked.run)
	default:
		return askNew()
	}
}

func unfinished(s store.Status) bool {
	return s.State != store.StateCompleted
}

func resumeLabel(s store.Status) string {
	what := fmt.Sprintf("%s of %s  (%.1f%%)", number(s.Checked), number(s.Total), float64(s.Checked)*100/float64(max(s.Total, 1)))
	if s.Recheck != nil {
		what = fmt.Sprintf("recheck %s of %s", number(s.Recheck.Rechecked), number(s.Recheck.Selected))
	}
	return fmt.Sprintf("Resume   %-20s %s", s.List, mutedStyle.Render(what+"  ·  "+s.Updated.Format("Jan 2 15:04")))
}

func recheckLabel(s store.Status) string {
	manual := 0
	for c, n := range s.Counts {
		if c.Status() == check.StatusManual {
			manual += n
		}
	}
	return fmt.Sprintf("Recheck  %-20s %s", s.List, mutedStyle.Render(fmt.Sprintf("%s results to check manually", number(manual))))
}

func askNew() (*options, error) {
	dirs := listDirs()
	files := findLists(dirs)
	if len(files) == 0 {
		return nil, noListError(dirs)
	}

	fileOptions := make([]huh.Option[string], len(files))
	for i, f := range files {
		label := fmt.Sprintf("%-40s %s", filepath.ToSlash(f.Path), mutedStyle.Render(fmt.Sprintf("%9s", size(f.Size))))
		fileOptions[i] = huh.NewOption(label, f.Path)
	}

	var (
		opts      = options{Kind: kindNew, Path: files[0].Path, Rate: 10, DNS: "1.1.1.1", BlockedOnly: true}
		rateText  string
		customDNS string
	)

	if err := newForm(huh.NewGroup(
		huh.NewSelect[string]().
			Value(&opts.Path).
			Title("Which list do you want to check?").
			Description("Lists in this folder and in the project's data folder").
			// No Height: with one, huh scrolls the chosen list to the top
			// and hides the lists above it
			Options(fileOptions...),
	)).Run(); err != nil {
		return nil, err
	}

	opts.Dir = filepath.Join(resultsDir, session.ListName(opts.Path))
	if store.Exists(opts.Dir) {
		replace := false
		err := newForm(huh.NewGroup(
			huh.NewConfirm().
				Value(&replace).
				Title(filepath.ToSlash(opts.Dir) + " already has a check of this list").
				Description("Replacing it keeps the old folder, renamed with the date").
				Affirmative("Replace").
				Negative("Back"),
		)).Run()
		if err != nil {
			return nil, err
		}
		if !replace {
			return nil, errBack
		}
		opts.Replace = true
	}

	groups := rateGroups(&opts.Rate, &rateText, false)
	groups = append(groups,
		huh.NewGroup(
			huh.NewSelect[string]().
				Value(&opts.DNS).
				Title("DNS server").
				Description("Queries are sent as plain DNS, so Iran's filtering can see and answer them").
				Options(
					huh.NewOption("Cloudflare  1.1.1.1", "1.1.1.1"),
					huh.NewOption("Google      8.8.8.8", "8.8.8.8"),
					huh.NewOption("Quad9       9.9.9.9", "9.9.9.9"),
					huh.NewOption("Custom…", ""),
				),
		),

		huh.NewGroup(
			huh.NewInput().
				Title("DNS server IP").
				Placeholder("1.0.0.1").
				Value(&customDNS).
				Validate(func(s string) error {
					if net.ParseIP(strings.TrimSpace(s)) == nil {
						return errors.New("enter a valid IP address")
					}
					return nil
				}),
		).WithHideFunc(func() bool { return opts.DNS != "" }),

		showGroup(&opts.BlockedOnly),
	)
	if err := newForm(groups...).Run(); err != nil {
		return nil, err
	}

	fmt.Println(mutedStyle.Render(" Counting domains in " + filepath.ToSlash(opts.Path) + "…"))
	var err error
	opts.Total, opts.Invalid, err = domain.Count(opts.Path)
	if err != nil {
		return nil, err
	}
	if opts.Total == 0 {
		return nil, errors.New("no valid domain found in " + opts.Path)
	}
	opts.Todo = opts.Total

	opts.Rate = parseRate(opts.Rate, rateText)
	opts.Workers = runner.WorkersFor(opts.Rate)
	if opts.DNS == "" {
		opts.DNS = strings.TrimSpace(customDNS)
	}
	return &opts, nil
}

func askResume(run *store.RunInfo) (*options, error) {
	s := run.Status
	opts := options{
		Kind: kindResume, Dir: run.Dir, Run: run,
		Total: s.Total, Todo: s.Total - s.Checked,
		Rate: max(s.Rate, 1), DNS: s.DNS, BlockedOnly: true,
	}
	if s.Recheck != nil {
		opts.Recheck = s.Recheck.Categories
		opts.Todo = s.Recheck.Selected - s.Recheck.Rechecked
	}

	var rateText string
	groups := append(rateGroups(&opts.Rate, &rateText, true), showGroup(&opts.BlockedOnly))
	if err := newForm(groups...).Run(); err != nil {
		return nil, err
	}

	opts.Rate = parseRate(opts.Rate, rateText)
	opts.Workers = runner.WorkersFor(opts.Rate)
	return &opts, nil
}

func askRecheck(run *store.RunInfo) (*options, error) {
	s := run.Status
	opts := options{
		Kind: kindRecheck, Dir: run.Dir, Run: run,
		Total: s.Total, Rate: max(s.Rate, 1), DNS: s.DNS, BlockedOnly: true,
	}

	var categoryOptions []huh.Option[check.Category]
	for _, c := range check.Categories {
		if n := s.Counts[c]; n > 0 {
			option := huh.NewOption(fmt.Sprintf("%-16s %s", c, mutedStyle.Render(number(n))), c)
			categoryOptions = append(categoryOptions, option.Selected(c.Status() == check.StatusManual))
		}
	}
	if len(categoryOptions) == 0 {
		return nil, errors.New("this check has no results yet")
	}

	var rateText string
	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewMultiSelect[check.Category]().
				Value(&opts.Recheck).
				Title("Which results do you want to check again?").
				Description("x to select or unselect. Each domain keeps its rank, and its old result is kept in the history").
				Options(categoryOptions...).
				Validate(func(c []check.Category) error {
					if len(c) == 0 {
						return errors.New("select at least one")
					}
					return nil
				}),
		),
	}
	groups = append(groups, rateGroups(&opts.Rate, &rateText, true)...)
	groups = append(groups, showGroup(&opts.BlockedOnly))
	if err := newForm(groups...).Run(); err != nil {
		return nil, err
	}

	for _, c := range opts.Recheck {
		opts.Todo += s.Counts[c]
	}
	opts.Rate = parseRate(opts.Rate, rateText)
	opts.Workers = runner.WorkersFor(opts.Rate)
	return &opts, nil
}

// rateGroups asks for the speed. The current *rate is the first option, since
// huh scrolls the selected option to the top and would hide the ones above it.
func rateGroups(rate *int, rateText *string, previous bool) []*huh.Group {
	var rateOptions []huh.Option[int]
	if previous {
		rateOptions = append(rateOptions, huh.NewOption(number(*rate)+" per second"+mutedStyle.Render("  · same as before"), *rate))
	}
	// Every domain per second takes about 300 entries of the connection table
	// of the router or server, which has 4,000 to 16,000 on home routers: a
	// full table drops connections and gives wrong results
	for _, n := range []int{10, 25, 50, 100, 200} {
		if !previous || n != *rate {
			rateOptions = append(rateOptions, huh.NewOption(number(n)+" per second", n))
		}
	}
	rateOptions = append(rateOptions, huh.NewOption("Custom…", customRate))

	return []*huh.Group{
		huh.NewGroup(
			huh.NewSelect[int]().
				Value(rate).
				Title("How fast?").
				Description("Number of domains checked per second. High speeds can overload\nthe router and give wrong results: 25 or less on home internet").
				Options(rateOptions...),
		),

		huh.NewGroup(
			huh.NewInput().
				Title("Domains per second").
				Placeholder("200").
				Value(rateText).
				Validate(func(s string) error {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 || n > 1000 {
						return errors.New("enter a number between 1 and 1000")
					}
					return nil
				}),
		).WithHideFunc(func() bool { return *rate != customRate }),
	}
}

func showGroup(blockedOnly *bool) *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[bool]().
			Value(blockedOnly).
			Title("Which results do you want to see while checking?").
			Description("Every result is saved either way").
			Options(
				huh.NewOption("Blocked domains only", true),
				huh.NewOption("All results", false),
			),
	)
}

func parseRate(rate int, text string) int {
	if rate == customRate {
		rate, _ = strconv.Atoi(strings.TrimSpace(text))
	}
	return rate
}

// displayPath shortens path relative to the working directory
func displayPath(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil {
			path = rel
		}
	}
	return filepath.ToSlash(path)
}

// confirm shows a summary of opts and asks to start
func confirm(opts *options) (bool, error) {
	var rows [][2]string
	switch opts.Kind {
	case kindNew:
		domains := number(opts.Total)
		if opts.Invalid > 0 {
			domains += mutedStyle.Render(fmt.Sprintf("   %s invalid lines skipped", number(opts.Invalid)))
		}
		rows = append(rows, [2]string{"List", displayPath(opts.Path)}, [2]string{"Domains", domains})
		if opts.Replace {
			rows = append(rows, [2]string{"Replaces", "the previous check, kept as a dated folder"})
		}
	case kindResume:
		rows = append(rows, [2]string{"List", opts.Run.Status.List}, [2]string{"Left", number(opts.Todo) + "  ·  continues where it stopped"})
	case kindRecheck:
		names := make([]string, len(opts.Recheck))
		for i, c := range opts.Recheck {
			names[i] = string(c)
		}
		rows = append(rows, [2]string{"List", opts.Run.Status.List}, [2]string{"Recheck", strings.Join(names, ", ")}, [2]string{"Domains", number(opts.Todo)})
	}

	estimate := time.Duration(float64(opts.Todo) / float64(max(opts.Rate, 1)) * float64(time.Second))
	rows = append(rows,
		[2]string{"Speed", fmt.Sprintf("%s per second", number(opts.Rate))},
		[2]string{"Takes about", duration(estimate)},
		[2]string{"DNS server", opts.DNS},
		[2]string{"Save to", filepath.ToSlash(opts.Dir)},
	)

	var lines []string
	for _, row := range rows {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("%-13s", row[0]))+textStyle.Render(row[1]))
	}
	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))

	start := true
	err := newForm(huh.NewGroup(
		huh.NewConfirm().
			Value(&start).
			Title("Ready to start?").
			Affirmative("Start").
			Negative("Change settings"),
	)).Run()
	return start, err
}

// formWidth is the widest a form gets. A form with a fixed width is not drawn
// again for every width while the window is resized: the terminal wraps the
// lines already drawn, so drawing them again would leave copies behind.
const formWidth = 80

func newForm(groups ...*huh.Group) *huh.Form {
	width := formWidth
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		width = min(width, w)
	}
	return huh.NewForm(groups...).WithTheme(theme()).WithWidth(width)
}
