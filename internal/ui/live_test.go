package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
)

// TestLive drives the real app against the signed-in site, read-only:
// LATUI_LIVE=1 loads the Notifications page and the first Jira issue's
// detail, LATUI_LIVE_DIR saves the screens there.
func TestLive(t *testing.T) {
	if os.Getenv("LATUI_LIVE") == "" {
		t.Skip("set LATUI_LIVE=1 to run against the real site")
	}
	creds := auth.Default(os.Getenv)
	m := New(jira.New(creds), notify.New(creds), time.Minute)
	m = run(t, m, tea.WindowSizeMsg{Width: 150, Height: 45})
	m = run(t, m, m.start()())
	if m.startErr != nil {
		t.Fatal(m.startErr)
	}
	if v := m.notes.view(); v.err != nil {
		t.Fatal(v.err)
	}
	save(t, "notifications", m.View())
	m = run(t, m, key("tab"))
	save(t, "watching", m.View())
	m = run(t, m, key("tab"))
	for i, g := range m.notes.view().groups {
		if g.Head.Key == "" {
			continue
		}
		m.notes.view().cursor = i
		m = run(t, m, openIssueMsg{key: g.Head.Key})
		if m.issue.err != nil {
			t.Fatal(m.issue.err)
		}
		save(t, "issue", m.View())
		m = run(t, m, key("2"))
		save(t, "history", m.View())
		break
	}
}

func save(t *testing.T, name, view string) {
	t.Helper()
	dir := os.Getenv("LATUI_LIVE_DIR")
	if dir == "" {
		t.Logf("%s:\n%s", name, ansi.Strip(view))
		return
	}
	if err := os.WriteFile(dir+"/"+name+".ans", []byte(view), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/"+name+".txt", []byte(strings.TrimRight(ansi.Strip(view), "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}
