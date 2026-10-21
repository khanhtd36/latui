package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khanhtd36/latui/internal/attach"
	"github.com/khanhtd36/latui/internal/jira"
)

// attachmentsRig is Issue detail on the Attachments tab, with the files saved
// in a temporary Downloads folder and the system's apps replaced.
type attachmentsRig struct {
	m        Model
	j        *fakeJira
	dir      string   // Downloads
	opened   []string // files handed to the system app
	revealed []string
}

func newAttachmentsRig(t *testing.T) *attachmentsRig {
	t.Helper()
	r := &attachmentsRig{dir: t.TempDir()}
	restore := attach.SetDownloads(func() (string, error) { return r.dir, nil })
	oldOpen, oldReveal := openFile, revealFile
	openFile = func(p string) error { r.opened = append(r.opened, p); return nil }
	revealFile = func(p string) error { r.revealed = append(r.revealed, p); return nil }
	t.Cleanup(func() { restore(); openFile, revealFile = oldOpen, oldReveal })

	issue := sampleIssue()
	issue.Fields.Attachments = []jira.Attachment{
		{ID: "1", Filename: "build.log", Size: 18, MimeType: "text/plain"},
		{ID: "2", Filename: "setup.exe", Size: 6, MimeType: "application/x-msdownload"},
		{ID: "3", Filename: "shot.png", Size: 4, MimeType: "image/png"},
		{ID: "4", Filename: "run.ps1", Size: 9, MimeType: "text/plain"},
	}
	r.j = &fakeJira{issue: issue, files: map[string]string{
		"1": "line one\nline two\n", "2": "MZ....", "3": "\x89PNG", "4": "echo hi\n\n",
	}}
	m := started(t, r.j, sampleFeed())
	m = run(t, m, openIssueMsg{key: "DEMO-42"})
	m = run(t, m, key("4"))
	m.View()
	r.m = m
	return r
}

// press runs a key and draws, as the runtime does.
func (r *attachmentsRig) press(t *testing.T, k string) {
	t.Helper()
	r.m = run(t, r.m, key(k))
	r.m.View()
}

func (r *attachmentsRig) down(t *testing.T, n int) {
	t.Helper()
	for range n {
		r.press(t, "j")
	}
}

func (r *attachmentsRig) saved(name string) string {
	return filepath.Join(r.dir, "latui", "DEMO-42", name)
}

func TestEnterOnTextOpensTheViewer(t *testing.T) {
	r := newAttachmentsRig(t)
	r.press(t, "enter")
	if _, ok := r.m.modal.(*viewerModal); !ok {
		t.Fatalf("enter on a text file did not open the viewer: %T (status %q)", r.m.modal, r.m.status)
	}
	if s := screen(r.m); !strings.Contains(s, "build.log") || !strings.Contains(s, "line one") || !strings.Contains(s, "line two") {
		t.Errorf("the viewer lacks the file:\n%s", s)
	}
	if b, _ := os.ReadFile(r.saved("build.log")); string(b) != "line one\nline two\n" {
		t.Errorf("the saved file holds %q", b)
	}
	if len(r.opened) != 0 {
		t.Error("a text file was also opened in the system app")
	}
	r.press(t, "f")
	if len(r.revealed) != 1 || r.revealed[0] != r.saved("build.log") {
		t.Errorf("f in the viewer revealed %v", r.revealed)
	}
	r.press(t, "esc")
	if r.m.modal != nil {
		t.Errorf("esc left the viewer open")
	}
}

func TestEnterOnAnImageOpensTheSystemApp(t *testing.T) {
	r := newAttachmentsRig(t)
	r.down(t, 2)
	r.press(t, "enter")
	if len(r.opened) != 1 || r.opened[0] != r.saved("shot.png") {
		t.Errorf("opened %v, status %q", r.opened, r.m.status)
	}
	if r.m.modal != nil {
		t.Errorf("an image opened a modal: %T", r.m.modal)
	}
}

func TestAProgramIsSavedButNeverOpened(t *testing.T) {
	r := newAttachmentsRig(t)
	r.down(t, 1)
	r.press(t, "enter")
	if len(r.opened) != 0 {
		t.Fatalf("a program was opened: %v", r.opened)
	}
	if !strings.Contains(r.m.status, "saved, not opened") || !strings.Contains(r.m.status, "setup.exe") {
		t.Errorf("status %q", r.m.status)
	}
	if _, err := os.Stat(r.saved("setup.exe")); err != nil {
		t.Errorf("the program was not saved: %v", err)
	}
}

func TestAScriptShowsAsTextButIsNotOpened(t *testing.T) {
	r := newAttachmentsRig(t)
	r.down(t, 3)
	r.press(t, "enter")
	if _, ok := r.m.modal.(*viewerModal); !ok {
		t.Fatalf("a script's text should show in the viewer: %T", r.m.modal)
	}
	r.press(t, "o")
	if len(r.opened) != 0 || !strings.Contains(r.m.status, "could run code") {
		t.Errorf("o on a script: opened %v, status %q", r.opened, r.m.status)
	}
}

func TestSaveKeySavesWithoutOpeningAndReusesTheCopy(t *testing.T) {
	r := newAttachmentsRig(t)
	r.press(t, "s")
	if !strings.HasPrefix(r.m.status, "saved: ") || len(r.opened) != 0 || r.m.modal != nil {
		t.Fatalf("status %q, opened %v, modal %T", r.m.status, r.opened, r.m.modal)
	}
	r.press(t, "s")
	if !strings.HasPrefix(r.m.status, "already saved: ") {
		t.Errorf("a second save said %q", r.m.status)
	}
}

func TestFolderKey(t *testing.T) {
	r := newAttachmentsRig(t)
	r.press(t, "f")
	if len(r.revealed) != 0 || !strings.Contains(r.m.status, "nothing saved for DEMO-42") {
		t.Fatalf("before any save: revealed %v, status %q", r.revealed, r.m.status)
	}
	r.press(t, "s")
	r.press(t, "f")
	if len(r.revealed) != 1 || r.revealed[0] != r.saved("build.log") {
		t.Errorf("the saved file should be picked out: %v", r.revealed)
	}
	r.down(t, 2) // shot.png, not saved: the folder opens
	r.press(t, "f")
	if len(r.revealed) != 2 || r.revealed[1] != filepath.Dir(r.saved("x")) {
		t.Errorf("an unsaved row should open the folder: %v", r.revealed)
	}
}

func TestClickOnTheSelectedRowViewsIt(t *testing.T) {
	r := newAttachmentsRig(t)
	r.m = clickText(t, r.m, "build.log")
	if r.m.modal == nil {
		t.Fatal("a click on the selected first row should open it")
	}
}

func TestDeletedAttachmentReloadsTheIssue(t *testing.T) {
	r := newAttachmentsRig(t)
	r.j.downloadErr = &jira.APIError{Status: 404, Messages: []string{"not found"}}
	r.press(t, "enter")
	if !strings.Contains(r.m.status, "is gone from DEMO-42") || r.m.modal != nil {
		t.Errorf("status %q, modal %T", r.m.status, r.m.modal)
	}
}

func TestDownloadErrorIsReportedAndLeavesNothing(t *testing.T) {
	r := newAttachmentsRig(t)
	r.j.downloadErr = os.ErrDeadlineExceeded
	r.press(t, "s")
	if !strings.HasPrefix(r.m.status, "error: ") {
		t.Errorf("status %q", r.m.status)
	}
	if entries, _ := os.ReadDir(filepath.Dir(r.saved("x"))); len(entries) != 0 {
		t.Errorf("a failed save left %d files", len(entries))
	}
}

func TestViewerScrolls(t *testing.T) {
	r := newAttachmentsRig(t)
	long := strings.Repeat("a long line that wraps around the viewer ", 6) + "\n" + strings.Repeat("row\n", 80) + "the end"
	r.j.files["1"] = long
	r.j.issue.Fields.Attachments[0].Size = int64(len(long))
	r.press(t, "enter")
	v, ok := r.m.modal.(*viewerModal)
	if !ok {
		t.Fatalf("no viewer: %T (%q)", r.m.modal, r.m.status)
	}
	if strings.Contains(screen(r.m), "the end") {
		t.Fatal("the end of a long file is already on screen")
	}
	r.press(t, "G")
	if !strings.Contains(screen(r.m), "the end") || v.scroll == 0 {
		t.Errorf("G did not reach the end:\n%s", screen(r.m))
	}
	r.press(t, "g")
	if v.scroll != 0 {
		t.Errorf("g left scroll at %d", v.scroll)
	}
}

func TestFilesOfOtherEncodingsAreOpenedNotShown(t *testing.T) {
	r := newAttachmentsRig(t)
	r.j.files["1"] = "caf\xe9 au lait" // Latin-1
	r.j.issue.Fields.Attachments[0].Size = int64(len(r.j.files["1"]))
	r.press(t, "enter")
	if r.m.modal != nil || len(r.opened) != 1 {
		t.Errorf("modal %T, opened %v", r.m.modal, r.opened)
	}
}

func TestSaveProgressText(t *testing.T) {
	run := &saveRun{name: "big.zip"}
	run.total.Store(8 << 20)
	run.done.Store(3<<20 + 100<<10)
	if got := run.String(); got != "saving big.zip 3.1 MB/8.0 MB" {
		t.Errorf("progress %q", got)
	}
}
