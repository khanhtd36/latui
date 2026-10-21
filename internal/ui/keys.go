package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// binding documents one shortcut; this table is the single source for the
// help popup (?) and the footer hints. press is the key the help popup
// replays when the user runs the entry (empty: describe only); hint is the
// footer's short label (empty: help only).
type binding struct {
	keys  string
	desc  string
	press string
	hint  string
}

type bindingGroup struct {
	title    string
	bindings []binding
}

// The same keys mean the same thing everywhere outside text inputs, as in
// lazdo: j/k move, ctrl+d/ctrl+u half page, g/G ends, enter deeper, esc
// back one level, q quit, / filter or find, J/K next group, tab next pane,
// y copy menu, o browser, r refresh.
var (
	moveKeys = []binding{
		{keys: "j / k", desc: "move (or scroll) down / up", hint: "move"},
		{keys: "ctrl+d / ctrl+u", desc: "half page down / up", press: "ctrl+d"},
		{keys: "g / G", desc: "top / bottom", press: "g"},
	}
	notificationsKeys = []binding{
		{keys: "enter", desc: "open: a Jira issue in Issue detail, anything else in the browser; marks it read", press: "enter", hint: "open"},
		{keys: "tab", desc: "Direct ⇄ Watching", press: "tab", hint: "direct/watching"},
		{keys: "u", desc: "Unread filter: show only unread notifications, or all", press: "u", hint: "unread"},
		{keys: "m", desc: "mark the selected notification read, or unread again", press: "m", hint: "read"},
		{keys: "M", desc: "mark all as read (asks first)", press: "M", hint: "all read"},
		{keys: "J / K", desc: "next / previous day", press: "J"},
		{keys: "o", desc: "open the target in the browser; marks it read", press: "o", hint: "browser"},
		{keys: "y", desc: "copy menu: link, issue key, title", press: "y", hint: "copy"},
		{keys: "r", desc: "refresh", press: "r", hint: "refresh"},
	}
	pageKeys = []binding{
		{keys: "1 / 2", desc: "My issues / Notifications page", hint: "pages"},
		{keys: "[ / ]", desc: "previous / next page", press: "]"},
		{keys: "n", desc: "new issue (in Issue detail: or a subtask of it)", press: "n", hint: "new"},
		{keys: "ctrl+g", desc: "go to an issue by key, number or link", press: "ctrl+g", hint: "go to"},
		{keys: "U", desc: "check for a newer latui now, and offer to install it", press: "U"},
	}
	myIssuesKeys = []binding{
		{keys: "enter", desc: "open the issue; on a Section header, open or close the Section", press: "enter", hint: "open"},
		{keys: "tab", desc: "Assigned ⇄ Reported", press: "tab", hint: "assigned/reported"},
		{keys: "J / K", desc: "next / previous Section", press: "J", hint: "section"},
		{keys: "f", desc: "show a favourite filter's issues (esc back)", press: "f", hint: "filters"},
		{keys: "t", desc: "transition the selected issue", press: "t", hint: "status"},
		{keys: "c", desc: "comment on the selected issue", press: "c", hint: "comment"},
		{keys: "e / l / C / A", desc: "edit summary, labels, components, assignee", press: "A"},
		{keys: "b", desc: "copy a git branch name", press: "b"},
		{keys: "y", desc: "copy menu: key, link, summary, branch", press: "y", hint: "copy"},
		{keys: "o", desc: "open in the browser", press: "o", hint: "browser"},
		{keys: "r", desc: "refresh", press: "r", hint: "refresh"},
	}
	issueKeys = []binding{
		{keys: "tab / shift+tab", desc: "next / previous pane: content, fields", press: "tab", hint: "pane"},
		{keys: "1-4 / [ ]", desc: "Comments, History, Links, Attachments tab (scrolls to it)", press: "]", hint: "tabs"},
		{keys: "t", desc: "transition: move to another status", press: "t", hint: "status"},
		{keys: "c", desc: "add a comment (markdown; ctrl+e opens $EDITOR)", press: "c", hint: "comment"},
		{keys: "e", desc: "edit the summary", press: "e", hint: "summary"},
		{keys: "l", desc: "edit labels", press: "l", hint: "labels"},
		{keys: "C", desc: "edit components", press: "C"},
		{keys: "A", desc: "change the assignee (Me and Unassigned first)", press: "A", hint: "assignee"},
		{keys: "b", desc: "copy a git branch name for the issue", press: "b"},
		{keys: "y", desc: "copy menu: key, link, summary, branch", press: "y", hint: "copy"},
		{keys: "o", desc: "open in the browser (on Links or Attachments: the selected one)", press: "o", hint: "browser"},
		{keys: "enter", desc: "on Links: open the linked issue", press: "enter"},
		{keys: "r", desc: "refresh", press: "r"},
		{keys: "esc", desc: "back to the notifications", press: "esc", hint: "back"},
	}
	globalKeys = []binding{
		{keys: "?", desc: "this help", hint: "help"},
		{keys: "q", desc: "quit (anywhere outside a text box)", hint: "quit"},
		{keys: "ctrl+c", desc: "quit, even while typing"},
		{keys: "mouse", desc: "click selects, click again opens; drag selects text and copies it; wheel scrolls"},
	}
)

// helpGroups lists the bindings for where the user is, most specific first.
func (m Model) helpGroups() []bindingGroup {
	if m.issue != nil {
		return withCommon(bindingGroup{"Issue", issueKeys}, bindingGroup{"Anywhere", pageKeys})
	}
	if m.page == pageIssues {
		return withCommon(bindingGroup{"My issues", myIssuesKeys}, bindingGroup{"Anywhere", pageKeys})
	}
	return withCommon(bindingGroup{"Notifications", notificationsKeys}, bindingGroup{"Anywhere", pageKeys})
}

// withCommon appends the movement and global keys every view shares.
func withCommon(groups ...bindingGroup) []bindingGroup {
	return append(groups, bindingGroup{"Move", moveKeys}, bindingGroup{"Global", globalKeys})
}

// footerPart is one hint of the footer; press is the key a Click on it
// presses, empty when the hint only describes.
type footerPart struct {
	text, press string
}

// footerGap is the columns between footer hints.
const footerGap = 2

// footerParts lists the hints of the given groups for the bottom help line.
func footerParts(groups []bindingGroup) []footerPart {
	var parts []footerPart
	for _, g := range groups {
		for _, b := range g.bindings {
			if b.hint != "" {
				parts = append(parts, footerPart{text: strings.ReplaceAll(b.keys, " / ", "/") + " " + b.hint, press: b.press})
			}
		}
	}
	return parts
}

func footerText(parts []footerPart) string {
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.text
	}
	return strings.Join(texts, strings.Repeat(" ", footerGap))
}

// footer renders the hints of the given groups as the bottom help line.
func footer(groups []bindingGroup) string { return footerText(footerParts(groups)) }

// keyMsg turns a binding's press string back into the key it stands for.
func keyMsg(press string) tea.KeyMsg {
	switch press {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	if k, ok := strings.CutPrefix(press, "ctrl+"); ok && len(k) == 1 {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(k[0]-'a')}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(press)}
}
