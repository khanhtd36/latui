// Package adf converts Atlassian Document Format, the JSON rich text of
// Jira descriptions, comments and notification snippets, to and from
// markdown (see ADR 0001).
package adf

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Node is one ADF node: a document, block, inline or text.
type Node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []Node         `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
	Version int            `json:"version,omitempty"`
}

type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Parse reads ADF JSON; JSON null or empty input is an empty document.
// Data can also be a JSON string holding ADF, as some APIs send it.
func Parse(data []byte) (Node, error) {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		return Node{Type: "doc"}, nil
	}
	if strings.HasPrefix(s, `"`) {
		var inner string
		if err := json.Unmarshal(data, &inner); err != nil {
			return Node{}, err
		}
		if !strings.HasPrefix(strings.TrimSpace(inner), "{") {
			return Node{Type: "doc", Content: []Node{{Type: "paragraph", Content: []Node{{Type: "text", Text: inner}}}}}, nil
		}
		data = []byte(inner)
	}
	var n Node
	err := json.Unmarshal(data, &n)
	return n, err
}

func (n Node) attr(key string) string {
	switch v := n.Attrs[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// ToMarkdown renders a document as markdown for display. Nodes markdown
// has no form for (panels, media, macros) come out as their nearest
// equivalent or a placeholder; the result is not meant to be written back.
func ToMarkdown(doc Node) string {
	var w mdWriter
	w.blocks(doc.Content, "")
	return strings.TrimRight(w.b.String(), "\n") + "\n"
}

// PlainText flattens a document to one line of text, for list snippets.
func PlainText(doc Node) string {
	var b strings.Builder
	var walk func(n Node)
	walk = func(n Node) {
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
			return
		case "hardBreak":
			b.WriteString(" ")
			return
		}
		if s, ok := inlineAtom(n); ok {
			b.WriteString(s)
			return
		}
		for _, c := range n.Content {
			walk(c)
		}
		if isBlock(n.Type) {
			b.WriteString(" ")
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(b.String()), " ")
}

func isBlock(t string) bool {
	switch t {
	case "paragraph", "heading", "listItem", "codeBlock", "blockquote", "tableCell", "tableHeader",
		"panel", "taskItem", "decisionItem", "expand", "nestedExpand", "rule", "mediaSingle":
		return true
	}
	return false
}

type mdWriter struct {
	b strings.Builder
}

// blocks writes block nodes, each line led by prefix (a quote's "> ").
func (w *mdWriter) blocks(nodes []Node, prefix string) {
	for i, n := range nodes {
		if i > 0 {
			w.b.WriteString(strings.TrimRight(prefix, " ") + "\n")
		}
		w.block(n, prefix)
	}
}

func (w *mdWriter) line(prefix, s string) {
	for l := range strings.SplitSeq(s, "\n") {
		w.b.WriteString(prefix + l + "\n")
	}
}

func (w *mdWriter) block(n Node, prefix string) {
	switch n.Type {
	case "paragraph":
		w.line(prefix, inlines(n.Content))
	case "heading":
		level, _ := strconv.Atoi(n.attr("level"))
		w.line(prefix, strings.Repeat("#", max(1, min(6, level)))+" "+inlines(n.Content))
	case "bulletList", "orderedList", "taskList", "decisionList":
		w.list(n, prefix)
	case "codeBlock":
		w.line(prefix, "```"+n.attr("language"))
		var code strings.Builder
		for _, c := range n.Content {
			code.WriteString(c.Text)
		}
		w.line(prefix, strings.TrimRight(code.String(), "\n"))
		w.line(prefix, "```")
	case "blockquote":
		w.blocks(n.Content, prefix+"> ")
	case "panel":
		label := strings.ToUpper(cmpOr(n.attr("panelType"), "info"))
		w.line(prefix+"> ", "**"+label+"**")
		w.b.WriteString(prefix + ">\n")
		w.blocks(n.Content, prefix+"> ")
	case "rule":
		w.line(prefix, "---")
	case "table":
		w.table(n, prefix)
	case "expand", "nestedExpand":
		w.line(prefix, "**▸ "+cmpOr(n.attr("title"), "Details")+"**")
		w.b.WriteString(strings.TrimRight(prefix, " ") + "\n")
		w.blocks(n.Content, prefix)
	case "mediaSingle", "mediaGroup":
		names := make([]string, 0, len(n.Content))
		for _, m := range n.Content {
			names = append(names, mediaLabel(m))
		}
		w.line(prefix, strings.Join(names, " "))
	case "media":
		w.line(prefix, mediaLabel(n))
	case "blockCard", "embedCard":
		w.line(prefix, n.attr("url"))
	case "extension", "bodiedExtension", "multiBodiedExtension":
		w.line(prefix, "_["+cmpOr(n.attr("extensionKey"), "macro")+"]_")
	default:
		if len(n.Content) > 0 {
			if isInlineContent(n.Content) {
				w.line(prefix, inlines(n.Content))
			} else {
				w.blocks(n.Content, prefix)
			}
		}
	}
}

func isInlineContent(nodes []Node) bool {
	for _, n := range nodes {
		if n.Type == "text" {
			return true
		}
		if _, ok := inlineAtom(n); ok {
			return true
		}
	}
	return false
}

func mediaLabel(n Node) string {
	name := cmpOr(n.attr("alt"), n.attr("__fileName"))
	if name == "" {
		return "[attachment]"
	}
	return "[attachment: " + name + "]"
}

func (w *mdWriter) list(n Node, prefix string) {
	for i, item := range n.Content {
		marker := "- "
		switch {
		case n.Type == "orderedList":
			start, err := strconv.Atoi(n.attr("order"))
			if err != nil || start < 1 {
				start = 1
			}
			marker = strconv.Itoa(start+i) + ". "
		case item.Type == "taskItem":
			marker = "- [ ] "
			if item.attr("state") == "DONE" {
				marker = "- [x] "
			}
		}
		indent := strings.Repeat(" ", len(marker))
		if item.Type == "taskItem" || item.Type == "decisionItem" {
			// Their content is inline, not paragraphs.
			w.line(prefix+marker, inlines(item.Content))
			continue
		}
		var sub mdWriter
		sub.blocks(item.Content, "")
		lines := strings.Split(strings.TrimRight(sub.b.String(), "\n"), "\n")
		for j, l := range lines {
			lead := indent
			if j == 0 {
				lead = marker
			}
			if l == "" {
				w.b.WriteString(strings.TrimRight(prefix, " ") + "\n")
				continue
			}
			w.b.WriteString(prefix + lead + l + "\n")
		}
	}
}

func (w *mdWriter) table(n Node, prefix string) {
	for i, row := range n.Content {
		cells := make([]string, 0, len(row.Content))
		for _, c := range row.Content {
			cells = append(cells, strings.ReplaceAll(PlainText(c), "|", `\|`))
		}
		w.line(prefix, "| "+strings.Join(cells, " | ")+" |")
		if i == 0 {
			seps := make([]string, len(cells))
			for j := range seps {
				seps[j] = "---"
			}
			w.line(prefix, "| "+strings.Join(seps, " | ")+" |")
		}
	}
}

// inlines renders inline nodes as one markdown string (hard breaks keep
// their newline).
func inlines(nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.Type == "text" {
			b.WriteString(marked(n.Text, n.Marks))
			continue
		}
		if n.Type == "hardBreak" {
			b.WriteString("  \n")
			continue
		}
		if s, ok := inlineAtom(n); ok {
			b.WriteString(s)
			continue
		}
		b.WriteString(inlines(n.Content))
	}
	return b.String()
}

// inlineAtom is the text of an inline node that isn't text.
func inlineAtom(n Node) (string, bool) {
	switch n.Type {
	case "mention":
		t := n.attr("text")
		if !strings.HasPrefix(t, "@") {
			t = "@" + t
		}
		return t, true
	case "emoji":
		return cmpOr(n.attr("text"), n.attr("shortName")), true
	case "inlineCard":
		return n.attr("url"), true
	case "status":
		return "[" + strings.ToUpper(n.attr("text")) + "]", true
	case "date":
		ms, err := strconv.ParseInt(n.attr("timestamp"), 10, 64)
		if err != nil {
			return n.attr("timestamp"), true
		}
		return time.UnixMilli(ms).UTC().Format("2006-01-02"), true
	case "placeholder":
		return n.attr("text"), true
	}
	return "", false
}

func marked(text string, marks []Mark) string {
	if text == "" {
		return ""
	}
	var code, link string
	strong, em, strike := false, false, false
	for _, m := range marks {
		switch m.Type {
		case "code":
			code = text
		case "strong":
			strong = true
		case "em":
			em = true
		case "strike":
			strike = true
		case "link":
			if href, ok := m.Attrs["href"].(string); ok {
				link = href
			}
		}
	}
	s := text
	if code != "" {
		s = "`" + s + "`"
	} else {
		// Keep surrounding spaces outside the markers, or markdown ignores them.
		lead := text[:len(text)-len(strings.TrimLeft(text, " "))]
		trail := text[len(strings.TrimRight(text, " ")):]
		core := strings.TrimSpace(text)
		if core == "" {
			return text
		}
		if strike {
			core = "~~" + core + "~~"
		}
		if em {
			core = "_" + core + "_"
		}
		if strong {
			core = "**" + core + "**"
		}
		s = lead + core + trail
	}
	if link != "" && link != text {
		s = "[" + s + "](" + link + ")"
	}
	return s
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
