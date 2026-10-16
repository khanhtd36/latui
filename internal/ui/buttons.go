package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// buttonBar is a row of buttons, one focused: enter presses it. The
// focused one is blue, the others grey, like the title's vote and complete.
type buttonBar struct {
	labels []string
	focus  int
	center bool // centered in the dialog, else at its left
}

const buttonGap = 2

func (b buttonBar) widths() []int {
	ws := make([]int, len(b.labels))
	for i, l := range b.labels {
		ws[i] = ansi.StringWidth(styleButton.Render(l))
	}
	return ws
}

func (b buttonBar) width() int {
	w := 0
	for _, x := range b.widths() {
		w += x
	}
	return w + buttonGap*(len(b.labels)-1)
}

// pad is the space before the first button in a content row of width w.
func (b buttonBar) pad(w int) int {
	if !b.center {
		return 0
	}
	return max(0, (w-b.width())/2)
}

func (b buttonBar) render(w int) string {
	parts := make([]string, len(b.labels))
	for i, l := range b.labels {
		parts[i] = styleButton.Render(l)
		if i == b.focus {
			parts[i] = styleButtonCTA.Render(l)
		}
	}
	return strings.Repeat(" ", b.pad(w)) + strings.Join(parts, strings.Repeat(" ", buttonGap))
}

// move handles the keys that walk the buttons; false for any other key.
func (b *buttonBar) move(k string) bool {
	n := len(b.labels)
	switch k {
	case "left", "h", "shift+tab":
		b.focus = (b.focus + n - 1) % n
	case "right", "l", "tab":
		b.focus = (b.focus + 1) % n
	default:
		return false
	}
	return true
}
