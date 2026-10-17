package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/latui/internal/adf"
	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
	"github.com/khanhtd36/latui/internal/prefs"
)

type fakeJira struct {
	issue      *jira.Issue
	labels     []string
	assigned   *string
	comment    string
	transition string
	rows       map[string][]jira.Row // by JQL prefix
	created    []jira.NewIssue
	moved      string
}

func (f *fakeJira) Site(context.Context) (auth.Credential, error) {
	return auth.Credential{Site: "x.atlassian.net", CloudID: "c"}, nil
}

func (f *fakeJira) Myself(context.Context) (jira.User, error) {
	return jira.User{AccountID: "me", DisplayName: "Alex Kim"}, nil
}

func (f *fakeJira) Issue(_ context.Context, key string) (*jira.Issue, error) {
	is := *f.issue
	is.Key = key
	return &is, nil
}

func (f *fakeJira) Comments(context.Context, string) ([]jira.Comment, error) {
	return []jira.Comment{{Author: jira.User{DisplayName: "Sam Rivera"}, Created: "2026-10-16T03:38:53.095-0500", Body: adf.FromMarkdown("Looks **good**")}}, nil
}

func (f *fakeJira) History(context.Context, string) ([]jira.HistoryEntry, error) {
	return []jira.HistoryEntry{{Author: jira.User{DisplayName: "Dat"}, Created: "2026-10-15T01:48:53.789-0500", Changes: []jira.Change{{Field: "status", From: "To Do", To: "In Progress"}}}}, nil
}

func (f *fakeJira) Transitions(context.Context, string) ([]jira.Transition, error) {
	return []jira.Transition{{ID: "31", Name: "Done", To: jira.Status{Name: "Done"}, NeedsResolution: true, Resolutions: []string{"Fixed", "Won't Do"}}}, nil
}

func (f *fakeJira) Transition(_ context.Context, _, id, res string) error {
	f.transition = id + ":" + res
	return nil
}

func (f *fakeJira) AddComment(_ context.Context, _, md string) error {
	f.comment = md
	return nil
}
func (f *fakeJira) SetSummary(context.Context, string, string) error { return nil }
func (f *fakeJira) SetLabels(_ context.Context, _ string, ls []string) error {
	f.labels = ls
	return nil
}
func (f *fakeJira) SetComponents(context.Context, string, []string) error { return nil }
func (f *fakeJira) Assign(_ context.Context, _, id string) error {
	f.assigned = &id
	return nil
}

func (f *fakeJira) AssignableUsers(context.Context, string, string) ([]jira.User, error) {
	return []jira.User{{AccountID: "u2", DisplayName: "Lee Park"}}, nil
}

func (f *fakeJira) LabelSuggestions(context.Context, string) ([]string, error) {
	return []string{"performance", "exports"}, nil
}

type fakeFeed struct {
	groups  map[notify.Category][]notify.Group
	marked  []string
	allRead notify.Category
}

func (f *fakeFeed) Feed(_ context.Context, cat notify.Category, unread bool, _ int, _ string) (notify.Page, error) {
	var out []notify.Group
	for _, g := range f.groups[cat] {
		if !unread || g.Unread {
			out = append(out, g)
		}
	}
	return notify.Page{Groups: out}, nil
}

func (f *fakeFeed) MarkGroup(_ context.Context, g notify.Group, read bool) error {
	f.marked = append(f.marked, g.ID+map[bool]string{true: ":read", false: ":unread"}[read])
	return nil
}

func (f *fakeFeed) MarkAllRead(_ context.Context, cat notify.Category) error {
	f.allRead = cat
	return nil
}

func sampleFeed() *fakeFeed {
	now := time.Now()
	return &fakeFeed{groups: map[notify.Category][]notify.Group{
		notify.Direct: {
			{ID: "g1", Size: 3, Unread: true, Head: notify.Notification{
				Time: now.Add(-15 * time.Minute), Unread: true, Product: notify.Confluence,
				Message: "Priya Shah mentioned you on a page", Title: "Release checklist 4.2",
				Context: "Quality Assurance", URL: "https://x.atlassian.net/wiki/spaces/QA/pages/1",
			}},
			{ID: "g2", Size: 1, Head: notify.Notification{
				Time: now.Add(-27 * time.Minute), Product: notify.Jira, Key: "DEMO-42", Status: "In Progress",
				Message: "Jo Tanaka updated a subtask", Title: "Stream large exports instead of buffering them",
				Snippet: "Memory stays flat with streaming, the 1M row sample finished in 40 seconds",
			}},
		},
		notify.Watching: {
			{ID: "w1", Unread: true, Head: notify.Notification{Time: now.Add(-48 * time.Hour), Product: notify.Jira, Key: "QA-1", Message: "Someone commented on a bug", Title: "Crash"}},
		},
	}}
}

func sampleIssue() *jira.Issue {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Exports run out of memory."}]}]}`
	return &jira.Issue{
		Fields: jira.Fields{
			Summary: "Stream large exports", Status: jira.Status{Name: "In Progress"},
			Labels:      []string{"qa"},
			Description: []byte(doc),
			Parent:      &jira.LinkedIssue{Key: "DEMO-1"},
		},
		EditMeta: map[string]jira.EditField{"labels": {}, "assignee": {}, "summary": {}},
	}
}

// run feeds msg to the model and runs every command it returns, and the
// commands those return, the way the bubbletea runtime would (ticks and
// update checks aside).
func run(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		next, cmd := m.Update(queue[0])
		m = next.(Model)
		queue = append(queue[1:], drain(cmd)...)
	}
	return m
}

func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, drain(c)...)
		}
		return out
	case tickMsg, updatesMsg, statusMsg:
		if s, ok := msg.(statusMsg); ok {
			return []tea.Msg{s}
		}
		return nil
	case nil:
		return nil
	}
	return []tea.Msg{msg}
}

func key(k string) tea.Msg { return keyMsg(k) }

func init() {
	tickAfter = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	prefs.SetFile(filepath.Join(os.TempDir(), fmt.Sprintf("latui-test-%d.json", os.Getpid())))
}

func started(t *testing.T, j *fakeJira, f *fakeFeed) Model {
	t.Helper()
	m := New(j, f, time.Minute)
	m = run(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = run(t, m, m.start()())
	return run(t, m, key("2")) // most tests here are about Notifications
}

func screen(m Model) string { return ansi.Strip(m.View()) }

func TestNotificationsShowGroups(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	s := screen(m)
	for _, want := range []string{
		"2 Notifications (1)", "Direct 1", "Watching 1", "Today",
		"Priya Shah mentioned you on a page", "+2", "Confluence · Quality Assurance",
		"DEMO-42 · In Progress", "│ Memory stays flat with streaming",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestUnreadFilterAndMarking(t *testing.T) {
	f := sampleFeed()
	m := started(t, &fakeJira{issue: sampleIssue()}, f)
	m = run(t, m, key("u"))
	if s := screen(m); strings.Contains(s, "Jo Tanaka") || !strings.Contains(s, "Priya Shah") {
		t.Fatalf("unread filter should hide read groups:\n%s", s)
	}
	m = run(t, m, key("m"))
	if len(f.marked) != 1 || f.marked[0] != "g1:read" {
		t.Errorf("marked %v", f.marked)
	}
	m = run(t, m, key("M"))
	m = run(t, m, key("y"))
	if f.allRead != notify.Direct {
		t.Errorf("mark all read went to %q", f.allRead)
	}
	_ = m
}

func TestTabSwitchesToWatching(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("tab"))
	if s := screen(m); !strings.Contains(s, "Someone commented on a bug") {
		t.Fatalf("watching tab:\n%s", s)
	}
}

func TestEnterOpensIssueDetail(t *testing.T) {
	f := sampleFeed()
	m := started(t, &fakeJira{issue: sampleIssue()}, f)
	m = run(t, m, key("j"))
	m = run(t, m, key("enter"))
	if m.issue == nil || m.issue.key != "DEMO-42" {
		t.Fatalf("issue not open")
	}
	s := screen(m)
	for _, want := range []string{"DEMO-42", "Stream large exports", "IN PROGRESS", "Exports run out of memory.", "Sam Rivera", "Looks good", "Labels", "qa"} {
		if !strings.Contains(s, want) {
			t.Errorf("issue screen lacks %q:\n%s", want, s)
		}
	}
	m = run(t, m, key("2"))
	if s := screen(m); !strings.Contains(s, "status: To Do → In Progress") {
		t.Errorf("history tab:\n%s", s)
	}
	m = run(t, m, key("esc"))
	if m.issue != nil {
		t.Error("esc should go back to the notifications")
	}
}

func TestIssueEdits(t *testing.T) {
	j := &fakeJira{issue: sampleIssue()}
	m := started(t, j, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})

	// Transition with a resolution.
	m = run(t, m, key("t"))
	m = run(t, m, key("enter")) // Done
	m = run(t, m, key("enter")) // Fixed
	if j.transition != "31:Fixed" {
		t.Errorf("transition %q", j.transition)
	}

	// Labels: add a suggestion, keep qa.
	m = run(t, m, key("l"))
	if _, ok := m.modal.(*pickerModal); !ok {
		t.Fatalf("labels picker not open: %T", m.modal)
	}
	m = run(t, m, key("down")) // qa → performance
	m = run(t, m, key("enter"))
	m = run(t, m, key("ctrl+s"))
	if strings.Join(j.labels, ",") != "qa,performance" {
		t.Errorf("labels %v", j.labels)
	}

	// Assign to me.
	m = run(t, m, key("A"))
	m = run(t, m, key("enter"))
	if j.assigned == nil || *j.assigned != "me" {
		t.Errorf("assigned %v", j.assigned)
	}

	// Comment.
	m = run(t, m, key("c"))
	for _, r := range "hello" {
		m = run(t, m, key(string(r)))
	}
	m = run(t, m, key("ctrl+s"))
	if j.comment != "hello" {
		t.Errorf("comment %q", j.comment)
	}
	if !strings.Contains(m.status, "commented on DEMO-42") {
		t.Errorf("status %q", m.status)
	}
}

func TestGoToIssue(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("ctrl+g"))
	for _, r := range "qa-12" {
		m = run(t, m, key(string(r)))
	}
	m = run(t, m, key("enter"))
	if m.issue == nil || m.issue.key != "QA-12" {
		t.Fatalf("go to issue failed: %+v", m.issue)
	}
}
