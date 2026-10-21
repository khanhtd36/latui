package attach

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"report.pdf":             "report.pdf",
		`..\..\Windows\evil.dll`: "evil.dll",
		"../../etc/passwd":       "passwd",
		"a:b*c?.txt":             "a_b_c_.txt",
		"name. ":                 "name",
		"":                       "attachment",
		"..":                     "attachment",
		"con.txt":                "_con.txt",
		"NUL":                    "_NUL",
		"tab\tname.log":          "tab_name.log",
		"日本語.txt":                "日本語.txt",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRisky(t *testing.T) {
	for name, want := range map[string]bool{
		"setup.exe": true, "run.BAT": true, "x.ps1": true, "doc.docm": true, "link.lnk": true,
		"photo.png": false, "notes.txt": false, "report.pdf": false, "archive.zip": false,
	} {
		if Risky(name) != want {
			t.Errorf("Risky(%q) = %v", name, !want)
		}
	}
}

func TestTextLike(t *testing.T) {
	for _, c := range []struct {
		name, mime string
		want       bool
	}{
		{"a.bin", "text/plain; charset=utf-8", true},
		{"a.bin", "application/json", true},
		{"a.bin", "application/vnd.api+json", true},
		{"build.log", "application/octet-stream", true},
		{"main.go", "", true},
		{"photo.png", "image/png", false},
		{"report.pdf", "application/pdf", false},
	} {
		if TextLike(c.name, c.mime) != c.want {
			t.Errorf("TextLike(%q, %q) = %v", c.name, c.mime, !c.want)
		}
	}
}

func TestDecode(t *testing.T) {
	u16 := func(s string, bigEndian bool) []byte {
		out := []byte{0xff, 0xfe}
		if bigEndian {
			out = []byte{0xfe, 0xff}
		}
		for _, c := range utf16.Encode([]rune(s)) {
			if bigEndian {
				out = append(out, byte(c>>8), byte(c))
			} else {
				out = append(out, byte(c), byte(c>>8))
			}
		}
		return out
	}
	for name, c := range map[string]struct {
		in   []byte
		want string
		ok   bool
	}{
		"utf8":        {[]byte("héllo\r\nworld"), "héllo\nworld", true},
		"utf8 bom":    {append([]byte{0xef, 0xbb, 0xbf}, "bom"...), "bom", true},
		"utf16 le":    {u16("日本 text\r\n", false), "日本 text\n", true},
		"utf16 be":    {u16("big", true), "big", true},
		"latin1":      {[]byte{'c', 'a', 'f', 0xe9}, "", false},
		"binary":      {[]byte{0x89, 'P', 'N', 'G', 0, 0, 0}, "", false},
		"nul in utf8": {[]byte("ab\x00cd"), "", false},
		"odd utf16":   {[]byte{0xff, 0xfe, 'a'}, "", false},
		"empty":       {nil, "", true},
	} {
		got, ok := Decode(c.in)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s: got %q, %v", name, got, ok)
		}
	}
}

func opener(body string) Opener {
	return func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader(body)), int64(len(body)), nil
	}
}

func TestSaveWritesReusesAndNumbers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "KEY-1")
	var last int64
	s, err := Save(context.Background(), opener("hello"), dir, "a.txt", 5, func(done, _ int64) { last = done })
	if err != nil || s.Reused || filepath.Base(s.Path) != "a.txt" || last != 5 {
		t.Fatalf("first save: %+v, %v, progress %d", s, err, last)
	}
	b, _ := os.ReadFile(s.Path)
	if string(b) != "hello" {
		t.Errorf("file holds %q", b)
	}
	// The same name and size is reused without a download.
	again, err := Save(context.Background(), func(context.Context) (io.ReadCloser, int64, error) {
		t.Error("downloaded again")
		return nil, 0, errors.New("no")
	}, dir, "a.txt", 5, nil)
	if err != nil || !again.Reused || again.Path != s.Path {
		t.Errorf("second save: %+v, %v", again, err)
	}
	// Another file of the same name gets a number.
	other, err := Save(context.Background(), opener("different!"), dir, "a.txt", 10, nil)
	if err != nil || filepath.Base(other.Path) != "a (1).txt" {
		t.Errorf("third save: %+v, %v", other, err)
	}
	third, _ := Save(context.Background(), opener("0123456789ab"), dir, "a.txt", 12, nil)
	if filepath.Base(third.Path) != "a (2).txt" {
		t.Errorf("fourth save: %+v", third)
	}
}

func TestSaveStaysInsideTheFolderAndLeavesNoPartFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "KEY-1")
	s, err := Save(context.Background(), opener("x"), dir, `..\..\escape.txt`, 1, nil)
	if err != nil || filepath.Dir(s.Path) != dir {
		t.Fatalf("saved outside the folder: %+v, %v", s, err)
	}
	failing := func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(&brokenReader{}), 100, nil
	}
	if _, err := Save(context.Background(), failing, dir, "b.bin", 100, nil); err == nil {
		t.Fatal("a broken download succeeded")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") || e.Name() == "b.bin" {
			t.Errorf("a failed save left %s", e.Name())
		}
	}
}

type brokenReader struct{ n int }

func (b *brokenReader) Read(p []byte) (int, error) {
	if b.n > 0 {
		return 0, errors.New("connection reset")
	}
	b.n++
	return copy(p, "partial"), nil
}

func TestFolderIsNamedByTheIssueKey(t *testing.T) {
	defer SetDownloads(func() (string, error) { return "/dl", nil })()
	if got := filepath.ToSlash(Folder("MITS11-3323")); got != "/dl/latui/MITS11-3323" {
		t.Errorf("folder %q", got)
	}
	if got := filepath.Base(Folder("../../x")); got != "x" {
		t.Errorf("a key with dots escaped: %q", got)
	}
}

func TestReadTextRefusesBigAndBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if text, ok, _ := ReadText(write("a.txt", []byte("hi"))); !ok || text != "hi" {
		t.Errorf("small text: %q %v", text, ok)
	}
	if _, ok, _ := ReadText(write("big.txt", make([]byte, MaxView+1))); ok {
		t.Error("a file over the limit was read")
	}
	if _, ok, _ := ReadText(write("bin", []byte{0, 1, 2})); ok {
		t.Error("binary was read as text")
	}
}
