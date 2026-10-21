package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/update"
)

func TestRestartStateRoundTrip(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	m.installed = "9.9.9"
	next, cmd := m.onRestart()
	m = next.(Model)
	state, ok := m.RestartState()
	if !ok || state != "page=1,tab=0,issue=DEMO-42" {
		t.Fatalf("restart state %q (%v)", state, ok)
	}
	if cmd == nil {
		t.Fatal("restarting should quit")
	}

	fresh := New(&fakeJira{issue: sampleIssue()}, sampleFeed(), 0).Restore("page=0,tab=1,issue=DEMO-42")
	if fresh.page != pageIssues || fresh.issues.tab != tabReported || fresh.restoreIssue != "DEMO-42" {
		t.Errorf("restored page %d, tab %d, issue %q", fresh.page, fresh.issues.tab, fresh.restoreIssue)
	}
	if bad := New(&fakeJira{}, sampleFeed(), 0).Restore("page=9,tab=-1,issue=nonsense"); bad.page != pageIssues || bad.restoreIssue != "" {
		t.Errorf("bad state was taken: page %d, issue %q", bad.page, bad.restoreIssue)
	}
}

func TestRestartedLatuiReopensTheIssue(t *testing.T) {
	m := New(&fakeJira{issue: sampleIssue()}, sampleFeed(), 0).Restore("page=1,tab=0,issue=DEMO-42")
	m = run(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = run(t, m, m.start()())
	if m.issue == nil || m.issue.key != "DEMO-42" {
		t.Fatalf("the issue was not reopened")
	}
}

func TestUpdateAndRestartPromptsAfterInstall(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m.updateRestart = true
	next, _ := m.onUpdateDone(updateDoneMsg{version: "9.9.9"})
	m = next.(Model)
	c, ok := m.modal.(*confirmModal)
	if !ok || !c.yes || !strings.Contains(c.prompt, "Restart latui now?") {
		t.Fatalf("no restart question with Yes focused after the install: %T", m.modal)
	}
	_, cmd := m.Update(key("enter"))
	if _, ok := cmd().(restartMsg); !ok {
		t.Error("enter on Yes should restart")
	}
}

func TestUpdateOnlyAndBusyEditorSkipThePrompt(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	next, _ := m.onUpdateDone(updateDoneMsg{version: "9.9.9"})
	m = next.(Model)
	if m.modal != nil {
		t.Error("Update only asked to restart")
	}
	if note := m.updateNote(); !strings.Contains(note, "U restart") {
		t.Errorf("title note %q", note)
	}
	m = run(t, m, key("U")) // later: U asks
	if c, ok := m.modal.(*confirmModal); !ok || !strings.Contains(c.prompt, "Restart latui now?") {
		t.Fatalf("U after an install should ask to restart: %T", m.modal)
	}

	busy := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	busy = run(t, busy, key("n")) // the create form is open: the user is typing
	busy.updateRestart = true
	next, _ = busy.onUpdateDone(updateDoneMsg{version: "9.9.9"})
	if _, ok := next.(Model).modal.(*createModal); !ok {
		t.Errorf("the restart question replaced the form: %T", next.(Model).modal)
	}
}

func TestUpdateDialogButtonsAndClick(t *testing.T) {
	was := installUpdate
	installUpdate = func(context.Context, update.Release, string, func(update.Step)) error { return nil }
	defer func() { installUpdate = was }()
	open := func() Model {
		m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
		m.modal = &updateModal{releases: []update.Release{{Tag: "v9.9.9"}}, exe: "latui", how: update.ByScript}
		if !strings.Contains(screen(m), "y Update and restart") {
			t.Fatalf("dialog lacks Update and restart:\n%s", screen(m))
		}
		return m
	}
	m := clickText(t, open(), "Update only")
	if m.installed != "9.9.9" || m.modal != nil {
		t.Errorf("Update only: installed %q, modal %T", m.installed, m.modal)
	}
	m = clickText(t, open(), "Update and restart")
	if c, ok := m.modal.(*confirmModal); !ok || !strings.Contains(c.prompt, "Restart latui now?") {
		t.Errorf("Update and restart did not ask to restart: %T", m.modal)
	}
}

func TestClickTitleNoteAsksToRestart(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m.installed = "9.9.9"
	m = clickText(t, m, "installed, U restart")
	if c, ok := m.modal.(*confirmModal); !ok || !strings.Contains(c.prompt, "Restart latui now?") {
		t.Errorf("clicking the note did not ask to restart: %T", m.modal)
	}
}
