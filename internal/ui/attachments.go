package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/actions"
	"github.com/khanhtd36/latui/internal/attach"
	"github.com/khanhtd36/latui/internal/jira"
)

// An attachment is saved into the Saved folder first, then shown: text in the
// viewer, anything else in the system app, except what could run code.

// attachPurpose is why an attachment is being saved.
type attachPurpose int

const (
	purposeSave attachPurpose = iota // only save it
	purposeView                      // save it, then show or open it
)

type (
	// attachStartMsg asks the Model to save an attachment.
	attachStartMsg struct {
		key     string // the issue's
		file    jira.Attachment
		purpose attachPurpose
	}
	// attachDoneMsg is a finished save.
	attachDoneMsg struct {
		attachStartMsg
		saved attach.Saved
		err   error
	}
	attachTickMsg struct{}
)

// saveRun is a save in progress; the copy writes its counts, the status line
// reads them.
type saveRun struct {
	name        string
	done, total atomic.Int64
}

func (r *saveRun) String() string {
	done, total := r.done.Load(), r.total.Load()
	if total <= 0 {
		return fmt.Sprintf("saving %s %s", r.name, megabytes(done))
	}
	return fmt.Sprintf("saving %s %s/%s", r.name, megabytes(done), megabytes(total))
}

func megabytes(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

func attachTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return attachTickMsg{} })
}

// startAttach saves an attachment in the background; one runs at a time.
func (m Model) startAttach(msg attachStartMsg) (tea.Model, tea.Cmd) {
	if m.saving != nil {
		return m, statusCmd("already " + m.saving.String())
	}
	run := &saveRun{name: attach.SafeName(msg.file.Filename)}
	run.total.Store(msg.file.Size)
	m.saving = run
	m.status = run.String()
	api := m.jira
	save := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		open := func(ctx context.Context) (io.ReadCloser, int64, error) { return api.Download(ctx, msg.file) }
		saved, err := attach.Save(ctx, open, attach.Folder(msg.key), msg.file.Filename, msg.file.Size, func(done, total int64) {
			run.done.Store(done)
			run.total.Store(total)
		})
		return attachDoneMsg{attachStartMsg: msg, saved: saved, err: err}
	}
	return m, tea.Batch(save, attachTick())
}

func (m Model) onAttachTick() (tea.Model, tea.Cmd) {
	if m.saving == nil {
		return m, nil
	}
	m.status = m.saving.String()
	return m, attachTick()
}

// onAttachDone shows what a finished save asked for.
func (m Model) onAttachDone(msg attachDoneMsg) (tea.Model, tea.Cmd) {
	m.saving = nil
	if msg.err != nil {
		m.status = "error: " + msg.err.Error()
		var api *jira.APIError
		if errors.As(msg.err, &api) && api.Status == 404 {
			m.status = "error: " + msg.file.Filename + " is gone from " + msg.key + ", reloading"
			if d, ok := m.knownIssues[msg.key]; ok {
				return m, d.load()
			}
		}
		return m, nil
	}
	path := msg.saved.Path
	m.status = pick2(msg.saved.Reused, "already saved: ", "saved: ") + path
	if msg.purpose == purposeSave {
		return m, nil
	}
	if attach.TextLike(msg.file.Filename, msg.file.MimeType) {
		if text, ok, err := attach.ReadText(path); err == nil && ok {
			m.modal = newViewer(msg.file.Filename, path, text)
			m.status = ""
			return m, nil
		}
	}
	if attach.Risky(msg.file.Filename) {
		m.status = "saved, not opened: it could run code. " + path + " (f opens the folder)"
		return m, nil
	}
	return m, func() tea.Msg { return resultMsg(openFile(path), "opened "+path) }
}

// selectedAttachment is the Attachments row the cursor is on.
func (d *issueModel) selectedAttachment() (jira.Attachment, bool) {
	if d.issue == nil || d.tab != tabAttachments {
		return jira.Attachment{}, false
	}
	as := d.issue.Fields.Attachments
	if d.cursor[tabAttachments] < 0 || d.cursor[tabAttachments] >= len(as) {
		return jira.Attachment{}, false
	}
	return as[d.cursor[tabAttachments]], true
}

// attachCmd asks the Model to save the selected attachment.
func (d *issueModel) attachCmd(purpose attachPurpose) tea.Cmd {
	a, ok := d.selectedAttachment()
	if !ok {
		return nil
	}
	msg := attachStartMsg{key: d.key, file: a, purpose: purpose}
	return func() tea.Msg { return msg }
}

// openFolder shows the issue's saved folder, with the selected attachment
// picked out when it is saved; with nothing saved it says so.
func (d *issueModel) openFolder() tea.Cmd {
	dir := attach.Folder(d.key)
	target := dir
	if a, ok := d.selectedAttachment(); ok {
		if p := filepath.Join(dir, attach.SafeName(a.Filename)); fileExists(p) {
			target = p
		}
	}
	if !fileExists(target) {
		return statusCmd("nothing saved for " + d.key + " yet: s saves an attachment")
	}
	return func() tea.Msg { return resultMsg(revealFile(target), "opened "+target) }
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// attachCopyMenu copies the selected attachment's name or link.
func (d *issueModel) attachCopyMenu() tea.Cmd {
	a, ok := d.selectedAttachment()
	if !ok {
		return nil
	}
	return copyMenu(a.Filename,
		copyItem{"File name", a.Filename},
		copyItem{"Link", d.site.SiteURL() + "/rest/api/3/attachment/content/" + a.ID},
		copyItem{"Saved path", filepath.Join(attach.Folder(d.key), attach.SafeName(a.Filename))},
	)
}

// --- Viewer ---

// viewerModal shows a saved text file, scrolling.
type viewerModal struct {
	title, path string
	lines       []string
	scroll      int
	w, h        int // the screen, set before each draw
	wrapped     []string
	wrapW       int
}

func newViewer(title, path, text string) *viewerModal {
	return &viewerModal{title: title, path: path, lines: strings.Split(strings.TrimRight(text, "\n"), "\n")}
}

// sizer is a modal that wants the screen's size before it draws.
type sizer interface{ size(w, h int) }

func (v *viewerModal) size(w, h int) { v.w, v.h = w, h }

func (v *viewerModal) inner() (w, h int) { return max(20, v.w-6), max(5, v.h-6) }

func (v *viewerModal) wrap() []string {
	w, _ := v.inner()
	if v.wrapped == nil || v.wrapW != w {
		v.wrapped, v.wrapW = nil, w
		for _, l := range v.lines {
			l = strings.ReplaceAll(l, "\t", "    ")
			if l == "" {
				v.wrapped = append(v.wrapped, "")
				continue
			}
			v.wrapped = append(v.wrapped, wrap(l, w)...)
		}
	}
	return v.wrapped
}

func (v *viewerModal) move(d int) {
	_, h := v.inner()
	v.scroll = max(0, min(v.scroll+d, max(0, len(v.wrap())-h)))
}

func (v *viewerModal) wheel(d int) { v.move(d * wheelLines) }

func (v *viewerModal) selectable() bool { return true }

func (v *viewerModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	_, h := v.inner()
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "j", "down":
		v.move(1)
	case "k", "up":
		v.move(-1)
	case "ctrl+d", "pgdown", " ":
		v.move(max(1, h/2))
	case "ctrl+u", "pgup":
		v.move(-max(1, h/2))
	case "g", "home":
		v.move(-1 << 30)
	case "G", "end":
		v.move(1 << 30)
	case "s":
		return v, statusCmd("saved: " + v.path)
	case "f":
		return v, func() tea.Msg { return resultMsg(revealFile(v.path), "opened the folder") }
	case "o":
		if attach.Risky(v.path) {
			return v, statusCmd("not opened: it could run code. " + v.path)
		}
		return v, func() tea.Msg { return resultMsg(openFile(v.path), "opened "+v.path) }
	}
	return v, nil
}

func (v *viewerModal) view(int) string {
	w, h := v.inner()
	lines := v.wrap()
	v.move(0) // keep the scroll in range after a resize
	shown := lines[min(v.scroll, len(lines)):min(len(lines), v.scroll+h)]
	pos := ""
	if len(lines) > h {
		pos = fmt.Sprintf("  %d%%", 100*v.scroll/max(1, len(lines)-h))
	}
	out := []string{styleSection.Render(truncate(v.title, w-len(pos))) + styleDim.Render(pos), ""}
	for i := range h {
		if i < len(shown) {
			out = append(out, truncate(shown[i], w))
		} else {
			out = append(out, "")
		}
	}
	out = append(out, "", styleDim.Render("j/k scroll · g/G ends · o open in the system app · f folder · esc close"))
	return styleModal.Width(w + 2).Render(strings.Join(out, "\n"))
}

// openFile and revealFile start the system's app and file manager; tests
// replace them.
var (
	openFile   = actions.OpenFile
	revealFile = actions.Reveal
)
