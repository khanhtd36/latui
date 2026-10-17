package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/notify"
)

const titlePrefix = "latui  "

func asSchemaError(err error, se **notify.SchemaError) bool { return errors.As(err, se) }

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.help != nil {
		return m.help.place(m.width, m.height)
	}
	if m.modal != nil {
		return placeModal(m.modal, m.width, m.height)
	}
	var body []string
	switch {
	case m.startErr != nil:
		body = signInHelp(m.startErr)
	case !m.started:
		body = []string{styleYellow.Render("signing in…")}
	case m.issue != nil:
		body = m.issue.view(m.width, m.bodyHeight())
	case m.page == pageIssues:
		body = m.issuesView()
	default:
		body = m.notesView()
	}
	var b strings.Builder
	b.WriteString(m.titleLine() + "\n")
	for _, line := range padLines(body, m.bodyHeight()) {
		b.WriteString(truncate(line, m.width) + "\n")
	}
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(styleDim.Render(truncate(footer(m.helpGroups()), m.width)))
	return b.String()
}

func (m Model) pageLabels() []string {
	label := "2 Notifications"
	if c := m.notes.counts[0].String(); c != "" {
		label += " (" + c + ")"
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
	loading, at := false, m.notes.view().fetchedAt
	if v := m.notes.view(); v.loading || v.loadingMore {
		loading = true
	}
	if m.page == pageIssues {
		l := m.issues.list()
		loading, at = l.loading, l.fetchedAt
	}
	if loading {
		s += styleYellow.Render("  refreshing…")
	} else if !at.IsZero() {
		s += styleDim.Render("  updated " + at.Format("15:04:05"))
	}
	return truncate(s+m.updateNote(), m.width)
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
	return append([]string{tabs}, framePanes(h, pane{title: title, width: m.width, lines: list, focused: true, pos: pos})...)
}

// --- Mouse ---

func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.modal != nil || m.help != nil || !m.started {
		return m, nil
	}
	if isClick(msg) && msg.Y == 0 && m.issue == nil {
		if p, ok := tabAt(m.pageLabels(), msg.X-len(titlePrefix), m.width-len(titlePrefix)); ok {
			return m.switchPage(p)
		}
		return m, nil
	}
	if m.issue == nil && m.page == pageIssues {
		if d := wheelDelta(msg); d != 0 {
			m.issues.list().move(d, m.issues.coll())
		}
		return m, nil
	}
	if d := wheelDelta(msg); d != 0 {
		if m.issue != nil {
			m.issue.scrollKey(pick2(d > 0, "j", "k"), m.bodyHeight())
			return m, nil
		}
		v := m.notes.view()
		v.move(sign(d))
		return m, m.notes.loadMoreIfNear(m.feed)
	}
	if !isClick(msg) || m.issue != nil {
		return m, nil
	}
	// The list starts below the title, the tabs and the pane's top edge.
	if msg.Y == 1 {
		if tab, ok := tabAt(m.notes.tabLabels(), msg.X, m.width-30); ok {
			return m, m.notes.switchTab(m.feed, tab)
		}
		return m, nil
	}
	v := m.notes.view()
	i, ok := v.groupAt(m.width-2, msg.Y-3)
	if !ok {
		return m, nil
	}
	if i == v.cursor {
		return m.Update(keyMsg("enter"))
	}
	v.cursor = i
	return m, m.notes.loadMoreIfNear(m.feed)
}

func isClick(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
}

func wheelDelta(msg tea.MouseMsg) int {
	if msg.Action != tea.MouseActionPress {
		return 0
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	default:
		return 0
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
		head = styleDim.Render("  " + truncate(p.filter.JQL, m.width-20) + "  (esc back)")
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
	return append([]string{head}, framePanes(h, pane{title: title, width: m.width, lines: list, focused: true, pos: pos})...)
}
