package ui

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/khanhtd36/latui/internal/adf"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
)

// TestShots renders the README screenshots from made-up data into SHOTS_DIR,
// as HTML pages scripts/shots.sh turns into images. No real people or projects.
func TestShots(t *testing.T) {
	dir := os.Getenv("SHOTS_DIR")
	if dir == "" {
		t.Skip("set SHOTS_DIR")
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	UseSymbols(true)
	defer UseSymbols(false)
	const w, h = 142, 34
	save := func(name, view string) {
		if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(ansiHTML(view, w)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	n := func(ago time.Duration, unread bool, size int, product notify.Product, msg, title, key, status, ctx, snippet string) notify.Group {
		return notify.Group{ID: msg, Size: size, Unread: unread, Head: notify.Notification{
			Time: now.Add(-ago), Unread: unread, Product: product, Message: msg, Title: title,
			Key: key, Status: status, Context: ctx, Snippet: snippet,
		}}
	}
	feed := &fakeFeed{groups: map[notify.Category][]notify.Group{
		notify.Direct: {
			n(12*time.Minute, true, 1, notify.Jira, "Sam Rivera mentioned you on a story", "Stream large exports instead of buffering them", "DEMO-1204", "In Progress", "",
				"@Alex Kim can you check the memory numbers on the staging run before we merge?"),
			n(40*time.Minute, true, 3, notify.Confluence, "Priya Shah commented on a page", "Release checklist 4.2", "", "", "Engineering",
				"Added the rollback steps, please review the database part."),
			n(3*time.Hour, false, 1, notify.Jira, "Jo Tanaka assigned a bug to you", "Chart tooltip shows the wrong unit", "DEMO-1188", "To Do", "", ""),
			n(27*time.Hour, false, 2, notify.Jira, "Sam Rivera changed a task from Review to Done", "Explain the release process", "DEMO-1210", "Done", "", ""),
		},
		notify.Watching: {
			n(2*time.Hour, true, 1, notify.Jira, "Priya Shah updated an epic", "Faster exports", "DEMO-1100", "In Progress", "", ""),
		},
	}}
	desc := adf.FromMarkdown("Exports over 100k rows run out of memory on the **staging** server.\n\n" +
		"- Stream rows to the file as they are read\n- Keep the progress bar\n- Measure memory on the 1M row sample\n\n" +
		"See the export design page for the format.")
	descJSON, _ := json.Marshal(desc)
	is := &jira.Issue{
		Fields: jira.Fields{
			Summary: "Stream large exports instead of buffering them", Status: jira.Status{Name: "In Progress"},
			IssueType: jira.Named{Name: "Story"}, Priority: &jira.Named{Name: "High"},
			Assignee: &jira.User{DisplayName: "Alex Kim"}, Reporter: &jira.User{DisplayName: "Sam Rivera"},
			Labels: []string{"exports", "performance"}, Components: []jira.Named{{Name: "Backend"}},
			Created:     now.Add(-50 * time.Hour).Format("2006-01-02T15:04:05.000-0700"),
			Updated:     now.Add(-12 * time.Minute).Format("2006-01-02T15:04:05.000-0700"),
			Description: descJSON,
			Parent:      &jira.LinkedIssue{Key: "DEMO-1100"},
		},
		EditMeta: map[string]jira.EditField{"labels": {}, "assignee": {}, "summary": {}},
	}
	is.Fields.Status.Category.Key = "indeterminate"
	is.Fields.Project.Name = "Demo"
	j := &fakeJira{issue: is}

	m := New(j, feed, time.Minute)
	m = run(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	m = run(t, m, m.start()())
	for _, v := range m.notes.views {
		v.fetchedAt = time.Date(2026, 10, 10, 9, 41, 0, 0, time.Local)
	}
	m.me.DisplayName = "Alex Kim"
	m.site.Site = "demo.atlassian.net"
	save("1-notifications", m.View())

	m = run(t, m, openIssueMsg{key: "DEMO-1204"})
	save("2-issue", m.View())

	m = run(t, m, key("t"))
	save("3-transition", m.View())
}

// ansiHTML turns a rendered screen into HTML that looks like the terminal:
// Cascadia Mono, so box-drawing lines join as they do on screen.
func ansiHTML(view string, cols int) string {
	var b strings.Builder
	fg, bg, bold, under := "", "", false, false
	open := false
	flush := func() {
		if open {
			b.WriteString("</span>")
			open = false
		}
		var st []string
		if fg != "" {
			st = append(st, "color:"+fg)
		}
		if bg != "" {
			st = append(st, "background:"+bg)
		}
		if bold {
			st = append(st, "font-weight:bold")
		}
		if under {
			st = append(st, "text-decoration:underline")
		}
		if len(st) > 0 {
			b.WriteString(`<span style="` + strings.Join(st, ";") + `">`)
			open = true
		}
	}
	for i := 0; i < len(view); {
		if view[i] == 0x1b && i+1 < len(view) && view[i+1] == '[' {
			j := i + 2
			for j < len(view) && view[j] != 'm' {
				j++
			}
			codes := strings.Split(view[i+2:j], ";")
			for k := 0; k < len(codes); k++ {
				n, _ := strconv.Atoi(codes[k])
				switch {
				case n == 0:
					fg, bg, bold, under = "", "", false, false
				case n == 1:
					bold = true
				case n == 4:
					under = true
				case n == 22:
					bold = false
				case n == 24:
					under = false
				case n >= 30 && n <= 37:
					fg = campbell[n-30]
				case n >= 90 && n <= 97:
					fg = campbell[n-90+8]
				case n >= 40 && n <= 47:
					bg = campbell[n-40]
				case n == 39:
					fg = ""
				case n == 49:
					bg = ""
				case (n == 38 || n == 48) && k+2 < len(codes) && codes[k+1] == "5":
					c, _ := strconv.Atoi(codes[k+2])
					if n == 38 {
						fg = color256(c)
					} else {
						bg = color256(c)
					}
					k += 2
				}
			}
			flush()
			i = j + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(view[i:])
		if r > 0x7f { // one cell, whatever the fallback font says
			b.WriteString("<i>" + html.EscapeString(string(r)) + "</i>")
		} else {
			b.WriteString(html.EscapeString(string(r)))
		}
		i += size
	}
	if open {
		b.WriteString("</span>")
	}
	return `<html><head><style>body{margin:0;background:#0C0C0C}i{font-style:normal;display:inline-block;width:1ch;height:1em;vertical-align:top;overflow:visible}pre{margin:0;padding:14px 18px;color:#CCCCCC;` +
		`font:15px/1.0 'Cascadia Mono',monospace;width:` + strconv.Itoa(cols) + `ch}</style></head><body><pre>` + b.String() + `</pre></body></html>`
}

// campbell is Windows Terminal's default palette for the 16 basic colors.
var campbell = [16]string{
	"#0C0C0C", "#C50F1F", "#13A10E", "#C19C00", "#0037DA", "#881798", "#3A96DD", "#CCCCCC",
	"#767676", "#E74856", "#16C60C", "#F9F1A5", "#3B78FF", "#B4009E", "#61D6D6", "#F2F2F2",
}

func color256(n int) string {
	switch {
	case n < 16:
		return campbell[n]
	case n < 232:
		n -= 16
		level := []int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", level[n/36], level[n/6%6], level[n%6])
	}
	g := 8 + (n-232)*10
	return fmt.Sprintf("#%02x%02x%02x", g, g, g)
}

// ansiHTML turns a rendered screen into HTML that looks like the terminal:
