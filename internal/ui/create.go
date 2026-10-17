package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/latui/internal/jira"
)

// Create form chips, in tab order after summary and description.
const (
	chipAssignee = iota
	chipLabels
	chipPriority
	chipComponents
	chipParent
	chipDue
	chipTester
	chipStatus
	chipCount
)

var chipNames = [chipCount]string{"Assignee", "Labels", "Priority", "Components", "Parent", "Due", "Tester", "Status"}

// Focus stops of the create form.
const (
	focusProject = iota
	focusType
	focusSummary
	focusDescription
	focusChips   // focusChips+i is chip i (only the visible ones are stopped at)
	focusAnother = focusChips + chipCount
	focusCreate  = focusAnother + 1
	focusStops   = focusCreate + 1
)

// handledFields are the create screen fields the form fills or leaves to
// Jira; another required one sends the user to the browser.
var handledFields = map[string]bool{
	"project": true, "issuetype": true, "summary": true, "description": true, "reporter": true,
	"assignee": true, "labels": true, "priority": true, "components": true, "parent": true, "duedate": true,
}

// createModal is the create form, shaped like the web's: project and
// type, summary, description, a row of field chips, then Create another
// and Create.
type createModal struct {
	api  jiraAPI
	me   jira.User
	site string

	projects []jira.Project
	project  string // key
	types    []jira.IssueType
	typeID   string
	fields   map[string]jira.CreateField // of the chosen type, by ID
	statuses map[string][]jira.StatusOption
	loading  string // what is on its way, for the form's note
	err      error

	summary textinput.Model
	desc    textarea.Model
	focus   int

	assignee    pickOption // id "" is Automatic
	labels      []string
	priority    jira.Option
	components  []jira.Option
	parent      string
	due         string
	testers     []pickOption
	status      string // "" keeps the workflow's first status
	another     bool
	created     []createdIssue
	creating    bool
	fixedParent string // a subtask's parent, set when opened from it
	wantSubtask bool
	onAnother   func(bool) // saves the Create another choice
	onUsed      func(project, typeID string)
	defaultType string
}

type createdIssue struct{ key, summary string }

type (
	// createDataMsg brings what the form loads: projects, types, fields
	// and statuses.
	createDataMsg struct {
		projects []jira.Project
		project  string
		types    []jira.IssueType
		typeID   string
		fields   []jira.CreateField
		statuses map[string][]jira.StatusOption
		err      error
	}
	createdMsg struct {
		key, summary string
		moveErr      string // the issue exists but its status move failed
		err          error
	}
)

type createOptions struct {
	project, typeID string
	parent          string // set for "new subtask of"
	another         bool
}

func newCreate(api jiraAPI, me jira.User, site string, o createOptions) (*createModal, tea.Cmd) {
	sum := textinput.New()
	sum.Prompt = ""
	sum.Placeholder = "what needs doing"
	sum.CharLimit = 255
	sum.Cursor.SetMode(cursor.CursorStatic)
	desc := textarea.New()
	desc.Placeholder = "Description (markdown; ctrl+e opens $EDITOR)"
	desc.ShowLineNumbers = false
	desc.CharLimit = 0
	desc.SetHeight(5)
	desc.Cursor.SetMode(cursor.CursorStatic)
	c := &createModal{
		api: api, me: me, site: site, project: o.project, defaultType: o.typeID,
		summary: sum, desc: desc, another: o.another,
		fixedParent: o.parent, parent: o.parent, wantSubtask: o.parent != "",
		assignee: pickOption{label: "Automatic"},
	}
	c.setFocus(focusSummary)
	cmds := []tea.Cmd{c.fetchProjects()}
	if c.project != "" {
		cmds = append(cmds, c.fetchTypes(c.project))
	}
	return c, tea.Batch(cmds...)
}

func (c *createModal) fetchProjects() tea.Cmd {
	api := c.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ps, err := api.RecentProjects(ctx)
		return createDataMsg{projects: ps, err: err}
	}
}

func (c *createModal) fetchTypes(project string) tea.Cmd {
	c.loading = "types"
	api := c.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ts, err := api.CreateTypes(ctx, project)
		if err != nil {
			return createDataMsg{project: project, err: err}
		}
		statuses, _ := api.TypeStatuses(ctx, project) // optional: no Status chip without it
		return createDataMsg{project: project, types: ts, statuses: statuses}
	}
}

func (c *createModal) fetchFields(project, typeID string) tea.Cmd {
	c.loading = "fields"
	api := c.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		fs, err := api.CreateFields(ctx, project, typeID)
		return createDataMsg{project: project, typeID: typeID, fields: fs, err: err}
	}
}

// data takes what a fetch brought, and asks for what follows from it.
func (c *createModal) data(msg createDataMsg) tea.Cmd {
	if msg.err != nil {
		c.err, c.loading = msg.err, ""
		return nil
	}
	switch {
	case msg.projects != nil:
		c.projects = msg.projects
		if c.project == "" && len(c.projects) > 0 {
			c.project = c.projects[0].Key
			return c.fetchTypes(c.project)
		}
	case msg.types != nil && msg.project == c.project:
		c.types, c.statuses, c.err = msg.types, msg.statuses, nil
		c.typeID = c.pickType()
		if c.typeID == "" {
			c.loading = ""
			c.err = fmt.Errorf("you can't create issues in %s", c.project)
			return nil
		}
		return c.fetchFields(c.project, c.typeID)
	case msg.fields != nil && msg.project == c.project && msg.typeID == c.typeID:
		c.loading, c.err = "", nil
		c.fields = map[string]jira.CreateField{}
		for _, f := range msg.fields {
			c.fields[f.ID] = f
		}
		c.dropMissing()
	}
	return nil
}

// pickType keeps the chosen type when the project has it, else the
// default one, else a subtask type for "new subtask", else the first
// standard type.
func (c *createModal) pickType() string {
	has := func(id string) bool {
		for _, t := range c.types {
			if t.ID == id {
				return true
			}
		}
		return false
	}
	for _, id := range []string{c.typeID, c.defaultType} {
		if id != "" && has(id) && (!c.wantSubtask || c.typeOf(id).Subtask) {
			return id
		}
	}
	for _, t := range c.types {
		if t.Subtask == c.wantSubtask {
			return t.ID
		}
	}
	if len(c.types) > 0 {
		return c.types[0].ID
	}
	return ""
}

func (c *createModal) typeOf(id string) jira.IssueType {
	for _, t := range c.types {
		if t.ID == id {
			return t
		}
	}
	return jira.IssueType{}
}

// dropMissing clears values for chips the new type doesn't have.
func (c *createModal) dropMissing() {
	if !c.has(chipComponents) {
		c.components = nil
	}
	if !c.has(chipPriority) {
		c.priority = jira.Option{}
	}
	if !c.has(chipParent) {
		c.parent = ""
	}
	if !c.has(chipTester) {
		c.testers = nil
	}
	if !c.has(chipStatus) {
		c.status = ""
	}
	if !c.has(chipLabels) {
		c.labels = nil
	}
	if !c.has(chipDue) {
		c.due = ""
	}
}

// testerField is the Tester user-list field, if the type has one.
func (c *createModal) testerField() (jira.CreateField, bool) {
	for _, f := range c.fields {
		if strings.EqualFold(f.Name, "Tester") && f.Schema.Type == "array" && f.Schema.Items == "user" {
			return f, true
		}
	}
	return jira.CreateField{}, false
}

// has reports whether the chosen type shows a chip.
func (c *createModal) has(chip int) bool {
	if c.fields == nil {
		return false
	}
	switch chip {
	case chipAssignee:
		_, ok := c.fields["assignee"]
		return ok
	case chipLabels:
		_, ok := c.fields["labels"]
		return ok
	case chipPriority:
		_, ok := c.fields["priority"]
		return ok
	case chipComponents:
		_, ok := c.fields["components"]
		return ok
	case chipParent:
		_, ok := c.fields["parent"]
		return ok || c.typeOf(c.typeID).Subtask
	case chipDue:
		_, ok := c.fields["duedate"]
		return ok
	case chipTester:
		_, ok := c.testerField()
		return ok
	case chipStatus:
		return len(c.statuses[c.typeID]) > 1
	}
	return false
}

// unsupported are required fields the form can't fill.
func (c *createModal) unsupported() []string {
	var out []string
	for id, f := range c.fields {
		if f.Required && !f.HasDefault && !handledFields[id] {
			out = append(out, f.Name)
		}
	}
	return out
}

func (c *createModal) stopAt(f int) bool {
	switch {
	case f >= focusChips && f < focusAnother:
		return c.has(f - focusChips)
	case f == focusProject && c.fixedParent != "":
		return false
	}
	return true
}

func (c *createModal) setFocus(f int) {
	c.focus = f
	if f == focusSummary {
		c.summary.Focus()
	} else {
		c.summary.Blur()
	}
	if f == focusDescription {
		c.desc.Focus()
	} else {
		c.desc.Blur()
	}
}

func (c *createModal) moveFocus(dir int) {
	f := c.focus
	for range focusStops {
		f = (f + dir + focusStops) % focusStops
		if c.stopAt(f) {
			break
		}
	}
	c.setFocus(f)
}

func (c *createModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		if strings.TrimSpace(c.summary.Value()) != "" || strings.TrimSpace(c.desc.Value()) != "" {
			return &confirmModal{prompt: "Discard this issue?", back: c, then: nil, onYes: c.closedNote()}, nil
		}
		return nil, c.closedNote()
	case "ctrl+s":
		return c.submit()
	case "tab":
		c.moveFocus(1)
		return c, nil
	case "shift+tab":
		c.moveFocus(-1)
		return c, nil
	case "ctrl+e":
		return c, editExternally(c.desc.Value())
	case "ctrl+a":
		c.toggleAnother()
		return c, nil
	case "ctrl+y":
		if len(c.created) == 0 {
			return c, statusCmd("nothing created yet")
		}
		return c.copyCreated(), nil
	}
	switch c.focus {
	case focusSummary:
		if k == "enter" {
			c.moveFocus(1)
			return c, nil
		}
		var cmd tea.Cmd
		c.summary, cmd = c.summary.Update(msg)
		return c, cmd
	case focusDescription:
		var cmd tea.Cmd
		c.desc, cmd = c.desc.Update(msg)
		return c, cmd
	}
	switch k {
	case "j", "down", "right", "l":
		c.moveFocus(1)
		return c, nil
	case "k", "up", "left", "h":
		c.moveFocus(-1)
		return c, nil
	case "enter", " ":
		switch {
		case c.focus == focusProject:
			return c.pickProject(), nil
		case c.focus == focusType:
			return c.pickTypeMenu(), nil
		case c.focus == focusAnother:
			c.toggleAnother()
			return c, nil
		case c.focus == focusCreate:
			return c.submit()
		case c.focus >= focusChips:
			return c.openChip(c.focus - focusChips)
		}
	}
	return c, nil
}

func (c *createModal) toggleAnother() {
	c.another = !c.another
	if c.onAnother != nil {
		c.onAnother(c.another)
	}
}

// closedNote says how many issues the form created, if any.
func (c *createModal) closedNote() tea.Cmd {
	switch n := len(c.created); {
	case n == 1:
		return statusCmd("created " + c.created[0].key)
	case n > 1:
		return statusCmd(fmt.Sprintf("created %d issues", n))
	}
	return nil
}

func (c *createModal) editorDone(msg editorDoneMsg) tea.Cmd {
	if msg.err != nil {
		return statusCmd("error: editor: " + msg.err.Error())
	}
	c.desc.SetValue(strings.TrimRight(msg.content, "\r\n"))
	return nil
}

func (c *createModal) pickProject() modal {
	opts := make([]pickOption, 0, len(c.projects))
	for _, p := range c.projects {
		opts = append(opts, pickOption{id: p.Key, label: p.Key, detail: p.Name})
	}
	p := newPicker("Project", false, opts, nil)
	p.allowCreate = true // a project key not among the recent ones
	p.noSpaces = true
	p.back = c
	p.onSave = func(chosen []pickOption) tea.Cmd {
		key := strings.ToUpper(strings.TrimSpace(chosen[0].id))
		if key == "" || key == c.project {
			return nil
		}
		c.project, c.types, c.fields, c.typeID = key, nil, nil, ""
		return c.fetchTypes(key)
	}
	return p
}

func (c *createModal) pickTypeMenu() modal {
	opts := make([]pickOption, 0, len(c.types))
	for _, t := range c.types {
		opts = append(opts, pickOption{id: t.ID, label: t.Name, detail: pick2(t.Subtask, "subtask", "")})
	}
	p := newPicker("Issue type in "+c.project, false, opts, func(ch []pickOption) tea.Cmd {
		if ch[0].id == c.typeID {
			return nil
		}
		c.typeID, c.fields = ch[0].id, nil
		return c.fetchFields(c.project, c.typeID)
	})
	p.back = c
	return p
}

// openChip opens the picker or prompt for a chip.
func (c *createModal) openChip(chip int) (modal, tea.Cmd) {
	api, project := c.api, c.project
	users := func(skipMe bool) func(q string) tea.Cmd {
		return func(q string) tea.Cmd {
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
				defer cancel()
				us, err := api.ProjectUsers(ctx, project, q)
				res := pickerResultsMsg{query: q, err: err}
				for _, u := range us {
					if skipMe && u.AccountID == c.me.AccountID {
						continue
					}
					res.options = append(res.options, pickOption{id: u.AccountID, label: u.DisplayName, detail: u.Email})
				}
				return res
			}
		}
	}
	var p *pickerModal
	switch chip {
	case chipAssignee:
		p = newPicker("Assignee", false, []pickOption{
			{id: "", label: "Automatic", detail: "the project's default"},
			{id: c.me.AccountID, label: "Me (" + c.me.DisplayName + ")"},
		}, func(ch []pickOption) tea.Cmd {
			c.assignee = ch[0]
			if c.assignee.id == c.me.AccountID {
				c.assignee.label = "Me"
			}
			return nil
		})
		p.search = users(true)
	case chipLabels:
		opts := make([]pickOption, 0, len(c.labels))
		for _, l := range c.labels {
			opts = append(opts, pickOption{id: l, label: l, checked: true})
		}
		p = newPicker("Labels", true, opts, func(ch []pickOption) tea.Cmd {
			c.labels = c.labels[:0]
			for _, o := range ch {
				c.labels = append(c.labels, o.id)
			}
			return nil
		})
		p.allowCreate, p.noSpaces = true, true
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
	case chipPriority:
		prios := c.fields["priority"].Options()
		opts := make([]pickOption, 0, len(prios))
		for _, o := range prios {
			opts = append(opts, pickOption{id: o.ID, label: o.Name})
		}
		p = newPicker("Priority", false, opts, func(ch []pickOption) tea.Cmd {
			c.priority = jira.Option{ID: ch[0].id, Name: ch[0].label}
			return nil
		})
	case chipComponents:
		have := map[string]bool{}
		for _, o := range c.components {
			have[o.ID] = true
		}
		comps := c.fields["components"].Options()
		opts := make([]pickOption, 0, len(comps))
		for _, o := range comps {
			opts = append(opts, pickOption{id: o.ID, label: o.Name, checked: have[o.ID]})
		}
		p = newPicker("Components", true, opts, func(ch []pickOption) tea.Cmd {
			c.components = c.components[:0]
			for _, o := range ch {
				c.components = append(c.components, jira.Option{ID: o.id, Name: o.label})
			}
			return nil
		})
	case chipParent:
		if c.fixedParent != "" {
			return c, statusCmd("a subtask of " + c.fixedParent)
		}
		p = newPicker("Parent", false, []pickOption{{id: "", label: "None"}}, func(ch []pickOption) tea.Cmd {
			c.parent = strings.ToUpper(ch[0].id)
			return nil
		})
		p.allowCreate, p.noSpaces = true, true
		p.search = func(q string) tea.Cmd {
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
				defer cancel()
				res := pickerResultsMsg{query: q}
				epics, err := api.OpenEpics(ctx, project)
				res.err = err
				ql := strings.ToLower(q)
				for _, e := range epics {
					if ql == "" || strings.Contains(strings.ToLower(e.Key+" "+e.Summary), ql) {
						res.options = append(res.options, pickOption{id: e.Key, label: e.Key, detail: e.Summary})
					}
				}
				return res
			}
		}
	case chipDue:
		in := newInput("Due date", c.due, "YYYY-MM-DD · empty for none · enter save · esc cancel", func(s string) (modal, tea.Cmd) {
			s = strings.TrimSpace(s)
			if s != "" {
				if _, err := time.Parse("2006-01-02", s); err != nil {
					return c, statusCmd("not a date: " + s + " (YYYY-MM-DD)")
				}
			}
			c.due = s
			return c, nil
		})
		in.back = c
		return in, nil
	case chipTester:
		p = newPicker("Tester", true, append([]pickOption(nil), c.testers...), func(ch []pickOption) tea.Cmd {
			c.testers = append(c.testers[:0], ch...)
			return nil
		})
		p.search = users(false)
	case chipStatus:
		opts := []pickOption{{id: "", label: "As created", detail: "the workflow's first status"}}
		for _, s := range c.statuses[c.typeID] {
			opts = append(opts, pickOption{id: s.Name, label: s.Name})
		}
		p = newPicker("Status after creating", false, opts, func(ch []pickOption) tea.Cmd {
			c.status = ch[0].id
			return nil
		})
	default:
		return c, nil
	}
	p.back = c
	if p.search != nil {
		return p, p.search("")
	}
	return p, nil
}

func (c *createModal) submit() (modal, tea.Cmd) {
	summary := strings.TrimSpace(c.summary.Value())
	switch {
	case c.creating:
		return c, statusCmd("creating…")
	case c.fields == nil:
		return c, statusCmd("still loading the form")
	case summary == "":
		c.setFocus(focusSummary)
		return c, statusCmd("the summary is empty")
	case c.typeOf(c.typeID).Subtask && c.parent == "":
		return c, statusCmd("a subtask needs a parent")
	}
	if u := c.unsupported(); len(u) > 0 {
		return c, statusCmd(c.project + " needs " + strings.Join(u, ", ") + ": create this one in the browser")
	}
	n := jira.NewIssue{
		Project: c.project, TypeID: c.typeID, Summary: summary,
		Description: strings.TrimSpace(c.desc.Value()),
		Assignee:    c.assignee.id, Labels: c.labels, PriorityID: c.priority.ID,
		Parent: c.parent, Due: c.due,
	}
	for _, o := range c.components {
		n.Components = append(n.Components, o.ID)
	}
	if f, ok := c.testerField(); ok && len(c.testers) > 0 {
		ids := make([]string, 0, len(c.testers))
		for _, t := range c.testers {
			ids = append(ids, t.id)
		}
		n.Users = map[string][]string{f.ID: ids}
	}
	if c.onUsed != nil {
		c.onUsed(c.project, c.typeID)
	}
	c.creating = true
	api, status := c.api, c.status
	return c, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		key, err := api.CreateIssue(ctx, n)
		if err != nil {
			return createdMsg{err: err}
		}
		msg := createdMsg{key: key, summary: n.Summary}
		if status != "" {
			moved, err := api.MoveTo(ctx, key, status)
			switch {
			case err != nil:
				msg.moveErr = err.Error()
			case !moved:
				msg.moveErr = "no transition leads there"
			}
		}
		return msg
	}
}

// onCreated keeps the form open for the next issue with Create another,
// else closes it and opens the new issue.
func (c *createModal) onCreated(msg createdMsg) (modal, tea.Cmd) {
	c.creating = false
	if msg.err != nil {
		return c, statusCmd("error: " + msg.err.Error())
	}
	c.created = append(c.created, createdIssue{msg.key, msg.summary})
	note := "created " + msg.key
	if msg.moveErr != "" {
		note = fmt.Sprintf("error: created %s, but couldn't move it to %s: %s", msg.key, c.status, msg.moveErr)
	}
	if c.another {
		c.summary.SetValue("")
		c.desc.SetValue("")
		c.setFocus(focusSummary)
		return c, tea.Batch(statusCmd(note), func() tea.Msg { return issuesChangedMsg{} })
	}
	return nil, tea.Batch(statusCmd(note), openIssueCmd(msg.key), func() tea.Msg { return issuesChangedMsg{} })
}

// issuesChangedMsg asks My issues to refresh.
type issuesChangedMsg struct{}

func (c *createModal) copyCreated() modal {
	keys := make([]string, 0, len(c.created))
	lines := make([]string, 0, len(c.created))
	for _, ci := range c.created {
		keys = append(keys, ci.key)
		lines = append(lines, ci.key+" "+ci.summary)
	}
	cp := func(text, done string) func() (modal, tea.Cmd) {
		return func() (modal, tea.Cmd) { return c, copyText(text, done) }
	}
	return &menuModal{title: fmt.Sprintf("Copy %d created", len(c.created)), items: []menuItem{
		{label: "Keys" + styleDim.Render("  "+truncate(strings.Join(keys, ", "), 50)), run: cp(strings.Join(keys, ", "), "copied the keys")},
		{label: "Key and summary lines", run: cp(strings.Join(lines, "\n"), "copied the issues")},
	}}
}

// --- View ---

func (c *createModal) chipText(chip int) string {
	switch chip {
	case chipAssignee:
		return c.assignee.label
	case chipLabels:
		if len(c.labels) == 0 {
			return "Labels"
		}
		return strings.Join(c.labels, ", ")
	case chipPriority:
		return cmpOr(c.priority.Name, "Priority")
	case chipComponents:
		if len(c.components) == 0 {
			return "Components"
		}
		names := make([]string, 0, len(c.components))
		for _, o := range c.components {
			names = append(names, o.Name)
		}
		return strings.Join(names, ", ")
	case chipParent:
		return cmpOr(c.parent, "Parent")
	case chipDue:
		if c.due == "" {
			return "Due date"
		}
		return "due " + c.due
	case chipTester:
		if len(c.testers) == 0 {
			return "Tester"
		}
		names := make([]string, 0, len(c.testers))
		for _, t := range c.testers {
			names = append(names, t.label)
		}
		return strings.Join(names, ", ")
	case chipStatus:
		return cmpOr(c.status, "Status")
	}
	return chipNames[chip]
}

func (c *createModal) chip(text string, focused, set bool) string {
	style := styleButton
	if focused {
		style = styleButtonCTA
	} else if !set {
		style = styleButton.Foreground(lipgloss.Color("244"))
	}
	return style.Render(text)
}

func (c *createModal) view(width int) string {
	w := min(100, max(50, width-8))
	c.summary.Width = w - 4
	c.desc.SetWidth(w - 2)
	typeName := "type"
	if t := c.typeOf(c.typeID); t.ID != "" {
		typeName = t.Name
	}
	lines := []string{
		c.chip("‹ "+cmpOr(c.project, "project")+" ›", c.focus == focusProject, true) + " " +
			c.chip("‹ "+typeName+" ›", c.focus == focusType, true) +
			styleDim.Render(pick2(c.fixedParent != "", "  subtask of "+c.fixedParent, "")),
		"",
		styleSection.Render("Summary  ") + c.summary.View(),
		"",
		c.desc.View(),
		"",
	}
	var chips []string
	for i := range chipCount {
		if c.has(i) {
			set := c.chipText(i) != chipNames[i] && c.chipText(i) != "Due date"
			chips = append(chips, c.chip(c.chipText(i), c.focus == focusChips+i, set))
		}
	}
	switch {
	case c.err != nil:
		lines = append(lines, styleRed.Render(truncate("error: "+c.err.Error(), w)))
	case c.loading != "":
		lines = append(lines, styleYellow.Render("loading "+c.loading+"…"))
	default:
		lines = append(lines, wrapChips(chips, w)...)
		if u := c.unsupported(); len(u) > 0 {
			lines = append(lines, styleYellow.Render(truncate(c.project+" needs "+strings.Join(u, ", ")+": create this type in the browser", w)))
		}
	}
	lines = append(lines, styleDim.Render(strings.Repeat("─", w)))
	created := ""
	if n := len(c.created); n > 0 {
		keys := make([]string, 0, n)
		for _, ci := range c.created {
			keys = append(keys, ci.key)
		}
		created = styleGreen.Render(truncate("Created: "+strings.Join(keys, ", "), w-40))
	}
	box := pick2(c.another, "[x]", "[ ]") + " Create another"
	if c.focus == focusAnother {
		box = styleSelected.Render(box)
	}
	button := c.chip(pick2(c.creating, "Creating…", "Create"), c.focus == focusCreate, true)
	right := box + "  " + button
	lines = append(lines, padRight(created, max(0, w-ansi.StringWidth(right)))+right, "",
		styleDim.Render("tab move · enter pick · ctrl+s create · ctrl+a create another · ctrl+e $EDITOR · ctrl+y copy created · esc close"))
	return styleModal.Render(styleSection.Render("New issue") + "\n\n" + strings.Join(lines, "\n"))
}

// wrapChips lays chips in rows of at most w cells.
func wrapChips(chips []string, w int) []string {
	var rows []string
	row := ""
	for _, ch := range chips {
		if row != "" && ansi.StringWidth(row)+1+ansi.StringWidth(ch) > w {
			rows = append(rows, row)
			row = ""
		}
		if row != "" {
			row += " "
		}
		row += ch
	}
	if row != "" {
		rows = append(rows, row)
	}
	return rows
}
