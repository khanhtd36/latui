package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/notify"
)

// pageSize is how many notification groups load at a time.
const pageSize = 50

// categories are the feed's tabs, in order.
var categories = [2]notify.Category{notify.Direct, notify.Watching}

func categoryTitle(c notify.Category) string {
	if c == notify.Watching {
		return "Watching"
	}
	return "Direct"
}

// notesPage is the Notifications page: two feeds and the Unread filter.
type notesPage struct {
	tab        int // index into categories
	unreadOnly bool
	views      [2]*feedView
	counts     [2]unreadCount
}

// unreadCount is how many unread groups a category has, up to a page.
type unreadCount struct {
	n     int
	more  bool // at least n: the first page was full
	known bool
}

func (c unreadCount) String() string {
	if !c.known || c.n == 0 {
		return ""
	}
	s := strconv.Itoa(c.n)
	if c.more {
		s += "+"
	}
	return s
}

// feedView is one category's list.
type feedView struct {
	groups      []notify.Group
	after       string
	hasMore     bool
	loaded      bool
	loading     bool // the first page is on its way
	loadingMore bool
	err         error
	fetchedAt   time.Time
	gen         int // bumps on every reload, to drop stale answers

	cursor int // index into groups
	offset int // first screen line shown
}

func newNotesPage() *notesPage {
	return &notesPage{views: [2]*feedView{{}, {}}}
}

type (
	feedMsg struct {
		tab    int
		gen    int
		more   bool // a next page, not the first
		page   notify.Page
		err    error
		unread bool
	}
	countMsg struct {
		tab   int
		count unreadCount
		err   error
	}
	markedMsg struct {
		all bool
		err error
	}
)

func (p *notesPage) view() *feedView { return p.views[p.tab] }

func fetchFeed(api feedAPI, tab, gen int, unread bool, after string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		page, err := api.Feed(ctx, categories[tab], unread, pageSize, after)
		return feedMsg{tab: tab, gen: gen, more: after != "", page: page, err: err, unread: unread}
	}
}

func fetchCount(api feedAPI, tab int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		page, err := api.Feed(ctx, categories[tab], true, pageSize, "")
		n := 0
		for _, g := range page.Groups {
			if g.Unread {
				n++
			}
		}
		// The unread-filtered feed says it has more even when it hasn't: only
		// a full page means there may be more.
		return countMsg{tab: tab, count: unreadCount{n: n, more: page.HasMore && len(page.Groups) >= pageSize, known: err == nil}, err: err}
	}
}

// reloadAll empties both lists and loads the open one and both counts.
func (p *notesPage) reloadAll(api feedAPI) tea.Cmd {
	cmds := []tea.Cmd{fetchCount(api, 0), fetchCount(api, 1)}
	for i, v := range p.views {
		v.gen++
		*v = feedView{gen: v.gen}
		if i == p.tab {
			v.loading = true
			cmds = append(cmds, fetchFeed(api, i, v.gen, p.unreadOnly, ""))
		}
	}
	return tea.Batch(cmds...)
}

// refresh reloads the first page of the open list (and of the other one
// once loaded) and the counts, keeping the cursor on the same group.
func (p *notesPage) refresh(api feedAPI) tea.Cmd {
	cmds := []tea.Cmd{fetchCount(api, 0), fetchCount(api, 1)}
	for i, v := range p.views {
		if i != p.tab && !v.loaded {
			continue
		}
		v.gen++
		v.loading = true
		cmds = append(cmds, fetchFeed(api, i, v.gen, p.unreadOnly, ""))
	}
	return tea.Batch(cmds...)
}

func (p *notesPage) onCount(msg countMsg) {
	if msg.err == nil {
		p.counts[msg.tab] = msg.count
	}
}

func (p *notesPage) onFeed(msg feedMsg, api feedAPI) tea.Cmd {
	v := p.views[msg.tab]
	if msg.gen != v.gen || msg.unread != p.unreadOnly {
		return nil // an answer for a list since reloaded
	}
	if msg.more {
		v.loadingMore = false
		if msg.err != nil {
			v.err = msg.err
			return nil
		}
		v.groups = appendNew(v.groups, msg.page.Groups)
		v.after, v.hasMore = msg.page.After, msg.page.HasMore
		return nil
	}
	v.loading = false
	if msg.err != nil {
		v.err = msg.err
		return nil
	}
	was := v.selected()
	v.err, v.loaded, v.fetchedAt = nil, true, time.Now()
	fresh := msg.page.Groups
	if len(v.groups) > len(fresh) && len(fresh) > 0 {
		// Keep the older groups loaded beyond the first page.
		last := fresh[len(fresh)-1].Head.Time
		var older []notify.Group
		for _, g := range v.groups {
			if g.Head.Time.Before(last) {
				older = append(older, g)
			}
		}
		fresh = appendNew(fresh, older)
	} else {
		v.after, v.hasMore = msg.page.After, msg.page.HasMore
	}
	v.groups = fresh
	v.keepCursor(was)
	return p.loadMoreIfNear(api)
}

// appendNew appends the groups of more not already in groups.
func appendNew(groups, more []notify.Group) []notify.Group {
	seen := make(map[string]bool, len(groups))
	for _, g := range groups {
		seen[g.ID] = true
	}
	for _, g := range more {
		if !seen[g.ID] {
			groups = append(groups, g)
			seen[g.ID] = true
		}
	}
	return groups
}

func (v *feedView) selected() *notify.Group {
	if v.cursor < 0 || v.cursor >= len(v.groups) {
		return nil
	}
	return &v.groups[v.cursor]
}

func (v *feedView) keepCursor(was *notify.Group) {
	if was != nil {
		id := was.ID
		for i, g := range v.groups {
			if g.ID == id {
				v.cursor = i
				return
			}
		}
	}
	v.cursor = max(0, min(v.cursor, len(v.groups)-1))
}

// loadMoreIfNear fetches the next page when the cursor nears the end.
func (p *notesPage) loadMoreIfNear(api feedAPI) tea.Cmd {
	v := p.view()
	if !v.hasMore || v.loadingMore || v.loading || v.cursor < len(v.groups)-5 {
		return nil
	}
	v.loadingMore = true
	return fetchFeed(api, p.tab, v.gen, p.unreadOnly, v.after)
}

func (p *notesPage) switchTab(api feedAPI, tab int) tea.Cmd {
	p.tab = tab
	if v := p.view(); !v.loaded && !v.loading {
		v.loading = true
		return fetchFeed(api, tab, v.gen, p.unreadOnly, "")
	}
	return nil
}

func (p *notesPage) key(msg tea.KeyMsg, api feedAPI, height int) tea.Cmd {
	v := p.view()
	listH := max(1, height-3)
	switch msg.String() {
	case "j", "down":
		v.move(1)
	case "k", "up":
		v.move(-1)
	case "ctrl+d", "pgdown":
		v.move(max(1, listH/8))
	case "ctrl+u", "pgup":
		v.move(-max(1, listH/8))
	case "g", "home":
		v.cursor = 0
	case "G", "end":
		v.cursor = max(0, len(v.groups)-1)
	case "J":
		v.jumpDay(1)
	case "K":
		v.jumpDay(-1)
	case "tab", "]", "shift+tab", "[":
		return p.switchTab(api, 1-p.tab)
	case "u":
		p.unreadOnly = !p.unreadOnly
		for i, fv := range p.views {
			fv.gen++
			*fv = feedView{gen: fv.gen}
			if i == p.tab {
				fv.loading = true
			}
		}
		return fetchFeed(api, p.tab, p.view().gen, p.unreadOnly, "")
	case "r":
		return p.refresh(api)
	case "m":
		if g := v.selected(); g != nil {
			return p.mark(api, g, g.Unread)
		}
	case "M":
		cat := categories[p.tab]
		return showModal(newConfirm("Mark all "+categoryTitle(cat)+" notifications as read?", func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			return markedMsg{all: true, err: api.MarkAllRead(ctx, cat)}
		}))
	case "enter", "o":
		g := v.selected()
		if g == nil {
			return nil
		}
		var cmds []tea.Cmd
		if g.Unread {
			cmds = append(cmds, p.mark(api, g, true))
		}
		if msg.String() == "enter" && g.Head.Key != "" {
			cmds = append(cmds, openIssueCmd(g.Head.Key))
		} else if g.Head.URL != "" {
			cmds = append(cmds, openURL(g.Head.URL, "opened "+cmpOr(g.Head.Key, g.Head.Title)))
		}
		return tea.Batch(cmds...)
	case "y":
		if g := v.selected(); g != nil {
			return copyMenu(cmpOr(g.Head.Key, "notification"),
				copyItem{"Link", g.Head.URL},
				copyItem{"Issue key", g.Head.Key},
				copyItem{"Title", g.Head.Title},
			)
		}
	}
	return p.loadMoreIfNear(api)
}

// mark marks a group read (or unread) here at once, then on the site; a
// failure reloads the list.
func (p *notesPage) mark(api feedAPI, g *notify.Group, read bool) tea.Cmd {
	g.Unread, g.Head.Unread = !read, !read
	c := &p.counts[p.tab]
	if c.known {
		if read {
			c.n = max(0, c.n-1)
		} else {
			c.n++
		}
	}
	group := *g
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return markedMsg{err: api.MarkGroup(ctx, group, read)}
	}
}

func (v *feedView) move(d int) {
	v.cursor = max(0, min(v.cursor+d, len(v.groups)-1))
}

// jumpDay moves to the first group of the next (or this/previous) day.
func (v *feedView) jumpDay(dir int) {
	if len(v.groups) == 0 {
		return
	}
	day := func(i int) string { return dayLabel(v.groups[i].Head.Time) }
	if dir > 0 {
		for i := v.cursor + 1; i < len(v.groups); i++ {
			if day(i) != day(v.cursor) {
				v.cursor = i
				return
			}
		}
		return
	}
	start := v.cursor
	for start > 0 && day(start-1) == day(start) {
		start--
	}
	if start == v.cursor && start > 0 {
		start--
		for start > 0 && day(start-1) == day(start) {
			start--
		}
	}
	v.cursor = start
}

// dayLabel groups the list like the web panel.
func dayLabel(t time.Time) string {
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Local().Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "Today"
	case now.AddDate(0, 0, -1).Format("2006-01-02") == t.Local().Format("2006-01-02"):
		return "Yesterday"
	}
	return "Older"
}

// --- View ---

// cardLines draws one group: who did what and when, the target, its
// issue key and status or Confluence space, and up to two lines of what
// was said.
func cardLines(g notify.Group, width int, selected bool) []string {
	n := g.Head
	inner := max(10, width-4)
	mark := " "
	msgStyle := styleDim
	if g.Unread {
		mark = styleCursor.Render(unreadMark())
		msgStyle = styleTitle
	}
	head := msgStyle.Render(n.Message)
	meta := relTime(n.Time)
	if g.Size > 1 {
		meta += " · +" + strconv.Itoa(g.Size-1)
	}
	lines := []string{mark + " " + truncate(head+styleDim.Render(" · "+meta), inner+2)}
	title := n.Title
	if title == "" {
		title = "(no title)"
	}
	if g.Unread {
		lines = append(lines, "  "+truncate(title, inner))
	} else {
		lines = append(lines, "  "+truncate(styleDim.Render(title), inner))
	}
	var ctx []string
	switch n.Product {
	case notify.Jira:
		if n.Key != "" {
			ctx = append(ctx, styleCyan.Render(n.Key))
		}
		if n.Status != "" {
			ctx = append(ctx, statusStyle(statusCategoryGuess(n.Status)).Render(n.Status))
		}
	case notify.Confluence:
		ctx = append(ctx, styleDim.Render("Confluence"))
		if n.Context != "" {
			ctx = append(ctx, styleDim.Render(n.Context))
		}
	case notify.Other:
		if n.Context != "" {
			ctx = append(ctx, styleDim.Render(n.Context))
		}
	}
	if len(ctx) > 0 {
		lines = append(lines, "  "+truncate(strings.Join(ctx, styleDim.Render(" · ")), inner))
	}
	if n.Snippet != "" {
		wrapped := wrap(n.Snippet, inner-2)
		if len(wrapped) > 2 {
			wrapped = wrapped[:2]
			wrapped[1] = truncate(wrapped[1]+" …", inner-2)
			if !strings.HasSuffix(wrapped[1], "…") {
				wrapped[1] += "…"
			}
		}
		for _, w := range wrapped {
			lines = append(lines, "  "+styleDim.Render("│ "+w))
		}
	}
	bar := "  "
	if selected {
		bar = styleCursor.Render("▌ ")
	}
	for i := range lines {
		lines[i] = bar + lines[i]
	}
	return lines
}

// statusCategoryGuess colors a status the feed names but doesn't
// categorize.
func statusCategoryGuess(status string) string {
	s := strings.ToLower(status)
	switch {
	case s == "done" || s == "closed" || s == "resolved" || strings.Contains(s, "complete"):
		return "done"
	case s == "to do" || s == "open" || s == "backlog" || s == "new":
		return "new"
	}
	return "indeterminate"
}

// listLines lays out the whole list with day headers; starts[i] and
// ends[i] are group i's first and past-last lines.
func (v *feedView) listLines(width int) (lines []string, starts, ends []int) {
	day := ""
	for i, g := range v.groups {
		if d := dayLabel(g.Head.Time); d != day {
			if day != "" {
				lines = append(lines, "")
			}
			day = d
			lines = append(lines, styleHeader.Render(d))
		}
		starts = append(starts, len(lines))
		lines = append(lines, cardLines(g, width, i == v.cursor)...)
		ends = append(ends, len(lines))
		lines = append(lines, "")
	}
	return lines, starts, ends
}

// render draws the list height lines tall, scrolled to keep the cursor's
// group in view.
func (v *feedView) render(width, height int, unreadOnly bool, more bool) []string {
	switch {
	case v.err != nil && len(v.groups) == 0:
		return padLines(errorLines(v.err, width), height)
	case !v.loaded && len(v.groups) == 0:
		return padLines([]string{styleYellow.Render("loading…")}, height)
	case len(v.groups) == 0:
		msg := "No notifications."
		if unreadOnly {
			msg = "No unread notifications. Press u to show all."
		}
		return padLines([]string{styleDim.Render(msg)}, height)
	}
	lines, starts, ends := v.listLines(width)
	if more {
		lines = append(lines, styleYellow.Render("loading more…"))
	}
	start, end := starts[v.cursor], ends[v.cursor]
	if start > 0 && lines[start-1] != "" {
		start-- // the day header above the group's first card
	}
	if start < v.offset {
		v.offset = start
	}
	if end > v.offset+height {
		v.offset = end - height
	}
	v.offset = max(0, min(v.offset, len(lines)-height))
	return padLines(lines[v.offset:], height)
}

// groupAt is the group drawn at screen line y of the list.
func (v *feedView) groupAt(width, y int) (int, bool) {
	_, starts, ends := v.listLines(width)
	line := v.offset + y
	for i := range starts {
		if line >= starts[i] && line < ends[i] {
			return i, true
		}
	}
	return 0, false
}

// errorLines shows an API error, with the schema hint for GraphQL errors.
func errorLines(err error, width int) []string {
	lines := []string{styleRed.Render(truncate("error: "+err.Error(), width))}
	var se *notify.SchemaError
	if asSchemaError(err, &se) {
		lines = append(lines, "",
			styleDim.Render("Atlassian may have changed the notifications API. Issue detail (ctrl+g) still works."),
			styleDim.Render("Press r to retry; if it keeps failing, update latui (U)."))
	}
	return lines
}

// tabLabels are the Direct and Watching tabs with their unread counts.
func (p *notesPage) tabLabels() []string {
	labels := make([]string, len(categories))
	for i, c := range categories {
		labels[i] = categoryTitle(c)
		if n := p.counts[i].String(); n != "" {
			labels[i] += " " + n
		}
	}
	return labels
}
