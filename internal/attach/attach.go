// Package attach saves an issue's attachments where the user can find them,
// and decides what may be shown or opened: the Downloads folder's latui
// folder, then one folder named by the issue key.
package attach

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// MaxView is the largest text shown in the viewer; bigger files are saved and
// opened in the system app instead.
const MaxView = 2 << 20

// downloads is where the Downloads folder is; a test replaces it.
var downloads = downloadsDir

// Root is the latui folder inside Downloads, or inside the temp folder when
// Downloads can't be found.
func Root() string {
	dir, err := downloads()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "latui")
}

// Folder is where an issue's attachments are saved.
func Folder(issueKey string) string {
	return filepath.Join(Root(), SafeName(issueKey))
}

// reserved are the names Windows will not let a file have.
var reserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SafeName is name as a file name that can only land inside a folder: just
// its last part, without characters Windows rejects, control characters, or
// the dots and spaces it trims from the end.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune("<>:\"|?*", r) || unicode.Is(unicode.Cf, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	name = strings.TrimLeft(name, " ")
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if reserved[stem] {
		name = "_" + name
	}
	return name
}

// risky are the extensions of files the system would run or load as code if
// asked to open them: they are saved, never opened.
var risky = map[string]bool{
	".exe": true, ".com": true, ".scr": true, ".pif": true, ".msi": true, ".msp": true, ".bat": true, ".cmd": true,
	".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true,
	".lnk": true, ".url": true, ".jar": true, ".hta": true, ".cpl": true, ".reg": true, ".dll": true, ".sh": true,
	".command": true, ".app": true, ".apk": true, ".appimage": true, ".deb": true, ".rpm": true, ".dmg": true,
	".iso": true, ".gadget": true, ".msc": true, ".docm": true, ".xlsm": true, ".pptm": true, ".py": true, ".pl": true,
}

// Risky says whether opening name in the system app could run code.
func Risky(name string) bool { return risky[strings.ToLower(filepath.Ext(name))] }

// textExt are extensions read as text even when Jira calls them something
// else; showing text in the viewer runs nothing.
var textExt = map[string]bool{
	".txt": true, ".log": true, ".md": true, ".markdown": true, ".json": true, ".jsonl": true, ".csv": true, ".tsv": true,
	".yaml": true, ".yml": true, ".xml": true, ".ini": true, ".cfg": true, ".conf": true, ".toml": true, ".sql": true,
	".html": true, ".htm": true, ".css": true, ".diff": true, ".patch": true, ".properties": true, ".env": true,
	".go": true, ".rs": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cs": true, ".java": true,
	".ts": true, ".tsx": true, ".jsx": true, ".py": true, ".sh": true, ".ps1": true, ".bat": true, ".cmd": true,
	".js": true, ".rb": true, ".php": true, ".kt": true, ".swift": true, ".lua": true, ".pl": true, ".gradle": true,
}

// TextLike says whether an attachment is probably text, from its MIME type
// or, failing that, its extension.
func TextLike(name, mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	switch {
	case strings.HasPrefix(mime, "text/"):
		return true
	case strings.HasSuffix(mime, "+json"), strings.HasSuffix(mime, "+xml"):
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/x-yaml", "application/yaml", "application/sql",
		"application/x-sh", "application/javascript", "application/x-ndjson", "application/toml":
		return true
	}
	return textExt[strings.ToLower(filepath.Ext(name))]
}

// Decode reads text in UTF-8 or UTF-16 (told by its byte order mark); ok is
// false for another encoding or for binary bytes.
func Decode(b []byte) (string, bool) {
	switch {
	case len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe:
		return utf16Text(b[2:], false)
	case len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff:
		return utf16Text(b[2:], true)
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(b) {
		return "", false
	}
	return clean(string(b))
}

func utf16Text(b []byte, bigEndian bool) (string, bool) {
	if len(b)%2 != 0 {
		return "", false
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return clean(string(utf16.Decode(u)))
}

// clean refuses text with control characters other than the usual white
// space: a binary file that happens to be valid UTF-8.
func clean(s string) (string, bool) {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' && r != '\f' || r == 0x7f {
			return "", false
		}
	}
	return strings.ReplaceAll(s, "\r\n", "\n"), true
}

// Opener starts the download of an attachment, giving its body and size.
type Opener func(ctx context.Context) (body io.ReadCloser, size int64, err error)

// Saved is the outcome of a save.
type Saved struct {
	Path   string
	Reused bool // an identical copy was already there
}

// Save puts an attachment in dir under name. A file already there with the
// same name and size is reused; otherwise the new one gets a "(1)" if the
// name is taken. progress gets the bytes written and the size.
func Save(ctx context.Context, open Opener, dir, name string, size int64, progress func(done, total int64)) (Saved, error) {
	name = SafeName(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Saved{}, err
	}
	if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() && st.Size() == size && size > 0 {
		return Saved{Path: filepath.Join(dir, name), Reused: true}, nil
	}
	body, total, err := open(ctx)
	if err != nil {
		return Saved{}, err
	}
	defer body.Close()
	if total <= 0 {
		total = size
	}
	tmp, err := os.CreateTemp(dir, ".latui-*.part")
	if err != nil {
		return Saved{}, err
	}
	defer os.Remove(tmp.Name())
	w := &countWriter{w: tmp, total: total, progress: progress}
	if _, err := io.Copy(w, &ctxReader{ctx: ctx, r: body}); err != nil {
		tmp.Close()
		return Saved{}, err
	}
	if err := tmp.Close(); err != nil {
		return Saved{}, err
	}
	path, err := claim(dir, name, tmp.Name())
	if err != nil {
		return Saved{}, err
	}
	return Saved{Path: path}, nil
}

// claim moves the finished file to the first free name: name, then "name
// (1).ext", "name (2).ext" and so on.
func claim(dir, name, from string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		path := filepath.Join(dir, candidate)
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		if err := os.Rename(from, path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", errors.New("no free file name")
}

type countWriter struct {
	w        io.Writer
	done     int64
	total    int64
	progress func(done, total int64)
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.done += int64(n)
	if c.progress != nil {
		c.progress(c.done, c.total)
	}
	return n, err
}

// ctxReader stops a copy when its context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ReadText reads a saved file for the viewer: its text, or ok false when it
// is too big or not text in a known encoding.
func ReadText(path string) (text string, ok bool, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if st.Size() > MaxView {
		return "", false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	text, ok = Decode(b)
	return text, ok, nil
}

// SetDownloads points the Downloads folder somewhere else, for tests; it
// returns what restores it.
func SetDownloads(f func() (string, error)) (restore func()) {
	was := downloads
	downloads = f
	return func() { downloads = was }
}
