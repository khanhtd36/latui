package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleHeader    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleCursor    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleGreen     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleYellow    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleRed       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleCyan      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	stylePurple    = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	styleSection   = lipgloss.NewStyle().Bold(true)
	styleSelected  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	styleButton    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238")).Padding(0, 1)
	styleButtonCTA = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("25")).Padding(0, 1)
)

// Status category colors, as Jira draws its lozenges.
func statusStyle(category string) lipgloss.Style {
	switch category {
	case "done":
		return styleGreen
	case "indeterminate":
		return styleCyan
	}
	return styleDim
}

// The unread marker is a dot when the terminal draws it one column wide.
const symUnread = "●"

// SymbolSample holds every marker symbol, to measure how wide the terminal
// draws them; each should take one column.
const SymbolSample = symUnread + "▌▸"

var useSymbols bool

// UseSymbols switches markers between symbols and ASCII.
func UseSymbols(on bool) { useSymbols = on }

func unreadMark() string { return pick2(useSymbols, symUnread, "*") }

type statusMsg string

func statusCmd(s string) tea.Cmd { return func() tea.Msg { return statusMsg(s) } }

// resultMsg is "error: …" for a failure, ok otherwise.
func resultMsg(err error, ok string) statusMsg {
	if err != nil {
		return statusMsg("error: " + err.Error())
	}
	return statusMsg(ok)
}

func pick2(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// relTime is "5m ago", "3h ago", "2d ago", then a date.
func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	if t.Year() == time.Now().Year() {
		return t.Local().Format("Jan 2")
	}
	return t.Local().Format("Jan 2, 2006")
}

// truncate fits s on one line of w cells: line breaks become spaces, as a
// break inside a row would push the whole screen down.
func truncate(s string, w int) string {
	if strings.ContainsAny(s, "\r\n") {
		s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	}
	return ansi.Truncate(screenSafe(s), max(0, w), "…")
}

// fit truncates or pads to exactly w cells.
func fit(s string, w int) string { return padRight(truncate(s, w), w) }

const tabWidth = 4

// screenSafe keeps text from Jira from drawing wider than it is counted:
// tabs become spaces and other control characters "?"; escape sequences
// (styling) and newlines stay.
func screenSafe(s string) string {
	if !strings.ContainsFunc(s, isUnsafeControl) {
		return s
	}
	s = strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
	return strings.Map(func(r rune) rune {
		if isUnsafeControl(r) {
			return '?'
		}
		return r
	}, s)
}

func isUnsafeControl(r rune) bool {
	return (r < 0x20 || r == 0x7f) && r != '\n' && r != 0x1b
}

func padRight(s string, w int) string {
	if n := ansi.StringWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func padLines(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:max(0, h)]
}

// wrap breaks plain text into lines of at most w cells.
func wrap(s string, w int) []string {
	if w < 1 {
		return nil
	}
	var out []string
	for para := range strings.SplitSeq(screenSafe(s), "\n") {
		out = append(out, strings.Split(ansi.Hardwrap(ansi.Wordwrap(para, w, ""), w, true), "\n")...)
	}
	return out
}
