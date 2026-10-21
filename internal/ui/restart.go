package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/jira"
)

// Restarting into an installed update: latui quits and main starts the new
// copy where this one was, as "page=1,tab=0,issue=DEMO-42".

// restartMsg is Yes to restarting into the installed update.
type restartMsg struct{}

// restoreState is where a restarted latui goes back to; issue is empty when
// no issue was open.
type restoreState struct {
	page, tab int
	issue     string
}

// RestartState is what to hand the restarted latui once the user asked to
// restart into an installed update; ok is false on a plain quit.
func (m Model) RestartState() (state string, ok bool) {
	return m.restartState, m.restartState != ""
}

// place is where this latui is: the page and its tab, and the issue open.
func (m Model) place() string {
	tab := m.notes.tab
	if m.page == pageIssues {
		tab = m.issues.tab
	}
	parts := []string{"page=" + strconv.Itoa(m.page), "tab=" + strconv.Itoa(tab)}
	if m.issue != nil {
		parts = append(parts, "issue="+m.issue.key)
	}
	return strings.Join(parts, ",")
}

// Restore puts a restarted latui back on the page, tab and issue the last
// one showed; the issue opens once the site is known.
func (m Model) Restore(state string) Model {
	var tab int
	hasTab := false
	for _, kv := range strings.Split(state, ",") {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "page":
			if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < pageCount {
				m.page = n
			}
		case "tab":
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				tab, hasTab = n, true
			}
		case "issue":
			if key, ok := jira.ParseKey(v, ""); ok {
				m.restore.issue = key
			}
		}
	}
	if hasTab {
		if m.page == pageIssues && tab < issueTabCount {
			m.issues.tab = tab
		} else if m.page == pageNotifications && tab < len(categories) {
			m.notes.tab = tab
		}
	}
	return m
}

// restartPrompt asks before restarting into the installed update, with Yes
// focused. It is only asked with no form or editor open, so nothing typed
// is lost.
func (m Model) restartPrompt() modal {
	c := newConfirm(fmt.Sprintf("v%s is installed. Restart latui now?", m.installed), func() tea.Msg { return restartMsg{} })
	c.yes = true
	return c
}

func (m Model) onRestart() (tea.Model, tea.Cmd) {
	m.restartState = m.place()
	return m, tea.Quit
}
