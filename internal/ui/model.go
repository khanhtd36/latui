// Package ui is the bubbletea app: the Notifications page and Issue detail.
package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/update"
)

// requestTimeout bounds every API call.
const requestTimeout = 60 * time.Second

type Model struct {
	jira     jiraAPI
	feed     feedAPI
	interval time.Duration

	site     auth.Credential
	me       jira.User
	started  bool  // the site and Me are known
	startErr error // signing in failed: nothing else can load

	width, height int

	notes *notesPage
	issue *issueModel // non-nil while Issue detail is open
	help  *helpModal
	modal modal

	status      string
	lastProject string // for ctrl+g with a bare number

	updates         []update.Release
	checkingUpdates bool
	updating        *updateRun
	installed       string
	updateFailed    *updateFailure
}

func New(j jiraAPI, f feedAPI, interval time.Duration) Model {
	return Model{jira: j, feed: f, interval: interval, notes: newNotesPage()}
}

type (
	startMsg struct {
		site auth.Credential
		me   jira.User
		err  error
	}
	tickMsg struct{}
)

func (m Model) Init() tea.Cmd { return tea.Batch(m.start(), checkUpdates()) }

func (m Model) start() tea.Cmd {
	j := m.jira
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		site, err := j.Site(ctx)
		if err != nil {
			return startMsg{err: err}
		}
		me, err := j.Myself(ctx)
		return startMsg{site: site, me: me, err: err}
	}
}

// tickAfter schedules the next refresh; tests replace it.
var tickAfter = tea.Tick

func (m Model) tick() tea.Cmd {
	return tickAfter(m.interval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case startMsg:
		if msg.err != nil {
			m.startErr = msg.err
			return m, nil
		}
		m.site, m.me, m.started, m.startErr = msg.site, msg.me, true, nil
		return m, tea.Batch(m.notes.reloadAll(m.feed), m.tick())
	case tickMsg:
		return m, tea.Batch(m.notes.refresh(m.feed), m.tick())
	case feedMsg:
		return m, m.notes.onFeed(msg, m.feed)
	case countMsg:
		m.notes.onCount(msg)
		return m, nil
	case markedMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, m.notes.refresh(m.feed)
		}
		if msg.all {
			return m, m.notes.refresh(m.feed)
		}
		return m, nil
	case issueMsg:
		if m.issue != nil {
			m.issue.update(msg)
		}
		return m, nil
	case writtenMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
		} else {
			m.status = msg.done
		}
		if m.issue != nil && m.issue.key == msg.key {
			return m, m.issue.afterWrite(msg)
		}
		return m, nil
	case transitionsMsg:
		if m.issue == nil || m.issue.key != msg.key {
			return m, nil
		}
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		m.modal = m.issue.transitionMenu(msg.transitions)
		return m, nil
	case pickerResultsMsg:
		if p, ok := m.modal.(*pickerModal); ok {
			p.results(msg)
		}
		return m, nil
	case openIssueMsg:
		return m.openIssue(msg.key)
	case showModalMsg:
		m.modal = msg.modal
		return m, nil
	case copyMenuMsg:
		if len(msg.items) > 0 {
			m.modal = newCopyMenu(msg)
		}
		return m, nil
	case editorDoneMsg:
		if e, ok := m.modal.(editorDoner); ok {
			return m, e.editorDone(msg)
		}
		return m, nil
	case statusMsg:
		m.status = string(msg)
		return m, nil
	case updatesMsg:
		return m.onUpdates(msg)
	case updateStartMsg:
		return m.startUpdate(msg)
	case updateTickMsg:
		if m.updating == nil {
			return m, nil
		}
		return m, updateTick()
	case updateDoneMsg:
		return m.onUpdateDone(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

type showModalMsg struct{ modal modal }

func showModal(md modal) tea.Cmd { return func() tea.Msg { return showModalMsg{md} } }

type openIssueMsg struct{ key string }

func openIssueCmd(key string) tea.Cmd { return func() tea.Msg { return openIssueMsg{key} } }

func (m Model) openIssue(key string) (tea.Model, tea.Cmd) {
	if p := jira.ProjectOf(key); p != "" {
		m.lastProject = p
	}
	if m.issue != nil {
		if m.issue.key == key {
			return m, nil
		}
		back := append(m.issue.back, m.issue.key)
		m.issue = newIssue(m.jira, m.site, m.me, key)
		m.issue.back = back
	} else {
		m.issue = newIssue(m.jira, m.site, m.me, key)
	}
	m.status = ""
	return m, m.issue.load()
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.help != nil {
		closed, run := m.help.update(msg)
		if closed {
			m.help = nil
		}
		if run != nil {
			return m.onKey(*run)
		}
		return m, nil
	}
	if m.modal != nil {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.update(msg)
		return m, cmd
	}
	if m.startErr != nil {
		switch k {
		case "q", "esc":
			return m, tea.Quit
		case "r":
			m.startErr = nil
			return m, m.start()
		}
		return m, nil
	}
	m.status = ""
	switch k {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = newHelp(m.helpGroups(), m.width, m.height-2)
		return m, nil
	case "U":
		return m.checkUpdatesNow()
	case "ctrl+g":
		m.modal = newInput("Go to issue", "", "a key (DEMO-42), a number in "+cmpOr(m.lastProject, "the last project")+", or a link", func(s string) (modal, tea.Cmd) {
			key, ok := jira.ParseKey(s, m.lastProject)
			if !ok {
				return nil, statusCmd("not an issue key: " + s)
			}
			return nil, openIssueCmd(key)
		})
		return m, nil
	}
	if !m.started {
		return m, nil
	}
	if m.issue != nil {
		if k == "esc" && !m.issue.inner() {
			// Back to the issue a link was followed from, else the list.
			if n := len(m.issue.back); n > 0 {
				back := m.issue.back[:n-1]
				m.issue = newIssue(m.jira, m.site, m.me, m.issue.back[n-1])
				m.issue.back = back
				return m, m.issue.load()
			}
			m.issue = nil
			return m, nil
		}
		return m, m.issue.onKey(msg, m.width, m.bodyHeight())
	}
	return m, m.notes.key(msg, m.feed, m.bodyHeight())
}

// bodyHeight is the screen between the title line and the status and
// footer lines.
func (m Model) bodyHeight() int { return max(3, m.height-3) }

// signInHelp explains a failed start.
func signInHelp(err error) []string {
	lines := []string{styleRed.Render("Couldn't sign in to Atlassian."), "", truncate(err.Error(), 200), ""}
	if errors.Is(err, auth.ErrExpired) || strings.Contains(err.Error(), "acli") {
		lines = append(lines,
			"latui signs in with Atlassian's acli. In another terminal run:",
			"",
			styleCyan.Render("  acli jira auth login --web"),
			"",
			"or set LATUI_SITE, LATUI_EMAIL and LATUI_API_TOKEN (an API token from",
			"id.atlassian.com/manage-profile/security/api-tokens).",
			"")
	}
	return append(lines, styleDim.Render("r retry · q quit"))
}
