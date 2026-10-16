package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/actions"
)

// copyItem is one thing the Copy menu (y) can put on the clipboard.
type copyItem struct {
	label string
	value string
}

// copyMenuMsg asks the root to open the Copy menu; the first item is the
// most common choice, so y enter copies it.
type copyMenuMsg struct {
	title string
	items []copyItem
}

func copyMenu(title string, items ...copyItem) tea.Cmd {
	var kept []copyItem
	for _, it := range items {
		if it.value != "" {
			kept = append(kept, it)
		}
	}
	return func() tea.Msg { return copyMenuMsg{title: title, items: kept} }
}

// copyText puts text on the clipboard straight away.
func copyText(text, done string) tea.Cmd {
	return func() tea.Msg { return resultMsg(actions.CopyToClipboard(text), done) }
}

func openURL(u, done string) tea.Cmd {
	return func() tea.Msg { return resultMsg(actions.OpenBrowser(u), done) }
}

func newCopyMenu(msg copyMenuMsg) modal {
	items := make([]menuItem, 0, len(msg.items))
	for _, it := range msg.items {
		items = append(items, menuItem{
			label: it.label + styleDim.Render("  "+truncate(it.value, 60)),
			run: func() (modal, tea.Cmd) {
				return nil, copyText(it.value, "copied "+it.value)
			},
		})
	}
	return &menuModal{title: "Copy " + msg.title, items: items}
}
