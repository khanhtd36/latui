package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// teamsSetupModal takes a pasted API token for the Teams lookup: it checks
// the token with the gateway and keeps it in the OS secure store, so the
// next run finds it. The token is never drawn: the box shows dots.
type teamsSetupModal struct {
	login func(ctx context.Context, email, token string) error
	email string // the account the token is for, shown; empty: unknown
	input textinput.Model
	back  modal
	again func() tea.Cmd // runs once the token works, to redo what needed it
	busy  bool
	err   string
}

const teamsTokenURL = "https://id.atlassian.com/manage-profile/security/api-tokens"

func newTeamsSetup(login func(context.Context, string, string) error, email string, back modal, again func() tea.Cmd) *teamsSetupModal {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "paste the token"
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	in.CharLimit = 2048
	in.Width = 50
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	return &teamsSetupModal{login: login, email: email, input: in, back: back, again: again}
}

// teamsLoginMsg is the answer to a pasted token.
type teamsLoginMsg struct{ err error }

func (t *teamsSetupModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return t.back, nil
	case "ctrl+o":
		return t, openURL(teamsTokenURL, "opened the API token page")
	case "enter":
		token := strings.TrimSpace(t.input.Value())
		if t.busy || token == "" {
			return t, nil
		}
		t.busy, t.err = true, ""
		login, email := t.login, ""
		return t, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			return teamsLoginMsg{err: login(ctx, email, token)}
		}
	}
	if t.busy {
		return t, nil
	}
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return t, cmd
}

// result takes the gateway's answer: a good token closes the box, a bad one
// stays for another try, with the reason and never the token.
func (t *teamsSetupModal) result(msg teamsLoginMsg) (modal, tea.Cmd) {
	t.busy = false
	if msg.err != nil {
		t.err = msg.err.Error()
		t.input.SetValue("")
		return t, nil
	}
	cmds := []tea.Cmd{statusCmd("teams are on: the token is saved")}
	if t.again != nil {
		cmds = append(cmds, t.again())
	}
	return t.back, tea.Batch(cmds...)
}

func (t *teamsSetupModal) view(int) string {
	lines := []string{
		styleSection.Render("Teams token"), "",
		"Mentioning a team needs an API token: acli's sign-in can't read teams.",
	}
	if t.email != "" {
		lines = append(lines, "It is used as "+styleCyan.Render(t.email)+" and kept in the OS secure store.")
	}
	lines = append(lines, "Create one on "+styleCyan.Render(teamsTokenURL), "", t.input.View(), "")
	switch {
	case t.busy:
		lines = append(lines, styleYellow.Render("checking the token…"))
	case t.err != "":
		lines = append(lines, styleRed.Render(truncate("error: "+t.err, 70)))
	default:
		lines = append(lines, "")
	}
	lines = append(lines, styleDim.Render("enter save · ctrl+o open the token page · esc cancel"))
	return styleModal.Render(strings.Join(lines, "\n"))
}
