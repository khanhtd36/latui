package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/adf"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/prefs"
	"github.com/khanhtd36/latui/internal/teams"
)

// Typing @ in a comment or a new issue's description opens a picker of
// people and teams. The text shows "@Name"; the box remembers who that is,
// and posting turns each remembered "@Name" into a real mention.

// teamsAPI is the Teams lookup; *teams.Client in the app, a fake in tests.
type teamsAPI interface {
	State(ctx context.Context) teams.State
	Login(ctx context.Context, email, token string) error
	Search(ctx context.Context, query string) ([]teams.Team, error)
	Members(ctx context.Context, teamID string) ([]teams.Member, error)
}

// mentionDeps is what every text box needs to mention people: the Teams
// lookup when there is one, and the recent mentions saved in the prefs.
type mentionDeps struct {
	teams    teamsAPI
	email    string // the sign-in's account, for the teams token box
	recents  func() []prefs.Mention
	remember func(prefs.Mention)
}

// mentionSource is where one text box's @ looks people up.
type mentionSource struct {
	mentionDeps
	api     jiraAPI
	site    string
	issue   string      // the issue commented on, or ""
	project string      // for a new issue: its project
	people  []jira.User // the issue's own, offered before any typing
}

// mentionTarget is a text box a picked mention goes into.
type mentionTarget interface {
	// insert types text at the caret and remembers sub for posting.
	insert(text string, sub *adf.Sub)
}

// textBox is the part of a text box that mentions share.
type textBox struct {
	area *textarea.Model
	subs []adf.Sub
}

func (b *textBox) insert(text string, sub *adf.Sub) {
	b.area.InsertString(text)
	if sub == nil {
		return
	}
	for _, s := range b.subs {
		if s.Token == sub.Token {
			return
		}
	}
	b.subs = append(b.subs, *sub)
}

// afterSpace says whether the caret sits at the start of a line or after
// whitespace: an @ there starts a mention, and one inside a word, as in an
// email address, is just an @.
func (b *textBox) afterSpace() bool {
	lines := strings.Split(b.area.Value(), "\n")
	row := b.area.Line()
	if row >= len(lines) {
		return true
	}
	li := b.area.LineInfo()
	col := li.StartColumn + li.CharOffset
	runes := []rune(lines[row])
	if col <= 0 || col > len(runes) {
		return col <= 0
	}
	return unicode.IsSpace(runes[col-1])
}

// mentionOption is a person or a team as the picker lists it.
func personOption(u jira.User, detail string) pickOption {
	return pickOption{id: u.AccountID, label: u.DisplayName, detail: detail}
}

func teamOption(t teams.Team) pickOption {
	detail := "team"
	if t.Members > 0 {
		detail = fmt.Sprintf("team · %d members", t.Members)
	}
	return pickOption{id: t.ID, label: t.Name, detail: detail, team: true}
}

const (
	teamsHint         = "ctrl+t to set up teams (LATUI_TEAMS_TOKEN works too)"
	teamsRejectedHint = "the teams token was turned down: ctrl+t to replace it"
)

// mentionPicker opens the picker for a mention in box, going back to back.
func (s mentionSource) picker(box mentionTarget, back modal) *pickerModal {
	var opts []pickOption
	seen := map[string]bool{}
	add := func(o pickOption) {
		if o.id != "" && !seen[o.id] {
			seen[o.id] = true
			opts = append(opts, o)
		}
	}
	if s.recents != nil {
		for _, r := range s.recents() {
			o := pickOption{id: r.ID, label: r.Name, detail: "recent", team: r.Team}
			if r.Team {
				o.detail = "recent team"
			}
			add(o)
		}
	}
	for _, u := range s.people {
		add(personOption(u, ""))
	}
	p := newPicker("Mention", false, opts, func(chosen []pickOption) tea.Cmd {
		if len(chosen) == 0 {
			return nil
		}
		o := chosen[0]
		if s.remember != nil {
			s.remember(prefs.Mention{ID: o.id, Name: o.label, Team: o.team})
		}
		if !o.team {
			sub := adf.PersonMention(o.id, o.label)
			box.insert(sub.Token+" ", &sub)
			return nil
		}
		return s.fetchTeam(box, o)
	})
	p.back = back
	p.cancel = func() { box.insert("@", nil) } // Esc keeps the @ as typed
	p.search = s.search
	if s.teams != nil {
		switch s.teams.State(context.Background()) {
		case teams.Off:
			p.note = teamsHint
		case teams.Rejected:
			p.note = teamsRejectedHint
		}
		if p.note != "" {
			p.keys = map[string]func() (modal, tea.Cmd){"ctrl+t": func() (modal, tea.Cmd) {
				setup := newTeamsSetup(s.teams.Login, s.email, p, func() tea.Cmd { return p.search(strings.TrimSpace(p.input.Value())) })
				return setup, nil
			}}
		}
	}
	return p
}

// search asks for the people who can see the issue and, when Teams is on,
// the teams, together.
func (s mentionSource) search(q string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		var (
			wg         sync.WaitGroup
			users      []jira.User
			found      []teams.Team
			uerr, terr error
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			users, uerr = s.api.MentionableUsers(ctx, s.issue, s.project, q)
		}()
		if s.teams != nil && s.teams.State(ctx) == teams.Ready {
			wg.Add(1)
			go func() {
				defer wg.Done()
				found, terr = s.teams.Search(ctx, q)
			}()
		}
		wg.Wait()
		res := pickerResultsMsg{query: q}
		for _, t := range found {
			res.options = append(res.options, teamOption(t))
		}
		for _, u := range users {
			res.options = append(res.options, personOption(u, u.Email))
		}
		switch {
		case uerr != nil && len(res.options) == 0:
			res.err = uerr
		case terr != nil && len(res.options) == 0:
			res.err = terr
		}
		return res
	}
}

// teamMembersMsg brings a mentioned team's people.
type teamMembersMsg struct {
	into    mentionTarget
	site    string
	team    pickOption
	members []teams.Member
	err     error
}

// fetchTeam asks for a team's members; the mention is typed once they are
// known, as the team and each of them.
func (s mentionSource) fetchTeam(box mentionTarget, o pickOption) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ms, err := s.teams.Members(ctx, o.id)
		return teamMembersMsg{into: box, site: s.site, team: o, members: ms, err: err}
	}
}

// onTeamMembers types the team into the box it was picked for.
func (m Model) onTeamMembers(msg teamMembersMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "error: " + msg.err.Error()
		return m, nil
	}
	members := make([]adf.Member, 0, len(msg.members))
	for _, mem := range msg.members {
		members = append(members, adf.Member{ID: mem.ID, Name: mem.Name})
	}
	sub := adf.TeamMention(msg.site, msg.team.id, msg.team.label, members)
	msg.into.insert(sub.Token+" ", &sub)
	m.status = fmt.Sprintf("%s: %d members will be mentioned", sub.Token, len(members))
	return m, nil
}

// mentionDeps is what the Model hands every text box.
func (m Model) mentionDeps() mentionDeps {
	p := m.prefs
	d := mentionDeps{
		recents: func() []prefs.Mention { return p.Mentions },
		remember: func(r prefs.Mention) {
			p.RememberMention(r)
			_ = p.Save()
		},
	}
	d.email = m.site.Email
	if m.teams != nil {
		d.teams = m.teams
	}
	return d
}

// mentionSource is where an @ in this issue's comments looks people up: the
// issue's own people first, Me, the reporter, the assignee, who commented.
func (d *issueModel) mentionSource() *mentionSource {
	src := &mentionSource{mentionDeps: d.mention, api: d.api, site: d.site.Site, issue: d.key}
	add := func(u *jira.User) {
		if u != nil && u.AccountID != "" {
			src.people = append(src.people, *u)
		}
	}
	add(&d.me)
	if d.issue != nil {
		add(d.issue.Fields.Reporter)
		add(d.issue.Fields.Assignee)
	}
	for i := range d.comments {
		add(&d.comments[i].Author)
	}
	return src
}

func (c *createModal) insert(text string, sub *adf.Sub) { c.box.insert(text, sub) }

// WithTeams turns on mentioning teams, looked up by t.
func (m Model) WithTeams(t teamsAPI) Model {
	m.teams = t
	return m
}
