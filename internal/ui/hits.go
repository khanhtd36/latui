package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// What the mouse can reach is recorded while the screen is drawn: every
// draw clears the hit map and the code that draws a tab, a row or a button
// adds a region for it, in screen cells. A Click then looks the cell up, so
// nothing recomputes a layout to find what was hit.

// region is a rectangle of screen cells that reacts to the mouse. click
// gets the cell hit; wheel gets the notches (negative is up). Either may be
// nil.
type region struct {
	x, y, w, h int
	click      func(m *Model, x, y int) tea.Cmd
	wheel      func(m *Model, d int) tea.Cmd
}

func (r region) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// rect is a rectangle of screen cells.
type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// selection is a Selection: the cells from where the drag began to where it
// is, inside one text area.
type selection struct {
	area   rect
	ax, ay int // where the drag began
	bx, by int // where it is
}

// span is the columns of row y the selection covers, as [from, to).
func (s selection) span(y int) (from, to int, ok bool) {
	ax, ay, bx, by := s.ax, s.ay, s.bx, s.by
	if ay > by || ay == by && ax > bx {
		ax, ay, bx, by = bx, by, ax, ay
	}
	if y < ay || y > by {
		return 0, 0, false
	}
	from, to = s.area.x, s.area.x+s.area.w
	if y == ay {
		from = ax
	}
	if y == by {
		to = bx + 1
	}
	return max(from, s.area.x), min(to, s.area.x+s.area.w), true
}

// hitMap is what the last draw left for the mouse, and the mouse's state.
// The Model is copied on every update, so this lives behind a pointer.
type hitMap struct {
	regions []region
	areas   []rect // where text can be selected
	screen  []string
	modal   *rect // the frame of the open modal or help

	down     bool // the left button is held
	dx, dy   int  // the cell it went down on
	downArea int  // index into areas of the cell it went down on, -1 for none
	dragging bool // it has left that cell
	sel      *selection
}

func newHitMap() *hitMap { return &hitMap{downArea: -1} }

func (h *hitMap) reset() {
	if h == nil {
		return
	}
	h.regions, h.areas, h.modal = h.regions[:0], h.areas[:0], nil
}

func (h *hitMap) add(r region) {
	if h != nil && r.w > 0 && r.h > 0 {
		h.regions = append(h.regions, r)
	}
}

func (h *hitMap) area(r rect) {
	if h != nil && r.w > 0 && r.h > 0 {
		h.areas = append(h.areas, r)
	}
}

// regionAt is the region on cell x, y; one added later lies over an earlier.
func (h *hitMap) regionAt(x, y int, want func(region) bool) (region, bool) {
	for i := len(h.regions) - 1; i >= 0; i-- {
		if r := h.regions[i]; r.has(x, y) && want(r) {
			return r, true
		}
	}
	return region{}, false
}

func (h *hitMap) areaAt(x, y int) int {
	for i := len(h.areas) - 1; i >= 0; i-- {
		if h.areas[i].has(x, y) {
			return i
		}
	}
	return -1
}

func (h *hitMap) clearSel() {
	if h != nil {
		h.sel = nil
	}
}

// clamp puts cell x, y inside the rectangle.
func (r rect) clamp(x, y int) (int, int) {
	return max(r.x, min(x, r.x+r.w-1)), max(r.y, min(y, r.y+r.h-1))
}

// highlight draws the Selection over the finished screen lines.
func (h *hitMap) highlight(lines []string) []string {
	if h == nil || h.sel == nil {
		return lines
	}
	out := append([]string(nil), lines...)
	rev := lipgloss.NewStyle().Reverse(true)
	for y := h.sel.area.y; y < min(len(out), h.sel.area.y+h.sel.area.h); y++ {
		from, to, ok := h.sel.span(y)
		if !ok || to <= from {
			continue
		}
		w := ansi.StringWidth(out[y])
		if from >= w {
			continue
		}
		to = min(to, w)
		out[y] = ansi.Cut(out[y], 0, from) + rev.Render(ansi.Strip(ansi.Cut(out[y], from, to))) + ansi.Cut(out[y], to, w)
	}
	return out
}

// selectedText is the text under the Selection, a row per line.
func (h *hitMap) selectedText() string {
	if h == nil || h.sel == nil {
		return ""
	}
	var rows []string
	for y := h.sel.area.y; y < min(len(h.screen), h.sel.area.y+h.sel.area.h); y++ {
		from, to, ok := h.sel.span(y)
		if !ok {
			continue
		}
		row := ""
		if w := ansi.StringWidth(h.screen[y]); to > from && from < w {
			row = ansi.Strip(ansi.Cut(h.screen[y], from, min(to, w)))
		}
		rows = append(rows, strings.TrimRight(row, " "))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// --- Mouse ---

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

// apply keeps a Model a click handler changed through the pointer it was
// given.
func (m *Model) apply(nm tea.Model, cmd tea.Cmd) tea.Cmd {
	*m = nm.(Model)
	return cmd
}

// press runs a key as if it were typed.
func (m *Model) press(k string) tea.Cmd {
	nm, cmd := m.onKey(keyMsg(k))
	return m.apply(nm, cmd)
}

// onMouse tells a Click from a drag: a press and a release on one cell is a
// Click, and acts on release; anything that moves between them is a
// Selection, copied on release.
func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	h := m.hits
	if h == nil || !m.started && m.modal == nil && m.help == nil {
		return m, nil
	}
	if d := wheelDelta(msg); d != 0 {
		h.clearSel()
		return m.onWheel(msg, d)
	}
	switch {
	case isClick(msg):
		h.clearSel()
		h.down, h.dx, h.dy, h.dragging = true, msg.X, msg.Y, false
		h.downArea = h.areaAt(msg.X, msg.Y)
	case msg.Action == tea.MouseActionMotion && h.down:
		if h.downArea < 0 || msg.X == h.dx && msg.Y == h.dy && !h.dragging {
			break
		}
		h.dragging = true
		area := h.areas[h.downArea]
		bx, by := area.clamp(msg.X, msg.Y)
		h.sel = &selection{area: area, ax: h.dx, ay: h.dy, bx: bx, by: by}
	case msg.Action == tea.MouseActionRelease && h.down:
		h.down = false
		if h.dragging {
			h.dragging = false
			return m, m.copySelection()
		}
		if msg.X != h.dx || msg.Y != h.dy {
			return m, nil
		}
		return m.onClick(msg.X, msg.Y)
	}
	return m, nil
}

// copySelection puts the Selection on the clipboard.
func (m Model) copySelection() tea.Cmd {
	text := m.hits.selectedText()
	if strings.TrimSpace(text) == "" {
		m.hits.clearSel()
		return nil
	}
	n := len([]rune(text))
	return copyText(text, fmt.Sprintf("copied %d characters", n))
}

func (m Model) onClick(x, y int) (tea.Model, tea.Cmd) {
	r, ok := m.hits.regionAt(x, y, func(r region) bool { return r.click != nil })
	if !ok {
		if (m.modal != nil || m.help != nil) && m.hits.modal != nil && !m.hits.modal.has(x, y) {
			return m.dismissOutside()
		}
		return m, nil
	}
	cmd := r.click(&m, x, y)
	m, hover := m.hover()
	return m, tea.Batch(cmd, hover)
}

// stayer is a modal an outside Click does not close: it asks a question
// that a stray Click must not answer.
type stayer interface{ stays() bool }

// dismissOutside closes the open modal or help as esc would, so one with
// something to lose still asks first.
func (m Model) dismissOutside() (tea.Model, tea.Cmd) {
	if m.help != nil {
		return m.onKey(keyMsg("esc"))
	}
	if s, ok := m.modal.(stayer); ok && s.stays() {
		return m, nil
	}
	return m.onKey(keyMsg("esc"))
}

// onWheel scrolls what is under the pointer: a region with a wheel, else
// the page.
func (m Model) onWheel(msg tea.MouseMsg, d int) (tea.Model, tea.Cmd) {
	if r, ok := m.hits.regionAt(msg.X, msg.Y, func(r region) bool { return r.wheel != nil }); ok {
		cmd := r.wheel(&m, d)
		return m, cmd
	}
	if m.modal != nil || m.help != nil {
		return m, nil
	}
	switch {
	case m.issue != nil:
		m.issue.scrollKey(pick2(d > 0, "j", "k"), m.bodyHeight())
		return m, nil
	case m.page == pageIssues:
		m.issues.list().move(d, m.issues.coll())
		return m, nil
	}
	v := m.notes.view()
	v.move(sign(d))
	return m, m.notes.loadMoreIfNear(m.feed)
}

// placeOver centers a popup on the screen and records where its frame is.
func (m Model) placeOver(s string) string {
	w, h := lipgloss.Width(s), lipgloss.Height(s)
	// lipgloss.Place leaves the larger half of an odd spare room below and to
	// the right.
	x, y := (m.width-w)/2, (m.height-h)/2
	if m.width < w {
		x = 0
	}
	if m.height < h {
		y = 0
	}
	if m.hits != nil {
		m.hits.modal = &rect{x, y, w, h}
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, s)
}

// modalFrame is the open popup's frame in screen cells.
func (m Model) modalFrame() rect {
	if m.hits == nil || m.hits.modal == nil {
		return rect{}
	}
	return *m.hits.modal
}

// inner is the frame without its border and padding: the text a popup
// holds.
func (r rect) inner() rect { return rect{r.x + 2, r.y + 1, r.w - 4, r.h - 2} }

// clicker is a modal that takes Clicks, at cells counted from its top left
// corner, border included.
type clicker interface {
	click(x, y int) (modal, tea.Cmd)
}

// wheeler is a modal the wheel moves.
type wheeler interface{ wheel(d int) }

// selectable is a modal whose text can be selected; one with a text box
// leaves the mouse to its caret.
type selectable interface{ selectable() bool }

func (m *Model) clickModal(x, y int, c clicker) tea.Cmd {
	next, cmd := c.click(x, y)
	m.modal = next
	return cmd
}
