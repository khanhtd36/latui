package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/prefs"
)

// My issues tabs.
const (
	tabAssigned = iota
	tabReported
	issueTabCount
)

var issueTabJQL = [issueTabCount]string{
	"assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC",
	"reporter = currentUser() AND statusCategory != Done ORDER BY updated DESC",
}

var issueTabNames = [issueTabCount]string{"Assigned", "Reported"}

// statusOrder is the order of Sections; other statuses follow by name.
var statusOrder = []string{"In Progress", "To Do", "Open", "QA Verification", "Fixed", "Deferred"}

// collapsedByDefault are the Sections closed until opened.
var collapsedByDefault = map[string]bool{"Fixed": true, "Deferred": true}

// issuesPage is the My issues page: Assigned and Reported, or the issues
// of a Favourite filter in their place.
type issuesPage struct {
	tab       int
	views     [issueTabCount]*issueList
	filter    *jira.Filter // non-nil while a Favourite filter replaces the list
	filtered  *issueList
	collapsed map[string]bool // by status name
	// filterCollapsed are a filter view's closed Sections: all open at first,
	// and not remembered.
	filterCollapsed map[string]bool
	// onFresh is told Assigned or Reported came in, for the cache.
	onFresh func()
}

// issueList is one list of rows in Sections.
type issueList struct {
	rows      []jira.Row
	loaded    bool
	loading   bool
	err       error
	fetchedAt time.Time
	cached    bool // rows are the last run's, until fresh ones come
	gen       int

	cursor int // index into lines()
	offset int
}

// issueLine is a screen line of the list: a Section header or a row.
type issueLine struct {
	status string
	count  int       // a header's row count
	row    *jira.Row // nil for a header
}

func newIssuesPage(p prefs.Prefs) *issuesPage {
	collapsed := p.Collapsed
	if collapsed == nil {
		collapsed = map[string]bool{}
		for s, c := range collapsedByDefault {
			collapsed[s] = c
		}
	}
	return &issuesPage{views: [issueTabCount]*issueList{{}, {}}, collapsed: collapsed}
}

type (
	searchMsg struct {
		tab    int // -1 for a filter
		filter string
		gen    int
		rows   []jira.Row
		err    error
	}
	filtersMsg struct {
		filters []jira.Filter
		err     error
	}
)

func (p *issuesPage) list() *issueList {
	if p.filter != nil {
		return p.filtered
	}
	return p.views[p.tab]
}

func search(api jiraAPI, tab int, filter string, gen int, jql string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		rows, err := api.Search(ctx, jql)
		return searchMsg{tab: tab, filter: filter, gen: gen, rows: rows, err: err}
	}
}

// load fetches the open list, or refreshes it.
func (p *issuesPage) load(api jiraAPI) tea.Cmd {
	l := p.list()
	l.gen++
	l.loading = true
	if p.filter != nil {
		return search(api, -1, p.filter.ID, l.gen, p.filter.JQL)
	}
	return search(api, p.tab, "", l.gen, issueTabJQL[p.tab])
}

// refresh reloads the lists already loaded.
func (p *issuesPage) refresh(api jiraAPI) tea.Cmd {
	var cmds []tea.Cmd
	for i, l := range p.views {
		if l.loaded || l.loading {
			l.gen++
			l.loading = true
			cmds = append(cmds, search(api, i, "", l.gen, issueTabJQL[i]))
		}
	}
	if p.filter != nil {
		p.filtered.gen++
		p.filtered.loading = true
		cmds = append(cmds, search(api, -1, p.filter.ID, p.filtered.gen, p.filter.JQL))
	}
	return tea.Batch(cmds...)
}

func (p *issuesPage) onSearch(msg searchMsg) {
	var l *issueList
	switch {
	case msg.tab >= 0:
		l = p.views[msg.tab]
	case p.filter != nil && p.filter.ID == msg.filter:
		l = p.filtered
	default:
		return
	}
	if msg.gen != l.gen {
		return
	}
	l.loading = false
	if msg.err != nil {
		l.err = msg.err
		return
	}
	coll := p.collapsed
	if l == p.filtered {
		coll = p.filterCollapsed
	}
	was := l.selectedKey(coll)
	l.rows, l.err, l.loaded, l.cached, l.fetchedAt = msg.rows, nil, true, false, time.Now()
	if p.onFresh != nil && msg.tab >= 0 {
		p.onFresh()
	}
	l.keepCursor(was, coll)
}

// sectionOrder ranks a status: the known ones first, in statusOrder.
func sectionOrder(status string) int {
	for i, s := range statusOrder {
		if strings.EqualFold(s, status) {
			return i
		}
	}
	return len(statusOrder)
}

// lines lays the rows out in Sections, collapsed ones as just a header.
func (l *issueList) lines(collapsed map[string]bool) []issueLine {
	by := map[string][]*jira.Row{}
	var names []string
	for i := range l.rows {
		r := &l.rows[i]
		s := cmpOr(r.Status.Name, "No status")
		if _, ok := by[s]; !ok {
			names = append(names, s)
		}
		by[s] = append(by[s], r)
	}
	sort.SliceStable(names, func(i, j int) bool {
		oi, oj := sectionOrder(names[i]), sectionOrder(names[j])
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	var out []issueLine
	for _, s := range names {
		rows := by[s]
		sort.SliceStable(rows, func(i, j int) bool { return jira.Time(rows[i].Updated).After(jira.Time(rows[j].Updated)) })
		out = append(out, issueLine{status: s, count: len(rows)})
		if collapsed[s] {
			continue
		}
		for _, r := range rows {
			out = append(out, issueLine{status: s, row: r})
		}
	}
	return out
}

func (l *issueList) selected(collapsed map[string]bool) (issueLine, bool) {
	lines := l.lines(collapsed)
	if l.cursor < 0 || l.cursor >= len(lines) {
		return issueLine{}, false
	}
	return lines[l.cursor], true
}

// selectedKey names what the cursor is on, to find it again: an issue
// key, or "#status" for a header.
func (l *issueList) selectedKey(collapsed map[string]bool) string {
	line, ok := l.selected(collapsed)
	switch {
	case !ok:
		return ""
	case line.row == nil:
		return "#" + line.status
	}
	return line.row.Key
}

func (l *issueList) keepCursor(key string, collapsed map[string]bool) {
	lines := l.lines(collapsed)
	if key != "" {
		for i, line := range lines {
			if line.row == nil && "#"+line.status == key || line.row != nil && line.row.Key == key {
				l.cursor = i
				return
			}
		}
	}
	l.cursor = max(0, min(l.cursor, len(lines)-1))
}

func (l *issueList) move(d int, collapsed map[string]bool) {
	l.cursor = max(0, min(l.cursor+d, len(l.lines(collapsed))-1))
}

// jumpSection moves to the next (or previous) Section's header.
func (l *issueList) jumpSection(dir int, collapsed map[string]bool) {
	lines := l.lines(collapsed)
	for i := l.cursor + dir; i >= 0 && i < len(lines); i += dir {
		if lines[i].row == nil {
			l.cursor = i
			return
		}
	}
}

// rowAction is a key on the selected row that acts on its issue.
type rowActionMsg struct {
	key    string // issue key
	action string // the Issue detail key to run
}

func (p *issuesPage) key(msg tea.KeyMsg, api jiraAPI, site string, height int, save func()) tea.Cmd {
	l := p.list()
	k := msg.String()
	switch k {
	case "j", "down":
		l.move(1, p.coll())
	case "k", "up":
		l.move(-1, p.coll())
	case "ctrl+d", "pgdown":
		l.move(max(1, height/2), p.coll())
	case "ctrl+u", "pgup":
		l.move(-max(1, height/2), p.coll())
	case "g", "home":
		l.cursor = 0
	case "G", "end":
		l.cursor = max(0, len(l.lines(p.coll()))-1)
	case "J":
		l.jumpSection(1, p.coll())
	case "K":
		l.jumpSection(-1, p.coll())
	case "tab", "shift+tab":
		if p.filter != nil {
			return nil
		}
		p.tab = (p.tab + 1) % issueTabCount
		if l := p.list(); !l.loaded && !l.loading {
			return p.load(api)
		}
	case "esc":
		if p.filter != nil {
			p.filter, p.filtered = nil, nil
		}
	case "r":
		return p.refresh(api)
	case "f":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			fs, err := api.FavouriteFilters(ctx)
			return filtersMsg{filters: fs, err: err}
		}
	}
	line, ok := l.selected(p.coll())
	if !ok {
		return nil
	}
	if line.row == nil {
		if k == "enter" || k == " " {
			p.coll()[line.status] = !p.coll()[line.status]
			save()
		}
		return nil
	}
	r := line.row
	switch k {
	case "enter":
		return openIssueCmd(r.Key)
	case "o":
		return openURL("https://"+site+"/browse/"+r.Key, "opened "+r.Key)
	case "y":
		return copyMenu(r.Key,
			copyItem{"Key", r.Key},
			copyItem{"Link", "https://" + site + "/browse/" + r.Key},
			copyItem{"Summary", r.Summary},
			copyItem{"Key and summary", r.Key + " " + r.Summary},
			copyItem{"Branch name", jira.BranchName(r.Key, r.Summary)},
		)
	case "b":
		name := jira.BranchName(r.Key, r.Summary)
		return copyText(name, "copied "+name)
	case "t", "c", "e", "l", "C", "A":
		action := rowActionMsg{key: r.Key, action: k}
		return func() tea.Msg { return action }
	}
	return nil
}

// filterMenu picks a Favourite filter to show in place of the list.
func (p *issuesPage) filterMenu(fs []jira.Filter, api jiraAPI) modal {
	if len(fs) == 0 {
		return &menuModal{title: "No favourite filters", items: []menuItem{{label: "Close", run: func() (modal, tea.Cmd) { return nil, nil }}}}
	}
	opts := make([]pickOption, 0, len(fs))
	for _, f := range fs {
		opts = append(opts, pickOption{id: f.ID, label: f.Name})
	}
	return newPicker("Favourite filters", false, opts, func(chosen []pickOption) tea.Cmd {
		for _, f := range fs {
			if len(chosen) == 1 && f.ID == chosen[0].id {
				p.filter, p.filtered, p.filterCollapsed = &f, &issueList{}, map[string]bool{}
				return p.load(api)
			}
		}
		return nil
	})
}

// --- View ---

// typeLetter stands for an issue type in one column.
func typeLetter(t string) string {
	switch strings.ToLower(t) {
	case "bug":
		return styleRed.Render("B")
	case "epic":
		return stylePurple.Render("E")
	case "subtask", "sub-task":
		return styleCyan.Render("s")
	case "":
		return " "
	}
	return styleCyan.Render(strings.ToUpper(t[:1]))
}

func priorityMark(p string) string {
	switch strings.ToLower(p) {
	case "highest", "blocker", "critical":
		return styleRed.Render("⇈")
	case "high", "major":
		return styleRed.Render("↑")
	case "medium":
		return styleYellow.Render("=")
	case "low", "minor":
		return styleCyan.Render("↓")
	case "lowest", "trivial":
		return styleDim.Render("⇊")
	}
	return " "
}

// dueBadge shows a due date within a week, red when past.
func dueBadge(due string) string {
	if due == "" {
		return ""
	}
	t, err := time.ParseInLocation("2006-01-02", due, time.Local)
	if err != nil {
		return ""
	}
	days := int(time.Until(t.Add(24*time.Hour)).Hours() / 24)
	switch {
	case days < 0:
		return styleRed.Render("overdue " + t.Format("Jan 2"))
	case days <= 7:
		return styleYellow.Render("due " + t.Format("Jan 2"))
	}
	return ""
}

func (l *issueList) rowLine(r *jira.Row, width int, keyW int) string {
	right := styleDim.Render(fit(relTime(jira.Time(r.Updated)), 9))
	if d := dueBadge(r.Due); d != "" {
		right = d + "  " + right
	}
	if r.Parent != "" && width >= 100 {
		right = styleDim.Render(r.Parent) + "  " + right
	}
	left := typeLetter(r.IssueType) + " " + styleCyan.Render(fit(r.Key, keyW)) + " " + priorityMark(r.Priority) + " "
	room := width - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
	return left + fit(r.Summary, max(10, room)) + "  " + right
}

func (l *issueList) render(width, height int, collapsed map[string]bool) []string {
	switch {
	case l.err != nil && len(l.rows) == 0:
		return padLines(errorLines(l.err, width), height)
	case !l.loaded:
		return padLines([]string{styleYellow.Render("loading…")}, height)
	case len(l.rows) == 0:
		return padLines([]string{styleDim.Render("Nothing here.")}, height)
	}
	lines := l.lines(collapsed)
	keyW := 0
	for _, r := range l.rows {
		keyW = max(keyW, len(r.Key))
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+height {
		l.offset = l.cursor - height + 1
	}
	l.offset = max(0, min(l.offset, len(lines)-height))
	out := make([]string, 0, height)
	for i := l.offset; i < min(len(lines), l.offset+height); i++ {
		line := lines[i]
		prefix := "  "
		if i == l.cursor {
			prefix = styleCursor.Render("▌ ")
		}
		if line.row == nil {
			mark := "▾ "
			if collapsed[line.status] {
				mark = "▸ "
			}
			out = append(out, prefix+styleHeader.Render(mark+line.status)+styleDim.Render(fmt.Sprintf(" (%d)", line.count)))
			continue
		}
		out = append(out, prefix+"  "+l.rowLine(line.row, width-4, keyW))
	}
	return padLines(out, height)
}

// tabLabels are Assigned and Reported with their counts.
func (p *issuesPage) tabLabels() []string {
	labels := make([]string, issueTabCount)
	for i, name := range issueTabNames {
		labels[i] = name
		if l := p.views[i]; l.loaded {
			labels[i] += fmt.Sprintf(" %d", len(l.rows))
		}
	}
	return labels
}

// coll is the closed Sections of the list on screen.
func (p *issuesPage) coll() map[string]bool {
	if p.filter != nil {
		return p.filterCollapsed
	}
	return p.collapsed
}

// showCached puts the last run's rows up until fresh ones come.
func (p *issuesPage) showCached(tab int, rows []jira.Row, saved time.Time) {
	l := p.views[tab]
	if len(rows) == 0 || l.loaded {
		return
	}
	l.rows, l.loaded, l.cached, l.fetchedAt = rows, true, true, saved
}

// loadStart loads the open tab, and any tab showing cached rows.
func (p *issuesPage) loadStart(api jiraAPI) tea.Cmd {
	var cmds []tea.Cmd
	for i, l := range p.views {
		if i == p.tab || l.cached {
			l.gen++
			l.loading = true
			cmds = append(cmds, search(api, i, "", l.gen, issueTabJQL[i]))
		}
	}
	return tea.Batch(cmds...)
}
