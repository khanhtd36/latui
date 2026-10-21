package adf

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A Sub is a token typed in markdown, such as "@Khanh Truong", that becomes
// inline nodes when the text is turned into a document: a mention, or a team
// and its members.
type Sub struct {
	Token string
	Nodes []Node
}

// Member is one person of a team.
type Member struct{ ID, Name string }

// PersonMention is the Sub for mentioning a person.
func PersonMention(id, name string) Sub {
	return Sub{Token: "@" + name, Nodes: []Node{mention(id, name)}}
}

// TeamMention is the Sub for mentioning a team, written as the web does: the
// team's name linked to its page, then its members as mentions in
// parentheses. Jira notifies the members, not the team.
func TeamMention(site, teamID, name string, members []Member) Sub {
	link := Mark{Type: "link", Attrs: map[string]any{"href": "https://" + site + "/jira/people/team/" + teamID + "?ref=jira&src=issue"}}
	nodes := []Node{{Type: "text", Text: name, Marks: []Mark{link}}}
	if len(members) > 0 {
		nodes = append(nodes, Node{Type: "text", Text: " ("})
		for i, m := range members {
			if i > 0 {
				nodes = append(nodes, Node{Type: "text", Text: " "})
			}
			nodes = append(nodes, mention(m.ID, m.Name))
		}
		nodes = append(nodes, Node{Type: "text", Text: ")"})
	}
	return Sub{Token: "@" + name, Nodes: nodes}
}

func mention(id, name string) Node {
	return Node{Type: "mention", Attrs: map[string]any{"id": id, "text": "@" + name, "accessLevel": ""}}
}

// Placeholders stand for tokens while markdown is parsed, so that no markdown
// reading of a name (an underscore, a star) can split it.
const (
	phOpen  = '\uE000'
	phClose = '\uE001'
)

// FromMarkdownWith is FromMarkdown, turning each token of subs found in src
// into its nodes. A token counts where it starts a word and ends one, and
// not inside code.
func FromMarkdownWith(src string, subs []Sub) Node {
	if len(subs) == 0 {
		return FromMarkdown(src)
	}
	order := make([]int, len(subs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return len(subs[order[a]].Token) > len(subs[order[b]].Token) })
	for _, i := range order {
		src = replaceToken(src, subs[i].Token, string(phOpen)+strconv.Itoa(i)+string(phClose))
	}
	doc := FromMarkdown(src)
	doc.Content = expand(doc.Content, subs, false)
	return doc
}

// replaceToken swaps each whole-word occurrence of token in s for with.
func replaceToken(s, token, with string) string {
	if token == "" {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, token)
		if i < 0 {
			break
		}
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[i+len(token):])
		startsWord := i == 0 || !isWordRune(before) && before != '@'
		endsWord := i+len(token) == len(s) || !isWordRune(after)
		b.WriteString(s[:i])
		if startsWord && endsWord {
			b.WriteString(with)
		} else {
			b.WriteString(token)
		}
		s = s[i+len(token):]
	}
	b.WriteString(s)
	return b.String()
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// expand puts each placeholder in nodes back as its token's nodes, or as the
// token's text where it sits in code.
func expand(nodes []Node, subs []Sub, inCode bool) []Node {
	var out []Node
	for _, n := range nodes {
		if n.Type == "text" {
			out = append(out, expandText(n, subs, inCode || hasMark(n, "code"))...)
			continue
		}
		n.Content = expand(n.Content, subs, inCode || n.Type == "codeBlock")
		out = append(out, n)
	}
	return out
}

func hasMark(n Node, typ string) bool {
	for _, m := range n.Marks {
		if m.Type == typ {
			return true
		}
	}
	return false
}

func expandText(n Node, subs []Sub, code bool) []Node {
	if !strings.ContainsRune(n.Text, phOpen) {
		return []Node{n}
	}
	var out []Node
	text := func(s string) {
		if s != "" {
			out = append(out, Node{Type: "text", Text: s, Marks: n.Marks})
		}
	}
	rest := n.Text
	for {
		i := strings.IndexRune(rest, phOpen)
		if i < 0 {
			break
		}
		j := strings.IndexRune(rest[i:], phClose)
		if j < 0 {
			break
		}
		k, err := strconv.Atoi(rest[i+utf8.RuneLen(phOpen) : i+j])
		if err != nil || k < 0 || k >= len(subs) {
			text(rest[:i+j+utf8.RuneLen(phClose)])
			rest = rest[i+j+utf8.RuneLen(phClose):]
			continue
		}
		text(rest[:i])
		if code {
			text(subs[k].Token)
		} else {
			out = append(out, subs[k].Nodes...)
		}
		rest = rest[i+j+utf8.RuneLen(phClose):]
	}
	text(rest)
	return out
}
