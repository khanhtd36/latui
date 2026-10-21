package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/khanhtd36/latui/internal/notify"
)

func mouseMsg(action tea.MouseAction, x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action}
}

// cellOf is the screen cell where text first appears.
func cellOf(t *testing.T, m Model, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("no %q on screen:\n%s", text, screen(m))
	return 0, 0
}

// click is a press and a release on one cell, drawn first the way the
// runtime draws before it reads input.
func click(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m.View()
	m = run(t, m, mouseMsg(tea.MouseActionPress, x, y))
	return run(t, m, mouseMsg(tea.MouseActionRelease, x, y))
}

func clickText(t *testing.T, m Model, text string) Model {
	t.Helper()
	x, y := cellOf(t, m, text)
	return click(t, m, x+1, y)
}

func TestClickTitleTabs(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = clickText(t, m, "1 My issues")
	if m.page != pageIssues {
		t.Fatalf("clicking My issues stayed on page %d", m.page)
	}
	m = clickText(t, m, "2 Notifications")
	if m.page != pageNotifications {
		t.Errorf("clicking Notifications stayed on page %d", m.page)
	}
}

func TestClickNotificationSelectsThenOpens(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("u")) // show all: the issue's notification is read
	if m.notes.view().cursor != 0 {
		t.Fatalf("cursor starts at %d", m.notes.view().cursor)
	}
	m = clickText(t, m, "Stream large exports")
	if m.issue != nil || m.notes.view().cursor != 1 {
		t.Fatalf("first click: issue open %v, cursor %d", m.issue != nil, m.notes.view().cursor)
	}
	m = clickText(t, m, "Stream large exports")
	if m.issue == nil || m.issue.key != "DEMO-42" {
		t.Errorf("second click did not open the issue")
	}
}

func TestClickFooterHintPressesItsKey(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	if !m.notes.unreadOnly {
		t.Fatal("Unread-only should start on")
	}
	m = clickText(t, m, "u unread")
	if m.notes.unreadOnly {
		t.Error("clicking the unread hint did not press u")
	}
	m = clickText(t, m, "only unread")
	if !m.notes.unreadOnly {
		t.Error("clicking the checkbox did not toggle Unread filter back")
	}
}

func TestClickNotificationTabs(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = clickText(t, m, "Watching")
	if m.notes.tab != 1 {
		t.Errorf("clicking Watching left tab %d", m.notes.tab)
	}
}

func TestDragIsASelectionNotAClick(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("u"))
	x, y := cellOf(t, m, "Stream large exports")
	m.View()
	next, _ := m.Update(mouseMsg(tea.MouseActionPress, x, y))
	m = next.(Model)
	next, _ = m.Update(mouseMsg(tea.MouseActionMotion, x+5, y))
	m = next.(Model)
	if got := ansi.Strip(m.hits.selectedText()); got != "Stream" {
		t.Errorf("dragging 5 cells selected %q", got)
	}
	// The release copies; it is not run here, so the clipboard is untouched.
	next, cmd := m.Update(mouseMsg(tea.MouseActionRelease, x+5, y))
	m = next.(Model)
	if cmd == nil {
		t.Error("releasing a drag should copy")
	}
	if m.notes.view().cursor != 0 || m.issue != nil {
		t.Errorf("a drag acted as a click: cursor %d", m.notes.view().cursor)
	}
}

func TestSelectionStaysInOnePane(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	x, y := cellOf(t, m, "Exports run out of memory.")
	m.View()
	next, _ := m.Update(mouseMsg(tea.MouseActionPress, x, y))
	m = next.(Model)
	// Dragging far right and down, over the Fields sidebar, stops at the
	// Content's edge.
	next, _ = m.Update(mouseMsg(tea.MouseActionMotion, 139, y+3))
	m = next.(Model)
	got := m.hits.selectedText()
	if strings.Contains(got, "Assignee") || strings.Contains(got, "│") {
		t.Errorf("selection reached the sidebar or a frame: %q", got)
	}
	if !strings.Contains(got, "Exports run out of memory.") {
		t.Errorf("selection lacks the description line: %q", got)
	}
}

func TestIssueDetailClicks(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	d := m.issue
	m = clickText(t, m, "Assignee")
	if d.focus != paneFields {
		t.Errorf("clicking the sidebar left focus on pane %d", d.focus)
	}
	m = clickText(t, m, "Exports run out of memory.")
	if d.focus != paneContent {
		t.Errorf("clicking the description left focus on pane %d", d.focus)
	}
	m = clickText(t, m, "3 Links")
	if d.tab != tabLinks {
		t.Errorf("clicking the Links tab left tab %d", d.tab)
	}
	// The parent is the one link row, selected already: a Click opens it.
	m = clickText(t, m, "parent")
	if m.issue == nil || m.issue.key != "DEMO-1" {
		t.Errorf("clicking the selected link row should open DEMO-1")
	}
}

func TestClickEscBackHint(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	m = clickText(t, m, "esc back")
	if m.issue != nil {
		t.Error("clicking the esc back hint stayed in Issue detail")
	}
}

func TestConfirmButtonsAndOutsideClick(t *testing.T) {
	f := sampleFeed()
	m := started(t, &fakeJira{issue: sampleIssue()}, f)
	m = run(t, m, key("M"))
	m = click(t, m, 0, 0) // outside: a question is not answered by a stray click
	if m.modal == nil {
		t.Fatal("an outside click closed the confirm")
	}
	m = clickText(t, m, "n No")
	if m.modal != nil || f.allRead != "" {
		t.Errorf("No: modal %v, allRead %q", m.modal != nil, f.allRead)
	}
	m = run(t, m, key("M"))
	m = clickText(t, m, "y Yes")
	if f.allRead != notify.Direct {
		t.Errorf("Yes marked %q", f.allRead)
	}
}

func TestMenuClickRunsItemAndOutsideCloses(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	m = run(t, m, key("n"))
	if _, ok := m.modal.(*menuModal); !ok {
		t.Fatalf("n did not open the New menu: %T", m.modal)
	}
	m = click(t, m, 0, 0)
	if m.modal != nil {
		t.Fatal("an outside click should close a menu")
	}
	m = run(t, m, key("n"))
	m = clickText(t, m, "New subtask of DEMO-42")
	if _, ok := m.modal.(*createModal); !ok {
		t.Errorf("clicking a menu item did not run it: %T", m.modal)
	}
}

func TestCreateFormClickFocusesThenOpens(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("n"))
	c, ok := m.modal.(*createModal)
	if !ok {
		t.Fatalf("n did not open the form: %T", m.modal)
	}
	c.onAnother = nil // the test must not save the choice for others
	m = clickText(t, m, "Create another")
	if !c.another {
		t.Error("clicking Create another did not toggle it")
	}
	x, y := cellOf(t, m, "Summary")
	m = click(t, m, x+12, y)
	if c.focus != focusSummary {
		t.Errorf("clicking the summary left focus %d", c.focus)
	}
}

func TestMyIssuesClicks(t *testing.T) {
	m := started(t, issuesJira(), sampleFeed())
	m = run(t, m, key("1"))
	m = clickText(t, m, "Reported")
	if m.issues.tab != tabReported || !strings.Contains(screen(m), "Reported bug") {
		t.Fatalf("clicking Reported left tab %d:\n%s", m.issues.tab, screen(m))
	}
	m = clickText(t, m, "Assigned")
	// A Section header opens or closes on one click.
	m = clickText(t, m, "▸ Deferred")
	if !strings.Contains(screen(m), "Deferred one") {
		t.Fatalf("clicking the Deferred header did not open it:\n%s", screen(m))
	}
	m = clickText(t, m, "▾ Deferred")
	if strings.Contains(screen(m), "Deferred one") {
		t.Error("clicking the header again did not close it")
	}
	// A row selects, then opens.
	m = clickText(t, m, "Next up")
	if m.issue != nil {
		t.Fatal("the first click opened the issue")
	}
	m = clickText(t, m, "Next up")
	if m.issue == nil || m.issue.key != "QA-5" {
		t.Errorf("the second click did not open QA-5")
	}
}

func TestPickerClickPicks(t *testing.T) {
	j := issuesJira()
	m := started(t, j, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	m = run(t, m, key("A"))
	if _, ok := m.modal.(*pickerModal); !ok {
		t.Fatalf("A did not open the assignee picker: %T", m.modal)
	}
	m = clickText(t, m, "Unassigned")
	if m.modal != nil || j.assigned == nil || *j.assigned != "" {
		t.Errorf("clicking Unassigned: modal %v, assigned %v", m.modal != nil, j.assigned)
	}
}

func TestHelpClosesOnOutsideClick(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("?"))
	if m.help == nil {
		t.Fatal("? did not open help")
	}
	m = click(t, m, 0, 0)
	if m.help != nil {
		t.Error("an outside click should close help")
	}
}

func TestSelectionSpan(t *testing.T) {
	s := selection{area: rect{2, 5, 10, 4}, ax: 6, ay: 6, bx: 4, by: 8}
	for _, c := range []struct {
		y, from, to int
		ok          bool
	}{
		{5, 0, 0, false},
		{6, 6, 12, true}, // the first row, from the anchor to the area's edge
		{7, 2, 12, true}, // a middle row, all of it
		{8, 2, 5, true},  // the last row, up to the end cell
		{9, 0, 0, false},
	} {
		from, to, ok := s.span(c.y)
		if ok != c.ok || ok && (from != c.from || to != c.to) {
			t.Errorf("row %d: got %d-%d %v, want %d-%d %v", c.y, from, to, ok, c.from, c.to, c.ok)
		}
	}
	// Dragging backwards covers the same cells.
	back := selection{area: s.area, ax: 4, ay: 8, bx: 6, by: 6}
	if f1, t1, _ := back.span(6); f1 != 6 || t1 != 12 {
		t.Errorf("a backwards drag covers %d-%d on the first row", f1, t1)
	}
}
