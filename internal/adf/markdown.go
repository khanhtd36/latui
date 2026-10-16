package adf

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var md = goldmark.New(goldmark.WithExtensions(extension.Strikethrough, extension.Linkify))

// FromMarkdown turns markdown typed in latui (paragraphs, headings, lists,
// code, quotes, bold, italic, strike, links) into an ADF document.
func FromMarkdown(src string) Node {
	source := []byte(strings.ReplaceAll(src, "\r\n", "\n"))
	root := md.Parser().Parse(text.NewReader(source))
	c := converter{src: source}
	doc := Node{Type: "doc", Version: 1, Content: c.blocks(root)}
	if doc.Content == nil {
		doc.Content = []Node{}
	}
	return doc
}

type converter struct {
	src []byte
}

func (c converter) blocks(parent ast.Node) []Node {
	var out []Node
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if b, ok := c.block(n); ok {
			out = append(out, b)
		}
	}
	return out
}

func (c converter) block(n ast.Node) (Node, bool) {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		in := c.inlines(n, nil)
		if len(in) == 0 {
			return Node{}, false
		}
		return Node{Type: "paragraph", Content: in}, true
	case *ast.Heading:
		return Node{Type: "heading", Attrs: map[string]any{"level": n.Level}, Content: c.inlines(n, nil)}, true
	case *ast.List:
		list := Node{Type: "bulletList"}
		if n.IsOrdered() {
			list.Type = "orderedList"
			if n.Start > 1 {
				list.Attrs = map[string]any{"order": n.Start}
			}
		}
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			content := c.blocks(item)
			if len(content) == 0 {
				content = []Node{{Type: "paragraph", Content: []Node{}}}
			}
			list.Content = append(list.Content, Node{Type: "listItem", Content: content})
		}
		return list, true
	case *ast.FencedCodeBlock:
		node := Node{Type: "codeBlock"}
		if lang := string(n.Language(c.src)); lang != "" {
			node.Attrs = map[string]any{"language": lang}
		}
		if code := c.lines(n); code != "" {
			node.Content = []Node{{Type: "text", Text: code}}
		}
		return node, true
	case *ast.CodeBlock:
		node := Node{Type: "codeBlock"}
		if code := c.lines(n); code != "" {
			node.Content = []Node{{Type: "text", Text: code}}
		}
		return node, true
	case *ast.Blockquote:
		return Node{Type: "blockquote", Content: c.blocks(n)}, true
	case *ast.ThematicBreak:
		return Node{Type: "rule"}, true
	case *ast.HTMLBlock:
		if t := strings.TrimSpace(c.lines(n)); t != "" {
			return Node{Type: "paragraph", Content: []Node{{Type: "text", Text: t}}}, true
		}
	}
	return Node{}, false
}

func (c converter) lines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		b.Write(seg.Value(c.src))
	}
	return strings.TrimRight(b.String(), "\n")
}

// inlines converts n's inline children, each text carrying marks.
func (c converter) inlines(n ast.Node, marks []Mark) []Node {
	var out []Node
	add := func(t string, ms []Mark) {
		if t == "" {
			return
		}
		// Merge with the previous text when the marks match.
		if k := len(out) - 1; k >= 0 && out[k].Type == "text" && sameMarks(out[k].Marks, ms) {
			out[k].Text += t
			return
		}
		out = append(out, Node{Type: "text", Text: t, Marks: ms})
	}
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch ch := ch.(type) {
		case *ast.Text:
			add(string(ch.Segment.Value(c.src)), marks)
			switch {
			case ch.HardLineBreak():
				out = append(out, Node{Type: "hardBreak"})
			case ch.SoftLineBreak():
				add(" ", marks)
			}
		case *ast.String:
			add(string(ch.Value), marks)
		case *ast.CodeSpan:
			var b strings.Builder
			for t := ch.FirstChild(); t != nil; t = t.NextSibling() {
				if tx, ok := t.(*ast.Text); ok {
					b.Write(tx.Segment.Value(c.src))
				}
			}
			add(b.String(), []Mark{{Type: "code"}})
		case *ast.Emphasis:
			m := Mark{Type: "em"}
			if ch.Level >= 2 {
				m.Type = "strong"
			}
			out = appendMerged(out, c.inlines(ch, withMark(marks, m)))
		case *east.Strikethrough:
			out = appendMerged(out, c.inlines(ch, withMark(marks, Mark{Type: "strike"})))
		case *ast.Link:
			link := Mark{Type: "link", Attrs: map[string]any{"href": string(ch.Destination)}}
			out = appendMerged(out, c.inlines(ch, withMark(marks, link)))
		case *ast.AutoLink:
			u := string(ch.URL(c.src))
			href := u
			if ch.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(href, "mailto:") {
				href = "mailto:" + href
			}
			add(u, withMark(marks, Mark{Type: "link", Attrs: map[string]any{"href": href}}))
		case *ast.Image:
			alt := c.plain(ch)
			add(cmpOr(alt, string(ch.Destination)), withMark(marks, Mark{Type: "link", Attrs: map[string]any{"href": string(ch.Destination)}}))
		case *ast.RawHTML:
			var b strings.Builder
			for i := range ch.Segments.Len() {
				seg := ch.Segments.At(i)
				b.Write(seg.Value(c.src))
			}
			add(b.String(), marks)
		default:
			out = appendMerged(out, c.inlines(ch, marks))
		}
	}
	return out
}

func (c converter) plain(n ast.Node) string {
	var b strings.Builder
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		if t, ok := ch.(*ast.Text); ok {
			b.Write(t.Segment.Value(c.src))
		} else {
			b.WriteString(c.plain(ch))
		}
	}
	return b.String()
}

func withMark(marks []Mark, m Mark) []Mark {
	out := make([]Mark, 0, len(marks)+1)
	out = append(out, marks...)
	return append(out, m)
}

func appendMerged(out, more []Node) []Node {
	for _, n := range more {
		if k := len(out) - 1; k >= 0 && n.Type == "text" && out[k].Type == "text" && sameMarks(out[k].Marks, n.Marks) {
			out[k].Text += n.Text
			continue
		}
		out = append(out, n)
	}
	return out
}

func sameMarks(a, b []Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || markHref(a[i]) != markHref(b[i]) {
			return false
		}
	}
	return true
}

func markHref(m Mark) string {
	if h, ok := m.Attrs["href"].(string); ok {
		return h
	}
	return ""
}
