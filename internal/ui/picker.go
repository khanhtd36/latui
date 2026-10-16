package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// pickOption is one choice in a picker.
type pickOption struct {
	id, label, detail string
	checked           bool
	create            bool // "create label …", from what was typed
}

// pickerResultsMsg brings a picker's search results for a query.
type pickerResultsMsg struct {
	query   string
	options []pickOption
	err     error
}

// pickerModal picks one option (assignee) or several (labels,
// components). Typing filters, and with search set also asks the site;
// ↑/↓ move; enter picks one, or toggles one with multi; ctrl+s saves the
// checked ones.
type pickerModal struct {
	title       string
	multi       bool
	allowCreate bool // typing a new value offers it, as labels do
	noSpaces    bool // values can't hold spaces, as labels can't
	input       textinput.Model
	options     []pickOption // fixed or pinned options; checked ones stay
	found       []pickOption // the last search's results
	searching   string       // the query asked for, "" when idle
	err         error
	cursor      int
	search      func(q string) tea.Cmd
	onSave      func(chosen []pickOption) tea.Cmd
	initial     string // the checked IDs when opened, to tell a change
}

func newPicker(title string, multi bool, options []pickOption, onSave func([]pickOption) tea.Cmd) *pickerModal {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "type to filter"
	in.Width = 40
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	p := &pickerModal{title: title, multi: multi, input: in, options: options, onSave: onSave}
	p.initial = p.checkedKey()
	return p
}

func (p *pickerModal) checkedKey() string {
	var ids []string
	for _, o := range p.options {
		if o.checked {
			ids = append(ids, o.id)
		}
	}
	return strings.Join(ids, "\x00")
}

func (p *pickerModal) results(msg pickerResultsMsg) {
	if msg.query != strings.TrimSpace(p.input.Value()) {
		return // typed on since
	}
	p.searching, p.found, p.err = "", msg.options, msg.err
	p.clamp()
}

// visible is what the list shows: options matching the query, then search
// results not already there, then "create" for a new value.
func (p *pickerModal) visible() []pickOption {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	var out []pickOption
	seen := map[string]bool{}
	for _, o := range p.options {
		if q == "" || strings.Contains(strings.ToLower(o.label), q) || o.checked && p.multi {
			out = append(out, o)
			seen[o.id] = true
		}
	}
	for _, o := range p.found {
		if !seen[o.id] {
			out = append(out, o)
			seen[o.id] = true
		}
	}
	if p.allowCreate && q != "" {
		v := strings.TrimSpace(p.input.Value())
		exact := false
		for _, o := range out {
			if strings.EqualFold(o.id, v) {
				exact = true
			}
		}
		if !exact {
			out = append(out, pickOption{id: v, label: v, create: true})
		}
	}
	return out
}

func (p *pickerModal) clamp() {
	p.cursor = max(0, min(p.cursor, len(p.visible())-1))
}

func (p *pickerModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if p.multi && p.checkedKey() != p.initial {
			return &confirmModal{prompt: "Discard your changes?", back: p}, nil
		}
		return nil, nil
	case "up", "ctrl+p", "ctrl+k":
		p.cursor = max(0, p.cursor-1)
		return p, nil
	case "down", "ctrl+n", "ctrl+j":
		p.cursor++
		p.clamp()
		return p, nil
	case "ctrl+s":
		if !p.multi {
			return p.pick()
		}
		if p.checkedKey() == p.initial {
			return nil, statusCmd("nothing changed")
		}
		var chosen []pickOption
		for _, o := range p.options {
			if o.checked {
				chosen = append(chosen, o)
			}
		}
		return nil, p.onSave(chosen)
	case "enter":
		if !p.multi {
			return p.pick()
		}
		p.toggle()
		return p, nil
	}
	if p.noSpaces && msg.Type == tea.KeySpace {
		return p, nil
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() == before {
		return p, cmd
	}
	p.cursor = 0
	q := strings.TrimSpace(p.input.Value())
	if p.search != nil {
		p.searching = q
		cmd = tea.Batch(cmd, p.search(q))
	}
	return p, cmd
}

func (p *pickerModal) pick() (modal, tea.Cmd) {
	vis := p.visible()
	if p.cursor >= len(vis) {
		return p, nil
	}
	return nil, p.onSave([]pickOption{vis[p.cursor]})
}

// toggle checks or unchecks the option under the cursor; a found or
// created one joins the options, so it stays when the query changes.
func (p *pickerModal) toggle() {
	vis := p.visible()
	if p.cursor >= len(vis) {
		return
	}
	o := vis[p.cursor]
	for i := range p.options {
		if p.options[i].id == o.id {
			p.options[i].checked = !p.options[i].checked
			return
		}
	}
	o.checked, o.create = true, false
	p.options = append(p.options, o)
	p.input.SetValue("")
	p.found = nil
	if p.search != nil {
		p.searching = ""
	}
}

const pickerRows = 12

func (p *pickerModal) view(width int) string {
	w := min(80, max(40, width-10))
	vis := p.visible()
	lines := []string{styleSection.Render(p.title), "", p.input.View(), ""}
	start := max(0, p.cursor-pickerRows+1)
	for i := start; i < min(len(vis), start+pickerRows); i++ {
		o := vis[i]
		prefix := "  "
		if i == p.cursor {
			prefix = styleSelected.Render("› ")
		}
		box := ""
		if p.multi {
			box = pick2(o.checked, "[x] ", "[ ] ")
		}
		label := o.label
		if o.create {
			label = styleCyan.Render("+ new: ") + o.label
		}
		if o.detail != "" {
			label += styleDim.Render("  " + o.detail)
		}
		lines = append(lines, truncate(prefix+box+label, w))
	}
	switch {
	case p.err != nil:
		lines = append(lines, styleRed.Render(truncate("error: "+p.err.Error(), w)))
	case p.searching != "":
		lines = append(lines, styleYellow.Render("searching…"))
	case len(vis) == 0:
		lines = append(lines, styleDim.Render("  no match"))
	}
	hint := "↑/↓ move · enter pick · esc cancel"
	if p.multi {
		hint = "↑/↓ move · enter check/uncheck · ctrl+s save · esc cancel"
	}
	lines = append(lines, "", styleDim.Render(hint))
	return styleModal.Render(strings.Join(lines, "\n"))
}

// inputModal asks for one line of text.
type inputModal struct {
	title, hint string
	input       textinput.Model
	onSubmit    func(string) (modal, tea.Cmd)
}

func newInput(title, value, hint string, onSubmit func(string) (modal, tea.Cmd)) *inputModal {
	in := textinput.New()
	in.Prompt = "› "
	in.SetValue(value)
	in.CharLimit = 255
	in.Width = 70
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	return &inputModal{title: title, hint: hint, input: in, onSubmit: onSubmit}
}

func (i *inputModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "enter":
		return i.onSubmit(i.input.Value())
	}
	var cmd tea.Cmd
	i.input, cmd = i.input.Update(msg)
	return i, cmd
}

func (i *inputModal) view(width int) string {
	i.input.Width = min(70, max(30, width-16))
	return styleModal.Render(styleSection.Render(i.title) + "\n\n" + i.input.View() + "\n\n" + styleDim.Render(i.hint))
}
