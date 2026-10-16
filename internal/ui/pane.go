package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A screen's body is panes, each framed with its name in the top border
// and, when it scrolls, its position in the bottom one; the focused pane's
// frame is in the accent color, the others gray.

var styleFrame = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// pane is one framed area: width counts its frame, and lines are its
// content, drawn inside at width-2 by height-2. pos, such as "3/40", goes
// in the bottom edge.
type pane struct {
	title   string
	width   int
	lines   []string
	focused bool
	pos     string
}

// paneInnerHeight is the content height inside a pane's frame; the width
// inside is the pane's width less 2.
func paneInnerHeight(height int) int { return max(1, height-2) }

// framePanes draws the panes side by side, height rows tall in all.
func framePanes(height int, panes ...pane) []string {
	out := make([]string, max(2, height))
	for _, p := range panes {
		for i, line := range framePane(p, len(out)) {
			out[i] += line
		}
	}
	return out
}

// frameStyles are a pane's edge and title styles.
func frameStyles(focused bool) (edge, title lipgloss.Style) {
	if focused {
		return styleAccent, styleCursor
	}
	return styleFrame, styleDim
}

// bottomEdge closes a pane inner columns wide, with pos near its right end.
func bottomEdge(edge lipgloss.Style, inner int, pos string) string {
	if pos == "" || ansi.StringWidth(pos)+4 > inner {
		return edge.Render("└" + strings.Repeat("─", inner) + "┘")
	}
	fill := inner - ansi.StringWidth(pos) - 3
	return edge.Render("└"+strings.Repeat("─", fill)+" ") + styleDim.Render(pos) + edge.Render(" ─┘")
}

func framePane(p pane, height int) []string {
	edge, title := frameStyles(p.focused)
	inner := max(1, p.width-2)
	name := ""
	if p.title != "" {
		name = title.Render(" " + truncate(p.title, max(1, inner-3)) + " ")
	}
	fill := max(0, inner-1-ansi.StringWidth(name))
	out := make([]string, 0, height)
	out = append(out, edge.Render("┌─")+name+edge.Render(strings.Repeat("─", fill)+"┐"))
	for i := range max(0, height-2) {
		line := ""
		if i < len(p.lines) {
			line = p.lines[i]
		}
		out = append(out, edge.Render("│")+padRight(truncate(line, inner), inner)+edge.Render("│"))
	}
	return append(out, bottomEdge(edge, inner, p.pos))
}

// position is "n/total" for a cursor on item n of total, "" for none.
func position(n, total int) string {
	if total == 0 {
		return ""
	}
	return strconv.Itoa(min(n, total)) + "/" + strconv.Itoa(total)
}
