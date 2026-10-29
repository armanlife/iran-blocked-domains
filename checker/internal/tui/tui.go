// Package tui is the interactive mode: it asks what to check and shows live, colored progress.
package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/session"
	"github.com/armanlife/iran-blocked-domains/checker/internal/store"
)

const (
	controlSNI = "example.com"
	timeout    = 5 * time.Second
	guardLimit = 50 // Percent of the conntrack table, see package guard

	// Shown in the terminal tab instead of the working folder
	windowTitle = "Iran Blocked Domains · Checker"
)

// restoreTitle gives the terminal its title back
var restoreTitle = func() {}

func Run() error {
	restoreTitle = setWindowTitle(windowTitle)
	defer restoreTitle()
	fmt.Println(banner())

	for {
		opts, err := ask()
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		if errors.Is(err, errBack) {
			continue
		}

		if err == nil {
			var start bool
			start, err = confirm(opts)
			if errors.Is(err, huh.ErrUserAborted) {
				return nil
			}
			if err == nil && !start {
				continue
			}
		}

		if err == nil {
			err = execute(opts)
		}
		if err != nil {
			fmt.Println("\n " + errorStyle.Render("✗ "+err.Error()) + "\n")
		}
		waitIfOwnWindow()
		return nil
	}
}

// waitIfOwnWindow keeps a window opened only for the checker, such as by a
// double click on Windows, until Enter is pressed: it closes when the checker
// exits, and the results could not be read
func waitIfOwnWindow() {
	if !ownWindow() {
		return
	}
	fmt.Print(mutedStyle.Render(" Press Enter to exit "))
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func execute(opts *options) error {
	if opts.Replace {
		old := opts.Dir + ".old-" + time.Now().Format("20060102-150405")
		if err := os.Rename(opts.Dir, old); err != nil {
			return err
		}
		fmt.Println(mutedStyle.Render(" Previous check kept in " + filepath.ToSlash(old)))
	}

	sessionOpts := session.Options{
		Dir:      opts.Dir,
		Defaults: store.Settings{DNS: opts.DNS, ControlSNI: controlSNI, Timeout: timeout},
		Recheck:  opts.Recheck,
		Rate:     opts.Rate,
		Log:      func(s string) { fmt.Println(mutedStyle.Render(" " + s)) },
	}
	if opts.Kind == kindNew {
		sessionOpts.Input = opts.Path
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ss, err := session.Prepare(ctx, sessionOpts)
	if err != nil {
		return err
	}
	defer ss.Close()

	model := newProgressModel(ss, opts.BlockedOnly, cancel)
	program := tea.NewProgram(model)
	notice := func(s string) { program.Send(noticeMsg(s)) }

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		stopped, err := ss.Run(ctx, session.RunConfig{
			Workers:    opts.Workers,
			GuardLimit: guardLimit,
			OnResult:   func(r check.Result) { program.Send(resultMsg(r)) },
			OnOutage: func(down bool) {
				if down {
					notice("Network is down: the DNS server answers for no domain. Waiting for it to come back…")
				} else {
					notice("Network is back, continuing")
				}
			},
			Log: notice,
		})
		program.Send(doneMsg{stopped: stopped, err: err})
	}()

	_, err = program.Run()
	if model.force {
		// Like a killed process: what is saved is kept, and the rest is checked again on resume
		fmt.Println("\n " + manualStyle.Render("■ Quit") + mutedStyle.Render("  ·  run the checker again and choose Resume to continue"))
		restoreTitle()
		os.Exit(130)
	}
	cancel()
	// The session must not be closed while results are still being saved
	<-finished

	if err == nil && model.done == nil {
		err = errors.New("stopped")
	} else if err == nil {
		err = model.done.err
	}
	if err != nil {
		return err
	}

	fmt.Println(summaryView(ss, time.Since(model.start), model.done.stopped))
	return nil
}

func summaryView(ss *session.Session, elapsed time.Duration, stopped bool) string {
	s := ss.Writer.Summary()

	head := openStyle.Render("✓ Done")
	if stopped {
		head = manualStyle.Render("■ Stopped") + mutedStyle.Render(fmt.Sprintf("  ·  %s of %s domains checked this time", number(ss.Done()), number(ss.Todo)))
	}

	lines := []string{head, ""}
	row := func(label, value string) {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("%-19s", label))+value)
	}

	if ss.Recheck != nil {
		row("Rechecked", textStyle.Render(number(ss.Done())))
	}
	row("Checked", textStyle.Render(fmt.Sprintf("%s of %s", number(s.Total), number(ss.Total))))
	row("Blocked", blockedStyle.Render(number(s.Blocked)))
	for _, c := range []check.Category{check.CategoryBlockedDNS, check.CategoryBlockedSNI} {
		row("  "+categoryLabels[c], textStyle.Render(number(s.ByCategory[c])))
	}
	row("Not blocked", openStyle.Render(number(s.NotBlocked)))
	row("Manual", manualStyle.Render(number(s.Manual)))
	for _, c := range check.Categories {
		if c.Status() == check.StatusManual {
			row("  "+categoryLabels[c], textStyle.Render(number(s.ByCategory[c])))
		}
	}
	lines = append(lines, "")
	row("Time", textStyle.Render(elapsed.Round(100*time.Millisecond).String()))

	view := "\n" + boxStyle.Render(strings.Join(lines, "\n")) + "\n"

	// Below the box, so a long path does not break it
	dir := ss.Store.Dir
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	view += " " + openStyle.Render("↳ Saved to ") + textStyle.Render(dir) + "\n"
	if stopped {
		view += " " + mutedStyle.Render("  Run the checker again and choose Resume to continue") + "\n"
	}
	return view
}
