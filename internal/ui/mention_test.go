package ui

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khanhtd36/latui/internal/adf"
	"github.com/khanhtd36/latui/internal/prefs"
	"github.com/khanhtd36/latui/internal/teams"
)

type fakeTeams struct {
	searched []string
	state    teams.State
	token    string // the token last saved
	loginErr error
}

func (f *fakeTeams) State(context.Context) teams.State { return f.state }

func (f *fakeTeams) Login(_ context.Context, _, token string) error {
	if f.loginErr != nil {
		return f.loginErr
	}
	f.token, f.state = token, teams.Ready
	return nil
}

func (f *fakeTeams) Search(_ context.Context, q string) ([]teams.Team, error) {
	f.searched = append(f.searched, q)
	return []teams.Team{{ID: "t-1", Name: "MITS11 Testers", Members: 2}}, nil
}

func (f *fakeTeams) Members(context.Context, string) ([]teams.Member, error) {
	return []teams.Member{{ID: "a", Name: "Ann"}, {ID: "b", Name: "Bob"}}, nil
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = run(t, m, key(string(r)))
	}
	return m
}

// commentBox opens Issue detail and the comment editor on it.
func commentBox(t *testing.T, j *fakeJira, tm teamsAPI) (Model, *editorModal) {
	t.Helper()
	prefs.SetFile(filepath.Join(t.TempDir(), "prefs.json"))
	m := started(t, j, sampleFeed())
	if tm != nil {
		m = m.WithTeams(tm)
	}
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	m = run(t, m, key("c"))
	ed, ok := m.modal.(*editorModal)
	if !ok {
		t.Fatalf("c did not open the comment editor: %T", m.modal)
	}
	return m, ed
}

func TestMentionAPersonInAComment(t *testing.T) {
	j := &fakeJira{issue: sampleIssue()}
	m, ed := commentBox(t, j, nil)
	m = typeText(t, m, "hi ")
	m = run(t, m, key("@"))
	if _, ok := m.modal.(*pickerModal); !ok {
		t.Fatalf("@ after a space did not open the picker: %T", m.modal)
	}
	if len(j.mentionQueries) == 0 {
		t.Error("the picker did not ask for people")
	}
	m = typeText(t, m, "lee")
	if s := screen(m); !strings.Contains(s, "Lee Park") {
		t.Errorf("the picker lacks Lee Park:\n%s", s)
	}
	m = run(t, m, key("enter"))
	if m.modal != ed {
		t.Fatalf("picking did not go back to the editor: %T", m.modal)
	}
	if got := ed.ta.Value(); got != "hi @Lee Park " {
		t.Fatalf("the box holds %q", got)
	}
	m = typeText(t, m, "look")
	_ = run(t, m, key("ctrl+s"))
	if len(j.commentSubs) != 1 || j.commentSubs[0].Token != "@Lee Park" {
		t.Fatalf("posted subs %+v", j.commentSubs)
	}
	doc := adf.FromMarkdownWith(j.comment, j.commentSubs)
	if len(doc.Content) == 0 || !strings.Contains(adfJSON(doc), `"id":"u2"`) {
		t.Errorf("the comment has no mention of u2: %s", adfJSON(doc))
	}
}

func adfJSON(n adf.Node) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestAtInsideAWordIsJustAnAt(t *testing.T) {
	m, ed := commentBox(t, &fakeJira{issue: sampleIssue()}, nil)
	m = typeText(t, m, "mail me")
	m = run(t, m, key("@"))
	if _, ok := m.modal.(*pickerModal); ok {
		t.Fatal("an @ inside a word opened the picker")
	}
	if got := ed.ta.Value(); got != "mail me@" {
		t.Errorf("the box holds %q", got)
	}
}

func TestEscKeepsTheAt(t *testing.T) {
	m, ed := commentBox(t, &fakeJira{issue: sampleIssue()}, nil)
	m = run(t, m, key("@"))
	m = run(t, m, key("esc"))
	if m.modal != ed || ed.ta.Value() != "@" {
		t.Errorf("after esc: modal %T, box %q", m.modal, ed.ta.Value())
	}
}

func TestMentionATeamExpandsToItsMembers(t *testing.T) {
	j := &fakeJira{issue: sampleIssue()}
	tm := &fakeTeams{state: teams.Ready}
	m, ed := commentBox(t, j, tm)
	m = run(t, m, key("@"))
	m = typeText(t, m, "MITS")
	if s := screen(m); !strings.Contains(s, "MITS11 Testers") || !strings.Contains(s, "team · 2 members") {
		t.Fatalf("the picker lacks the team:\n%s", s)
	}
	m = run(t, m, key("enter"))
	if got := ed.ta.Value(); got != "@MITS11 Testers " {
		t.Fatalf("the box holds %q", got)
	}
	if !strings.Contains(m.status, "2 members will be mentioned") {
		t.Errorf("status %q", m.status)
	}
	_ = run(t, m, key("ctrl+s"))
	doc := adfJSON(adf.FromMarkdownWith(j.comment, j.commentSubs))
	for _, want := range []string{`"id":"a"`, `"id":"b"`, "/jira/people/team/t-1"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the comment lacks %s: %s", want, doc)
		}
	}
}

func TestPickerSaysHowToSetUpTeams(t *testing.T) {
	tm := &fakeTeams{state: teams.Off}
	m, _ := commentBox(t, &fakeJira{issue: sampleIssue()}, tm)
	m = run(t, m, key("@"))
	if !strings.Contains(screen(m), "ctrl+t to set up teams") {
		t.Errorf("no hint about setting up teams:\n%s", screen(m))
	}
	if len(tm.searched) != 0 {
		t.Error("teams were searched with no token")
	}
	m = run(t, m, key("esc"))

	rejected := &fakeTeams{state: teams.Rejected}
	m, _ = commentBox(t, &fakeJira{issue: sampleIssue()}, rejected)
	m = run(t, m, key("@"))
	if !strings.Contains(screen(m), "turned down") || len(rejected.searched) != 0 {
		t.Errorf("a rejected token should say so and not be tried:\n%s", screen(m))
	}
}

func TestPastingATeamsToken(t *testing.T) {
	tm := &fakeTeams{state: teams.Off}
	m, ed := commentBox(t, &fakeJira{issue: sampleIssue()}, tm)
	m = run(t, m, key("@"))
	picker := m.modal
	m = run(t, m, key("ctrl+t"))
	if _, ok := m.modal.(*teamsSetupModal); !ok {
		t.Fatalf("ctrl+t did not open the token box: %T", m.modal)
	}
	m = typeText(t, m, "secret-token")
	if s := screen(m); strings.Contains(s, "secret") || !strings.Contains(s, "••••") {
		t.Fatalf("the token is shown or not masked:\n%s", s)
	}

	// A token the gateway turns down stays in the box with the reason.
	tm.loginErr = errors.New("teams: 401 Unauthorized: the token was turned down")
	m = run(t, m, key("enter"))
	if _, ok := m.modal.(*teamsSetupModal); !ok || !strings.Contains(screen(m), "turned down") {
		t.Fatalf("a bad token should stay in the box with the reason:\n%s", screen(m))
	}
	if strings.Contains(screen(m), "secret-token") {
		t.Error("the token came back in the error")
	}

	// A good one saves, turns teams on and goes back to the picker.
	tm.loginErr = nil
	m = typeText(t, m, "good-token")
	m = run(t, m, key("enter"))
	if tm.token != "good-token" || m.modal != picker || tm.state != teams.Ready {
		t.Fatalf("token %q, modal %T, state %v", tm.token, m.modal, tm.state)
	}
	if !strings.Contains(m.status, "teams are on") {
		t.Errorf("status %q", m.status)
	}
	if len(tm.searched) == 0 {
		t.Error("the picker did not look for teams again")
	}
	_ = ed
}

func TestEscLeavesTheTokenBoxWithoutSaving(t *testing.T) {
	tm := &fakeTeams{state: teams.Off}
	m, _ := commentBox(t, &fakeJira{issue: sampleIssue()}, tm)
	m = run(t, m, key("@"))
	picker := m.modal
	m = run(t, m, key("ctrl+t"))
	m = typeText(t, m, "abc")
	m = run(t, m, key("esc"))
	if m.modal != picker || tm.token != "" {
		t.Errorf("esc: modal %T, saved %q", m.modal, tm.token)
	}
}

func TestRecentMentionsComeFirst(t *testing.T) {
	m, _ := commentBox(t, &fakeJira{issue: sampleIssue()}, nil)
	m = run(t, m, key("@"))
	m = typeText(t, m, "max")
	m = run(t, m, key("enter")) // Max Reyes, remembered
	if len(m.prefs.Mentions) == 0 || m.prefs.Mentions[0].ID != "u3" {
		t.Fatalf("recent mentions %+v", m.prefs.Mentions)
	}
	m = run(t, m, key("ctrl+s"))
	m = run(t, m, key("c"))
	m = run(t, m, key("@"))
	s := screen(m)
	if i, j := strings.Index(s, "Max Reyes"), strings.Index(s, "Alex Kim"); i < 0 || j < 0 || i > j {
		t.Errorf("the recent mention should come before the issue's people:\n%s", s)
	}
	if !strings.Contains(s, "recent") {
		t.Errorf("the recent mention is not marked:\n%s", s)
	}
}

func TestMentionInTheCreateFormDescription(t *testing.T) {
	m := started(t, &fakeJira{issue: sampleIssue()}, sampleFeed())
	m = run(t, m, key("n"))
	c, ok := m.modal.(*createModal)
	if !ok {
		t.Fatalf("n did not open the form: %T", m.modal)
	}
	c.setFocus(focusDescription)
	m = run(t, m, key("@"))
	if _, ok := m.modal.(*pickerModal); !ok {
		t.Fatalf("@ in the description did not open the picker: %T", m.modal)
	}
	m = typeText(t, m, "lee")
	m = run(t, m, key("enter"))
	if m.modal != c || !strings.Contains(c.desc.Value(), "@Lee Park") || len(c.box.subs) != 1 {
		t.Errorf("modal %T, description %q, subs %+v", m.modal, c.desc.Value(), c.box.subs)
	}
}
