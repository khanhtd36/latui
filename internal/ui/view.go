package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/latui/internal/notify"
)

const titlePrefix = "latui  "

func asSchemaError(err error, se **notify.SchemaError) bool { return errors.As(err, se) }

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	h := m.hits
	h.reset()
	var lines []string
	switch {
	case m.help != nil:
		lines = strings.Split(m.helpOver(), "\n")
	case m.modal != nil:
		lines = strings.Split(m.modalOver(), "\n")
	default:
		lines = m.pageLines()
	}
	if h != nil {
		h.screen = lines
	}
	return strings.Join(h.highlight(lines), "\n")
}

// pageLines is the screen under any popup: the title, the page, the status
// and the footer.
func (m Model) pageLines() []string {
	var body []string
	switch {
	case m.startErr != nil:
		body = signInHelp(m.startErr)
	case !m.started:
		body = []string{styleYellow.Render("signing in…")}
	case m.issue != nil:
		body = m.issue.view(m.width, m.bodyHeight(), m.hits)
	case m.page == pageIssues:
		body = m.issuesView()
	default:
		body = m.notesView()
	}
	padded := padLines(body, m.bodyHeight())
	lines := make([]string, 0, len(padded)+3)
	lines = append(lines, m.titleLine())
	for _, line := range padded {
		lines = append(lines, truncate(line, m.width))
	}
	lines = append(lines, m.statusLine())
	foot := footerParts(m.helpGroups())
	lines = append(lines, styleDim.Render(truncate(footerText(foot), m.width)))
	m.titleRegions()
	m.footerRegions(foot, len(lines)-1)
	return lines
}

// titleRegions makes the page tabs in the title line clickable.
func (m Model) titleRegions() {
	if m.issue != nil || m.startErr != nil || !m.started {
		return
	}
	x := len(titlePrefix)
	for i, w := range tabWidths(m.pageLabels(), m.width-len(titlePrefix)) {
		m.hits.add(region{x: x, y: 0, w: w, h: 1, click: func(mm *Model, _, _ int) tea.Cmd {
			return mm.apply(mm.switchPage(i))
		}})
		x += w + 1
	}
}

// footerRegions makes each footer hint press its key.
func (m Model) footerRegions(parts []footerPart, y int) {
	x := 0
	for _, p := range parts {
		w := ansi.StringWidth(p.text)
		if p.press != "" && x < m.width {
			m.hits.add(region{x: x, y: y, w: min(w, m.width-x), h: 1, click: func(mm *Model, _, _ int) tea.Cmd {
				return mm.press(p.press)
			}})
		}
		x += w + footerGap
	}
}

// helpOver draws the help popup over the screen.
func (m Model) helpOver() string {
	out := m.placeOver(m.help.view())
	m.hits.area(m.modalFrame().inner())
	return out
}

// modalOver draws the open modal over the screen. A modal that takes
// clicks says so, and one without a text box can have its text selected.
func (m Model) modalOver() string {
	out := m.placeOver(m.modal.view(m.width))
	f := m.modalFrame()
	if c, ok := m.modal.(clicker); ok {
		w, _ := m.modal.(wheeler)
		reg := region{x: f.x, y: f.y, w: f.w, h: f.h, click: func(mm *Model, x, y int) tea.Cmd {
			cmd := mm.clickModal(x-f.x, y-f.y, c)
			return cmd
		}}
		if w != nil {
			reg.wheel = func(_ *Model, d int) tea.Cmd { w.wheel(d); return nil }
		}
		m.hits.add(reg)
	}
	if s, ok := m.modal.(selectable); ok && s.selectable() {
		m.hits.area(f.inner())
	}
	return out
}

func (m Model) pageLabels() []string {
	label := "2 Notifications"
	if m.notes.unseenKnown && m.notes.unseen > 0 {
		label += fmt.Sprintf(" (%d)", m.notes.unseen)
	}
	issues := "1 My issues"
	if l := m.issues.views[tabAssigned]; l.loaded {
		issues += fmt.Sprintf(" (%d)", len(l.rows))
	}
	return []string{issues, label}
}

func (m Model) titleLine() string {
	s := styleTitle.Render(strings.TrimSpace(titlePrefix)) + "  "
	active := m.page
	if m.issue != nil {
		active = -1 // Issue detail covers the page
	}
	s += tabBar(m.pageLabels(), active, m.width-len(titlePrefix))
	if m.site.Site != "" {
		s += styleDim.Render("   " + strings.TrimSuffix(m.site.Site, ".atlassian.net"))
	}
	if m.me.DisplayName != "" {
		s += styleDim.Render(" · " + m.me.DisplayName)
	}
	v := m.notes.view()
	loading, cached, at := v.loading || v.loadingMore, v.cached, v.fetchedAt
	if m.page == pageIssues {
		l := m.issues.list()
		loading, cached, at = l.loading, l.cached, l.fetchedAt
	}
	switch {
	case cached:
		// The last run's list: say how old it is while the fresh one loads.
		s += styleYellow.Render("  cached " + at.Local().Format("Jan 2 15:04") + pick2(loading, " · refreshing…", ""))
	case loading:
		s += styleYellow.Render("  refreshing…")
	case !at.IsZero():
		s += styleDim.Render("  updated " + at.Format("15:04:05"))
	}
	// A Click on the word about updates is U: the dialog, or the restart.
	if note := m.updateNote(); note != "" {
		x := ansi.StringWidth(s)
		m.hits.add(region{x: x, y: 0, w: min(ansi.StringWidth(note), m.width-x), h: 1, click: func(mm *Model, _, _ int) tea.Cmd { return mm.press("U") }})
		s += note
	}
	return truncate(s, m.width)
}

func (m Model) statusLine() string {
	if strings.HasPrefix(m.status, "error: ") {
		return styleRed.Render(truncate(m.status, m.width))
	}
	return truncate(m.status, m.width)
}

// notesView is the Notifications page: the Direct and Watching tabs with
// the Unread filter beside them, then the list.
func (m Model) notesView() []string {
	p := m.notes
	labels := p.tabLabels()
	box := pick2(p.unreadOnly, "[x]", "[ ]")
	tabs := tabBar(labels, p.tab, m.width-30) + "   " + styleDim.Render(box+" only unread (u)")
	h := m.bodyHeight() - 1
	v := p.view()
	title := categoryTitle(categories[p.tab])
	if p.unreadOnly {
		title += " · unread"
	}
	if v.err != nil && len(v.groups) > 0 {
		title += " · " + styleRed.Render("refresh failed")
	}
	list := v.render(m.width-2, paneInnerHeight(h), p.unreadOnly, v.loadingMore)
	pos := ""
	if len(v.groups) > 0 {
		pos = position(v.cursor+1, len(v.groups))
		if v.hasMore {
			pos += "+"
		}
	}
	m.notesRegions(tabs, h, len(v.groups) > 0)
	return append([]string{tabs}, framePanes(h, pane{title: title, width: m.width, lines: list, focused: true, pos: pos})...)
}

// notesRegions makes the Notifications page clickable: the tabs, the
// Unread filter and the groups. The list starts on screen row 3, under the
// title, the tabs and the pane's top edge.
func (m Model) notesRegions(tabs string, h int, hasGroups bool) {
	p := m.notes
	x := 0
	for i, w := range tabWidths(p.tabLabels(), m.width-30) {
		m.hits.add(region{x: x, y: 1, w: w, h: 1, click: func(mm *Model, _, _ int) tea.Cmd { return mm.notes.switchTab(mm.feed, i) }})
		x += w + 1
	}
	box := "[ ] only unread (u)"
	m.hits.add(region{x: ansi.StringWidth(tabs) - ansi.StringWidth(box), y: 1, w: ansi.StringWidth(box), h: 1, click: func(mm *Model, _, _ int) tea.Cmd { return mm.press("u") }})
	inner := paneInnerHeight(h)
	m.hits.area(rect{1, 3, m.width - 2, inner})
	if !hasGroups {
		return
	}
	v := p.view()
	_, starts, ends := v.listLines(m.width - 2)
	for i := range starts {
		first, last := max(starts[i], v.offset), min(ends[i], v.offset+inner)
		if first >= last {
			continue
		}
		m.hits.add(region{x: 1, y: 3 + first - v.offset, w: m.width - 2, h: last - first, click: func(mm *Model, _, _ int) tea.Cmd {
			v := mm.notes.view()
			if i == v.cursor {
				return mm.press("enter")
			}
			v.cursor = i
			return mm.notes.loadMoreIfNear(mm.feed)
		}})
	}
}

// issuesView is the My issues page: Assigned and Reported, or a
// Favourite filter's issues, in Sections by status.
func (m Model) issuesView() []string {
	p := m.issues
	var head string
	title := issueTabNames[p.tab]
	if p.filter != nil {
		title = "filter: " + p.filter.Name
		head = styleDim.Render("  " + truncate(oneLine(p.filter.JQL), m.width-20) + "  (esc back)")
	} else {
		head = tabBar(p.tabLabels(), p.tab, m.width-30) + "   " + styleDim.Render("f favourite filters")
	}
	h := m.bodyHeight() - 1
	l := p.list()
	if l.err != nil && len(l.rows) > 0 {
		title += " · " + styleRed.Render("refresh failed")
	}
	pos := ""
	if n := len(l.lines(p.coll())); n > 0 {
		pos = position(l.cursor+1, n)
	}
	list := l.render(m.width-2, paneInnerHeight(h), p.coll())
	m.issuesRegions(head, paneInnerHeight(h))
	return append([]string{head}, framePanes(h, pane{title: title, width: m.width, lines: list, focused: true, pos: pos})...)
}

// issuesRegions makes My issues clickable: the tabs or the way back from a
// filter, the filters hint, and the lines of the list. The list starts on
// screen row 3, under the title, the tabs and the pane's top edge.
func (m Model) issuesRegions(head string, inner int) {
	p := m.issues
	if p.filter != nil {
		hint := "(esc back)"
		x := ansi.StringWidth(head) - len(hint)
		m.hits.add(region{x: x, y: 1, w: len(hint), h: 1, click: func(mm *Model, _, _ int) tea.Cmd { return mm.press("esc") }})
	} else {
		x := 0
		for i, w := range tabWidths(p.tabLabels(), m.width-30) {
			m.hits.add(region{x: x, y: 1, w: w, h: 1, click: func(mm *Model, _, _ int) tea.Cmd { return mm.issues.setTab(i, mm.jira) }})
			x += w + 1
		}
		hint := "f favourite filters"
		m.hits.add(region{x: ansi.StringWidth(head) - len(hint), y: 1, w: len(hint), h: 1, click: func(mm *Model, _, _ int) tea.Cmd { return mm.press("f") }})
	}
	m.hits.area(rect{1, 3, m.width - 2, inner})
	l := p.list()
	lines := l.lines(p.coll())
	for i := l.offset; i < min(len(lines), l.offset+inner); i++ {
		m.hits.add(region{x: 1, y: 3 + i - l.offset, w: m.width - 2, h: 1, click: func(mm *Model, _, _ int) tea.Cmd {
			return mm.clickIssueLine(i)
		}})
	}
}

// clickIssueLine acts on a Click on line i of the My issues list: a Section
// header opens or closes at once, and a row selects, then opens.
func (m *Model) clickIssueLine(i int) tea.Cmd {
	p := m.issues
	l := p.list()
	lines := l.lines(p.coll())
	if i >= len(lines) {
		return nil
	}
	was := l.cursor
	l.cursor = i
	if lines[i].row == nil {
		p.coll()[lines[i].status] = !p.coll()[lines[i].status]
		m.savePrefs()
		return nil
	}
	if was == i {
		return openIssueCmd(lines[i].row.Key)
	}
	return nil
}

// setTab shows Assigned or Reported, loading it when it has not been.
func (p *issuesPage) setTab(tab int, api jiraAPI) tea.Cmd {
	p.tab = tab
	if l := p.list(); !l.loaded && !l.loading {
		return p.load(api)
	}
	return nil
}
