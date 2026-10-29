package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/session"
)

type (
	resultMsg check.Result
	noticeMsg string
	doneMsg   struct {
		stopped bool // Domains are left
		err     error
	}
	tickMsg time.Time
)

// progressModel shows a live progress bar and prints results as they arrive
type progressModel struct {
	ss          *session.Session
	blockedOnly bool
	checked     int // Results of this session
	bar         progress.Model
	spinner     spinner.Model
	start       time.Time
	cancel      context.CancelFunc
	stopping    bool // Waiting for the running checks to finish
	force       bool // Quit without waiting
	done        *doneMsg
}

func newProgressModel(ss *session.Session, blockedOnly bool, cancel context.CancelFunc) *progressModel {
	return &progressModel{
		ss:          ss,
		blockedOnly: blockedOnly,
		bar:         progress.New(progress.WithSolidFill("#58A6FF"), progress.WithWidth(50)),
		spinner:     spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(titleStyle)),
		start:       time.Now(),
		cancel:      cancel,
	}
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *progressModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tick())
}

func (m *progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			if m.stopping {
				m.force = true
				return m, tea.Quit
			}
			m.stopping = true
			m.cancel()
		}

	case tea.WindowSizeMsg:
		m.bar.Width = max(10, min(msg.Width-12, 60))

	case resultMsg:
		r := check.Result(msg)
		m.checked++
		if !m.blockedOnly || r.Status == check.StatusBlocked {
			return m, tea.Println(resultLine(r))
		}

	case noticeMsg:
		return m, tea.Println(" " + manualStyle.Render("! ") + mutedStyle.Render(string(msg)))

	case doneMsg:
		m.done = &msg
		return m, tea.Quit

	case tickMsg:
		return m, tick()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *progressModel) View() string {
	if m.done != nil {
		return ""
	}

	todo := m.ss.Todo
	elapsed := time.Since(m.start)
	speed := float64(m.checked) / elapsed.Seconds()

	eta := "–"
	if speed > 0 && m.checked < todo {
		eta = duration(time.Duration(float64(todo-m.checked) / speed * float64(time.Second)))
	}

	title := "Checking domains…"
	if m.ss.Recheck != nil {
		names := make([]string, len(m.ss.Recheck.Categories))
		for i, c := range m.ss.Recheck.Categories {
			names[i] = string(c)
		}
		title = "Rechecking " + strings.Join(names, ", ") + "…"
	}
	title = titleStyle.Render(title)
	if m.stopping {
		title = manualStyle.Render("Stopping: finishing the running checks, up to 20s…")
	}

	stats := mutedStyle.Render(fmt.Sprintf("%s / %s  ·  %.1f per second  ·  elapsed %s  ·  remaining %s",
		number(m.checked), number(todo), speed, duration(elapsed), eta))

	help := "press q to stop, you can resume later"
	if m.stopping {
		help = "press q again to quit now, the running checks are done again when you resume"
	}

	var b strings.Builder
	b.WriteString("\n " + m.spinner.View() + " " + title + "\n\n")
	b.WriteString(" " + m.bar.ViewAs(float64(m.checked)/float64(max(todo, 1))) + "\n")
	b.WriteString(" " + stats + "\n\n")
	b.WriteString(" " + countsLine(m.ss.Writer.Summary()) + "\n\n")
	b.WriteString(" " + mutedStyle.Render(help) + "\n")
	return b.String()
}
