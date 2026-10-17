package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/khanhtd36/latui/internal/jira"
)

func ago(d time.Duration) string { return time.Now().Add(-d).Format("2006-01-02T15:04:05.000-0700") }

func issuesJira() *fakeJira {
	st := func(name, cat string) jira.Status {
		s := jira.Status{Name: name}
		s.Category.Key = cat
		return s
	}
	return &fakeJira{issue: sampleIssue(), rows: map[string][]jira.Row{
		"assignee": {
			{Key: "QA-9", Summary: "Deferred one", Status: st("Deferred", "new"), IssueType: "Bug", Updated: ago(time.Hour)},
			{Key: "QA-7", Summary: "Older work", Status: st("In Progress", "indeterminate"), IssueType: "Bug", Priority: "High", Updated: ago(48 * time.Hour)},
			{Key: "DEMO-3", Summary: "Fresh work", Status: st("In Progress", "indeterminate"), IssueType: "Task", Updated: ago(time.Hour), Parent: "DEMO-1"},
			{Key: "QA-5", Summary: "Next up", Status: st("To Do", "new"), IssueType: "Improvement", Updated: ago(3 * time.Hour)},
		},
		"reporter":  {{Key: "QA-20", Summary: "Reported bug", Status: st("Open", "new"), IssueType: "Bug", Updated: ago(time.Hour)}},
		"filter-qa": {{Key: "QA-30", Summary: "From the filter", Status: st("Fixed", "indeterminate"), IssueType: "Bug", Updated: ago(time.Hour)}},
	}}
}

func TestMyIssuesSections(t *testing.T) {
	j := issuesJira()
	m := started(t, j, sampleFeed())
	m = run(t, m, key("1"))
	s := screen(m)
	for _, want := range []string{"1 My issues (4)", "Assigned 4", "Reported", "▾ In Progress (2)", "▾ To Do (1)", "▸ Deferred (1)"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Deferred one") {
		t.Error("Deferred should start collapsed")
	}
	if strings.Index(s, "Fresh work") > strings.Index(s, "Older work") {
		t.Error("newest updated should come first in a Section")
	}
	// Open the Deferred Section: jump to it and press enter.
	m = run(t, m, key("J"))
	m = run(t, m, key("J"))
	m = run(t, m, key("enter"))
	if !strings.Contains(screen(m), "Deferred one") {
		t.Errorf("enter on the header should open Deferred:\n%s", screen(m))
	}
	m = run(t, m, key("tab"))
	if !strings.Contains(screen(m), "Reported bug") {
		t.Errorf("Reported tab:\n%s", screen(m))
	}
	m = run(t, m, key("f"))
	m = run(t, m, key("enter"))
	if s := screen(m); !strings.Contains(s, "filter: QA tickets to verify") || !strings.Contains(s, "From the filter") {
		t.Errorf("favourite filter:\n%s", s)
	}
	m = run(t, m, key("esc"))
	if strings.Contains(screen(m), "From the filter") {
		t.Error("esc should leave the filter")
	}
}

func TestRowActionRunsIssueKey(t *testing.T) {
	j := issuesJira()
	m := started(t, j, sampleFeed())
	m = run(t, m, key("1"))
	m = run(t, m, key("j")) // the first issue under In Progress
	m = run(t, m, key("A"))
	if _, ok := m.modal.(*pickerModal); !ok {
		t.Fatalf("assignee picker not open: %T", m.modal)
	}
	m = run(t, m, key("enter")) // Me
	if j.assigned == nil || *j.assigned != "me" {
		t.Errorf("assigned %v", j.assigned)
	}
	if m.issue != nil {
		t.Error("a row action should not open Issue detail")
	}
}

func TestCreateAnotherKeepsFields(t *testing.T) {
	j := issuesJira()
	m := started(t, j, sampleFeed())
	m = run(t, m, key("n"))
	c, ok := m.modal.(*createModal)
	if !ok {
		t.Fatalf("create form not open: %T", m.modal)
	}
	if c.project != "QA" || c.typeOf(c.typeID).Name != "Bug" {
		t.Fatalf("defaults: project %q type %q", c.project, c.typeID)
	}
	if s := screen(m); !strings.Contains(s, "Components") || !strings.Contains(s, "Automatic") {
		t.Errorf("form:\n%s", s)
	}
	// Labels: pick a suggestion.
	c.setFocus(focusChips + chipLabels)
	m = run(t, m, key("enter"))
	m = run(t, m, key("down"))
	m = run(t, m, key("enter"))
	m = run(t, m, key("ctrl+s"))
	if strings.Join(c.labels, ",") != "exports" {
		t.Fatalf("labels %v", c.labels)
	}
	m = run(t, m, key("ctrl+a")) // Create another
	c.setFocus(focusSummary)
	for _, r := range "First" {
		m = run(t, m, key(string(r)))
	}
	m = run(t, m, key("ctrl+s"))
	if m.modal != c {
		t.Fatal("Create another should keep the form open")
	}
	if c.summary.Value() != "" || strings.Join(c.labels, ",") != "exports" {
		t.Errorf("after create: summary %q labels %v", c.summary.Value(), c.labels)
	}
	for _, r := range "Second" {
		m = run(t, m, key(string(r)))
	}
	m = run(t, m, key("ctrl+s"))
	if len(j.created) != 2 || j.created[1].Summary != "Second" || strings.Join(j.created[1].Labels, ",") != "exports" || j.created[1].Assignee != "" {
		t.Fatalf("created %+v", j.created)
	}
	if !strings.Contains(screen(m), "Created: QA-1, QA-2") {
		t.Errorf("created list:\n%s", screen(m))
	}
	m = run(t, m, key("esc"))
	if m.modal != nil || !strings.Contains(m.status, "created 2 issues") {
		t.Errorf("closing: modal %T status %q", m.modal, m.status)
	}
	if !m.prefs.CreateAnother || m.prefs.LastProject != "QA" {
		t.Errorf("prefs %+v", m.prefs)
	}
}

func TestCreateOpensIssueAndMovesStatus(t *testing.T) {
	j := issuesJira()
	m := started(t, j, sampleFeed())
	m.prefs.CreateAnother = false
	m = run(t, m, key("n"))
	c := m.modal.(*createModal)
	c.status = "In Progress"
	for _, r := range "Solo" {
		m = run(t, m, key(string(r)))
	}
	m = run(t, m, key("ctrl+s"))
	if m.modal != nil || m.issue == nil || m.issue.key != "QA-1" {
		t.Fatalf("should open the new issue: modal %T issue %+v", m.modal, m.issue)
	}
	if j.moved != "QA-1→In Progress" {
		t.Errorf("moved %q", j.moved)
	}
}

func TestNewSubtaskFromIssue(t *testing.T) {
	j := issuesJira()
	m := started(t, j, sampleFeed())
	m = run(t, m, openIssueMsg{key: "QA-7"})
	m = run(t, m, key("n"))
	m = run(t, m, key("j"))
	m = run(t, m, key("enter"))
	c, ok := m.modal.(*createModal)
	if !ok {
		t.Fatalf("form not open: %T", m.modal)
	}
	if c.parent != "QA-7" || !c.typeOf(c.typeID).Subtask || c.project != "QA" {
		t.Errorf("subtask form: parent %q type %+v project %q", c.parent, c.typeOf(c.typeID), c.project)
	}
}
