package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/jira"
)

// Issue detail panes, in tab order: the Content (description and activity
// in one scroll) and the Fields sidebar.
const (
	paneContent = iota
	paneFields
	paneCount
)

// Activity tabs.
const (
	tabComments = iota
	tabHistory
	tabLinks
	tabAttachments
	tabCount
)

var tabNames = [tabCount]string{"Comments", "History", "Links", "Attachments"}

type issueModel struct {
	api  jiraAPI
	site auth.Credential
	me   jira.User
	key  string
	back []string // issues a link was followed from, oldest first

	issue       *jira.Issue
	err         error
	comments    []jira.Comment
	cErr        error
	cLoaded     bool
	history     []jira.HistoryEntry
	hErr        error
	hLoaded     bool
	transitions []jira.Transition
	tLoaded     bool
	loadedAt    time.Time // when the last load started, to skip needless prefetches
	refreshErr  error     // a background refresh failed; the shown data is older
	saving      string    // what a write in flight changes, for the title
	pending     string    // a key to run once the issue loads (a My issues row action)

	mention mentionDeps // how a text box in this issue mentions people

	focus  int
	tab    int
	scroll [paneCount]int // first line shown, per pane
	cursor [tabCount]int  // Links and Attachments rows

	// Set by keys, settled by the next draw, which knows the layout.
	jump   bool // scroll the Content to the activity tabs
	reveal bool // scroll the Content to the cursor row

	// Set by the last draw, for keys.
	actStart   int  // Content line of the activity tabs
	rows       int  // lines the Content shows
	cursorMode bool // j/k move the cursor: the activity is at the top and its tab has rows

	md *markdownCache
}

func newIssue(api jiraAPI, site auth.Credential, me jira.User, key string) *issueModel {
	return &issueModel{api: api, site: site, me: me, key: key, focus: paneContent, md: newMarkdownCache()}
}

type (
	// issueMsg carries one part of an issue: fields, comments or history.
	issueMsg struct {
		key      string
		issue    *jira.Issue
		comments []jira.Comment
		history  []jira.HistoryEntry
		trans    []jira.Transition
		part     int // which of the four
		err      error
	}
	// writtenMsg is a finished edit.
	writtenMsg struct {
		key, done string
		comment   bool
		err       error
	}
	transitionsMsg struct {
		key         string
		transitions []jira.Transition
		err         error
	}
)

const (
	partIssue = iota
	partComments
	partHistory
	partTransitions
)

// load fetches every part of the issue at once, transitions included so
// that t opens without a wait.
func (d *issueModel) load() tea.Cmd {
	d.loadedAt = time.Now()
	return tea.Batch(d.fetch(partIssue), d.fetch(partComments), d.fetch(partHistory), d.fetch(partTransitions))
}

func (d *issueModel) fetch(part int) tea.Cmd {
	api, key := d.api, d.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		msg := issueMsg{key: key, part: part}
		switch part {
		case partIssue:
			msg.issue, msg.err = api.Issue(ctx, key)
		case partComments:
			msg.comments, msg.err = api.Comments(ctx, key)
		case partHistory:
			msg.history, msg.err = api.History(ctx, key)
		case partTransitions:
			msg.trans, msg.err = api.Transitions(ctx, key)
		}
		return msg
	}
}

func (d *issueModel) update(msg issueMsg) {
	if msg.key != d.key {
		return
	}
	switch msg.part {
	case partIssue:
		switch {
		case msg.err == nil:
			d.issue, d.err, d.refreshErr = msg.issue, nil, nil
		case d.issue != nil:
			d.refreshErr = msg.err // keep showing what was loaded before
		default:
			d.err = msg.err
		}
	case partComments:
		d.comments, d.cErr, d.cLoaded = msg.comments, msg.err, true
	case partHistory:
		d.history, d.hErr, d.hLoaded = msg.history, msg.err, true
	case partTransitions:
		if msg.err == nil {
			d.transitions, d.tLoaded = msg.trans, true
		}
	}
}

// afterWrite reloads what an edit changed: the fields and history, and
// the comments after a comment.
func (d *issueModel) afterWrite(msg writtenMsg) tea.Cmd {
	d.saving = ""
	if msg.err != nil {
		return nil
	}
	cmds := []tea.Cmd{d.fetch(partIssue), d.fetch(partHistory), d.fetch(partTransitions)}
	if msg.comment {
		cmds = append(cmds, d.fetch(partComments))
		d.tab, d.focus, d.jump = tabComments, paneContent, true
	}
	return tea.Batch(cmds...)
}

// inner reports a state esc backs out of before leaving the issue; there
// is none yet, but panes may grow some.
func (d *issueModel) inner() bool { return false }

func (d *issueModel) browseURL() string { return d.site.SiteURL() + "/browse/" + d.key }

func (d *issueModel) write(what, done string, comment bool, run func(ctx context.Context) error) tea.Cmd {
	d.saving = what
	key := d.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return writtenMsg{key: key, done: done, comment: comment, err: run(ctx)}
	}
}

func (d *issueModel) onKey(msg tea.KeyMsg, width, height int) tea.Cmd {
	k := msg.String()
	switch k {
	case "tab":
		d.focus = (d.focus + 1) % paneCount
		return nil
	case "shift+tab":
		d.focus = (d.focus + paneCount - 1) % paneCount
		return nil
	case "1", "2", "3", "4":
		d.tab, d.focus, d.jump = int(k[0]-'1'), paneContent, true
		return nil
	case "]":
		d.nextTab(1)
		return nil
	case "[":
		d.nextTab(tabCount - 1)
		return nil
	case "r":
		return d.load()
	case "o":
		if d.cursorMode && d.focus == paneContent {
			if u := d.selectedURL(); u != "" {
				return openURL(u, "opened in the browser")
			}
		}
		return openURL(d.browseURL(), "opened "+d.key)
	case "y":
		summary := ""
		if d.issue != nil {
			summary = d.issue.Fields.Summary
		}
		return copyMenu(d.key,
			copyItem{"Key", d.key},
			copyItem{"Link", d.browseURL()},
			copyItem{"Summary", summary},
			copyItem{"Key and summary", strings.TrimSpace(d.key + " " + summary)},
			copyItem{"Branch name", jira.BranchName(d.key, summary)},
		)
	case "b":
		summary := ""
		if d.issue != nil {
			summary = d.issue.Fields.Summary
		}
		name := jira.BranchName(d.key, summary)
		return copyText(name, "copied "+name)
	case "enter":
		if d.cursorMode && d.focus == paneContent && d.tab == tabLinks {
			if rows := d.linkRows(); d.cursor[tabLinks] < len(rows) {
				return openIssueCmd(rows[d.cursor[tabLinks]].key)
			}
		}
		return nil
	}
	if d.issue == nil {
		return d.scrollKey(k, height)
	}
	switch k {
	case "t":
		if d.tLoaded {
			return showModal(d.transitionMenu(d.transitions))
		}
		return d.fetchTransitions()
	case "c":
		ed := newEditor("Comment on "+d.key, "", width, nil)
		ed.mention = d.mentionSource()
		ed.onSubmit = func(text string) tea.Cmd {
			subs := ed.box.subs
			return d.write("comment", "commented on "+d.key, true, func(ctx context.Context) error {
				return d.api.AddComment(ctx, d.key, text, subs)
			})
		}
		return showModal(ed)
	case "e":
		if !d.issue.Editable("summary") {
			return statusCmd("the summary can't be edited here")
		}
		return showModal(newInput("Summary of "+d.key, d.issue.Fields.Summary, "enter save · esc cancel", func(s string) (modal, tea.Cmd) {
			s = strings.TrimSpace(s)
			if s == "" || s == d.issue.Fields.Summary {
				return nil, nil
			}
			return nil, d.write("summary", "summary saved", false, func(ctx context.Context) error {
				return d.api.SetSummary(ctx, d.key, s)
			})
		}))
	case "l":
		return d.editLabels()
	case "C":
		return d.editComponents()
	case "A":
		return d.editAssignee()
	}
	return d.scrollKey(k, height)
}

// nextTab steps the activity tab; the scroll jumps to the tabs only when
// they are out of view.
func (d *issueModel) nextTab(by int) {
	d.tab, d.focus = (d.tab+by)%tabCount, paneContent
	if d.actStart < d.scroll[paneContent] || d.actStart >= d.scroll[paneContent]+d.rows {
		d.jump = true
	}
}

// scrollKey moves in the focused pane: a cursor on Links and Attachments
// once their tabs are at the top of the Content, else the scroll.
func (d *issueModel) scrollKey(k string, height int) tea.Cmd {
	var step int
	switch k {
	case "j", "down":
		step = 1
	case "k", "up":
		step = -1
	case "ctrl+d", "pgdown":
		step = max(1, height/4)
	case "ctrl+u", "pgup":
		step = -max(1, height/4)
	case "g", "home":
		step = -1 << 20
	case "G", "end":
		step = 1 << 20
	default:
		return nil
	}
	if d.focus == paneContent && d.cursorMode && (k == "j" || k == "k" || k == "down" || k == "up") {
		n := len(d.linkRows())
		if d.tab == tabAttachments && d.issue != nil {
			n = len(d.issue.Fields.Attachments)
		}
		// At either end of the rows the keys scroll on.
		if next := d.cursor[d.tab] + step; next >= 0 && next < n {
			d.cursor[d.tab], d.reveal = next, true
			return nil
		}
	}
	d.scroll[d.focus] = max(0, d.scroll[d.focus]+step)
	return nil
}

func (d *issueModel) selectedURL() string {
	switch d.tab {
	case tabLinks:
		if rows := d.linkRows(); d.cursor[tabLinks] < len(rows) {
			return d.site.SiteURL() + "/browse/" + rows[d.cursor[tabLinks]].key
		}
	case tabAttachments:
		if d.issue != nil && d.cursor[tabAttachments] < len(d.issue.Fields.Attachments) {
			a := d.issue.Fields.Attachments[d.cursor[tabAttachments]]
			return d.site.SiteURL() + "/rest/api/3/attachment/content/" + a.ID
		}
	}
	return ""
}

// linkRow is one row of the Links tab.
type linkRow struct {
	relation, key, summary, status, category string
}

func (d *issueModel) linkRows() []linkRow {
	if d.issue == nil {
		return nil
	}
	f := d.issue.Fields
	var rows []linkRow
	add := func(rel string, li *jira.LinkedIssue) {
		if li == nil || li.Key == "" {
			return
		}
		rows = append(rows, linkRow{rel, li.Key, li.Fields.Summary, li.Fields.Status.Name, li.Fields.Status.Category.Key})
	}
	add("parent", f.Parent)
	for i := range f.Subtasks {
		add("subtask", &f.Subtasks[i])
	}
	for _, l := range f.Links {
		if l.Outward != nil {
			add(l.Type.Outward, l.Outward)
		}
		if l.Inward != nil {
			add(l.Type.Inward, l.Inward)
		}
	}
	return rows
}

func (d *issueModel) fetchTransitions() tea.Cmd {
	api, key := d.api, d.key
	return tea.Batch(statusCmd("loading transitions…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ts, err := api.Transitions(ctx, key)
		return transitionsMsg{key: key, transitions: ts, err: err}
	})
}

// transitionMenu lists the moves from the current status. One with a
// required field other than Resolution is done in the browser.
func (d *issueModel) transitionMenu(ts []jira.Transition) modal {
	if len(ts) == 0 {
		return &menuModal{title: d.key + ": no transitions from here", items: []menuItem{{label: "Close", run: func() (modal, tea.Cmd) { return nil, nil }}}}
	}
	current := ""
	if d.issue != nil {
		current = d.issue.Fields.Status.Name
	}
	items := make([]menuItem, 0, len(ts))
	for _, t := range ts {
		label := t.Name
		if t.To.Name != "" && !strings.EqualFold(t.To.Name, t.Name) {
			label += styleDim.Render("  → ") + statusStyle(t.To.Category.Key).Render(t.To.Name)
		}
		if len(t.OtherRequired) > 0 {
			label += styleDim.Render("  (needs " + strings.Join(t.OtherRequired, ", ") + ": opens the browser)")
		}
		items = append(items, menuItem{label: label, run: func() (modal, tea.Cmd) {
			switch {
			case len(t.OtherRequired) > 0:
				return nil, openURL(d.browseURL(), "finish the transition in the browser")
			case t.NeedsResolution:
				return d.resolutionMenu(t), nil
			}
			return nil, d.doTransition(t, "")
		}})
	}
	return &menuModal{title: fmt.Sprintf("Move %s from %s", d.key, cmpOr(current, "its status")), items: items}
}

func (d *issueModel) resolutionMenu(t jira.Transition) modal {
	items := make([]menuItem, 0, len(t.Resolutions))
	for _, r := range t.Resolutions {
		items = append(items, menuItem{label: r, run: func() (modal, tea.Cmd) { return nil, d.doTransition(t, r) }})
	}
	return &menuModal{title: t.Name + ": resolution", items: items}
}

func (d *issueModel) doTransition(t jira.Transition, resolution string) tea.Cmd {
	to := cmpOr(t.To.Name, t.Name)
	return d.write("status", d.key+" → "+to, false, func(ctx context.Context) error {
		return d.api.Transition(ctx, d.key, t.ID, resolution)
	})
}

func (d *issueModel) editLabels() tea.Cmd {
	if !d.issue.Editable("labels") {
		return statusCmd("labels can't be edited on this issue")
	}
	var opts []pickOption
	for _, l := range d.issue.Fields.Labels {
		opts = append(opts, pickOption{id: l, label: l, checked: true})
	}
	api := d.api
	p := newPicker("Labels of "+d.key, true, opts, func(chosen []pickOption) tea.Cmd {
		labels := make([]string, 0, len(chosen))
		for _, o := range chosen {
			labels = append(labels, o.id)
		}
		return d.write("labels", "labels saved", false, func(ctx context.Context) error {
			return d.api.SetLabels(ctx, d.key, labels)
		})
	})
	p.allowCreate = true
	p.noSpaces = true
	p.search = func(q string) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			ls, err := api.LabelSuggestions(ctx, q)
			res := pickerResultsMsg{query: q, err: err}
			for _, l := range ls {
				res.options = append(res.options, pickOption{id: l, label: l})
			}
			return res
		}
	}
	return tea.Batch(showModal(p), p.search(""))
}

func (d *issueModel) editComponents() tea.Cmd {
	if !d.issue.Editable("components") {
		return statusCmd("components can't be edited on this issue")
	}
	have := map[string]bool{}
	for _, c := range d.issue.Fields.Components {
		have[c.ID] = true
	}
	var opts []pickOption
	for _, c := range d.issue.AllowedComponents() {
		opts = append(opts, pickOption{id: c.ID, label: c.Name, checked: have[c.ID]})
	}
	if len(opts) == 0 {
		return statusCmd(d.issue.Fields.Project.Key + " has no components")
	}
	return showModal(newPicker("Components of "+d.key, true, opts, func(chosen []pickOption) tea.Cmd {
		ids := make([]string, 0, len(chosen))
		for _, o := range chosen {
			ids = append(ids, o.id)
		}
		return d.write("components", "components saved", false, func(ctx context.Context) error {
			return d.api.SetComponents(ctx, d.key, ids)
		})
	}))
}

func (d *issueModel) editAssignee() tea.Cmd {
	if !d.issue.Editable("assignee") {
		return statusCmd("the assignee can't be changed on this issue")
	}
	pinned := []pickOption{
		{id: d.me.AccountID, label: "Me (" + d.me.DisplayName + ")"},
		{id: "", label: "Unassigned"},
	}
	api, key := d.api, d.key
	p := newPicker("Assign "+d.key, false, pinned, func(chosen []pickOption) tea.Cmd {
		if len(chosen) == 0 {
			return nil
		}
		who := chosen[0]
		done := "assigned to " + who.label
		if who.id == "" {
			done = "unassigned"
		}
		return d.write("assignee", done, false, func(ctx context.Context) error {
			return d.api.Assign(ctx, d.key, who.id)
		})
	})
	p.search = func(q string) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			users, err := api.AssignableUsers(ctx, key, q)
			res := pickerResultsMsg{query: q, err: err}
			for _, u := range users {
				if u.AccountID == d.me.AccountID {
					continue // pinned as Me
				}
				res.options = append(res.options, pickOption{id: u.AccountID, label: u.DisplayName, detail: u.Email})
			}
			return res
		}
	}
	return tea.Batch(showModal(p), p.search(""))
}
