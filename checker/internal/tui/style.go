package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
	"github.com/armanlife/iran-blocked-domains/checker/internal/runner"
)

var (
	colorAccent  = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	colorBlocked = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	colorOpen    = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	colorManual  = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}
	colorText    = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}
	colorBorder  = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	colorButton  = lipgloss.AdaptiveColor{Light: "#EAEEF2", Dark: "#21262D"}

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	mutedStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	textStyle    = lipgloss.NewStyle().Foreground(colorText)
	blockedStyle = lipgloss.NewStyle().Foreground(colorBlocked).Bold(true)
	openStyle    = lipgloss.NewStyle().Foreground(colorOpen).Bold(true)
	manualStyle  = lipgloss.NewStyle().Foreground(colorManual).Bold(true)
	methodStyle  = lipgloss.NewStyle().Foreground(colorAccent)
	errorStyle   = lipgloss.NewStyle().Foreground(colorBlocked)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 3)
)

// theme styles the questions with the same colors as the rest of the app
func theme() *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Base = t.Focused.Base.BorderForeground(colorAccent)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(colorText).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(colorText).Bold(true).MarginBottom(1)
	t.Focused.Description = t.Focused.Description.Foreground(colorMuted)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(colorBlocked)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(colorBlocked)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(colorAccent).SetString("› ")
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(colorAccent)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(colorAccent)
	t.Focused.Option = t.Focused.Option.Foreground(colorText)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colorAccent)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("#FFFFFF")).Background(colorAccent).Bold(true)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(colorText).Background(colorButton)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colorAccent)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(colorMuted)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colorAccent)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = t.Focused.Title.Foreground(colorMuted)
	t.Blurred.SelectSelector = t.Focused.SelectSelector.Foreground(colorMuted)
	t.Blurred.Option = t.Focused.Option.Foreground(colorMuted)
	t.Blurred.SelectedOption = t.Focused.SelectedOption.Foreground(colorMuted)
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	t.Help.ShortKey = t.Help.ShortKey.Foreground(colorMuted)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(colorMuted)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(colorBorder)
	return t
}

func banner() string {
	title := titleStyle.Render("Iran Blocked Domains") + mutedStyle.Render("  ·  ") + textStyle.Bold(true).Render("Checker")
	subtitle := mutedStyle.Render("Detects DNS poisoning and SNI filtering")
	warning := manualStyle.Render("!  ") + mutedStyle.Render("Run inside Iran without VPN, otherwise results are not accurate")

	return "\n" + boxStyle.Render(title+"\n"+subtitle) + "\n " + warning + "\n"
}

// resultLine renders one result for the live output
func resultLine(r check.Result) string {
	var icon, status string
	switch r.Status {
	case check.StatusBlocked:
		icon, status = blockedStyle.Render("✗"), blockedStyle.Render(fmt.Sprintf("%-8s", "blocked"))
	case check.StatusNotBlocked:
		icon, status = openStyle.Render("✓"), openStyle.Render(fmt.Sprintf("%-8s", "open"))
	default:
		icon, status = manualStyle.Render("?"), manualStyle.Render(fmt.Sprintf("%-8s", "manual"))
	}

	line := fmt.Sprintf(" %s %s %s %s", icon, status,
		textStyle.Render(fmt.Sprintf("%-34s", r.Domain)),
		methodStyle.Render(fmt.Sprintf("%-4s", r.Method)))

	// At most two IPs, so the line fits in the terminal
	if ips := r.IPs; len(ips) > 0 {
		shown := strings.Join(ips[:min(len(ips), 2)], ", ")
		if len(ips) > 2 {
			shown += fmt.Sprintf(" +%d", len(ips)-2)
		}
		line += " " + mutedStyle.Render(shown)
	}
	if r.Reason != "" {
		line += " " + mutedStyle.Italic(true).Render(r.Reason)
	}
	return line
}

// categoryLabels are the display names of categories
var categoryLabels = map[check.Category]string{
	check.CategoryBlockedDNS:    "by DNS",
	check.CategoryBlockedSNI:    "by SNI",
	check.CategoryNotBlocked:    "not blocked",
	check.CategoryDNSTimeout:    "DNS timeout",
	check.CategoryDNSNotFound:   "not found",
	check.CategoryDNSError:      "DNS error",
	check.CategoryConnectFailed: "connect failed",
	check.CategoryTLSFailed:     "TLS failed",
}

func countsLine(s *runner.Summary) string {
	return fmt.Sprintf("%s %s   %s   %s",
		blockedStyle.Render(fmt.Sprintf("● %s blocked", number(s.Blocked))),
		mutedStyle.Render(fmt.Sprintf("(dns %s · sni %s)", number(s.ByCategory[check.CategoryBlockedDNS]), number(s.ByCategory[check.CategoryBlockedSNI]))),
		openStyle.Render(fmt.Sprintf("● %s open", number(s.NotBlocked))),
		manualStyle.Render(fmt.Sprintf("● %s manual", number(s.Manual))),
	)
}

// number formats n with thousands separators
func number(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// size formats a file size in bytes
func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// duration formats an estimated duration like "5h 33m"
func duration(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
