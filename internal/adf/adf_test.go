package adf

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) Node {
	t.Helper()
	n, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestToMarkdown(t *testing.T) {
	doc := mustParse(t, `{"type":"doc","version":1,"content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps"}]},
		{"type":"paragraph","content":[
			{"type":"mention","attrs":{"id":"x","text":"@Alex Kim"}},
			{"type":"text","text":" sure, see "},
			{"type":"text","text":"the doc","marks":[{"type":"link","attrs":{"href":"https://x.test"}}]},
			{"type":"text","text":" and "},
			{"type":"text","text":"bold ","marks":[{"type":"strong"}]},
			{"type":"text","text":"x()","marks":[{"type":"code"}]}]},
		{"type":"bulletList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
				{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]},
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},
		{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]},
		{"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"careful"}]}]},
		{"type":"mediaSingle","content":[{"type":"media","attrs":{"alt":"shot.png"}}]}
	]}`)
	got := ToMarkdown(doc)
	want := "## Steps\n\n" +
		"@Alex Kim sure, see [the doc](https://x.test) and **bold** `x()`\n\n" +
		"- one\n\n  1. nested\n- two\n\n" +
		"```go\nx := 1\n```\n\n" +
		"> **WARNING**\n>\n> careful\n\n" +
		"[attachment: shot.png]\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPlainText(t *testing.T) {
	doc := mustParse(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Memory stays"},{"type":"hardBreak"},{"type":"text","text":"flat"}]},{"type":"paragraph","content":[{"type":"status","attrs":{"text":"done"}}]}]}`)
	if got := PlainText(doc); got != "Memory stays flat [DONE]" {
		t.Errorf("got %q", got)
	}
}

func TestParseStringWrapped(t *testing.T) {
	doc := mustParse(t, `"{\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"hi\"}]}]}"`)
	if PlainText(doc) != "hi" {
		t.Errorf("got %q", PlainText(doc))
	}
}

func TestFromMarkdown(t *testing.T) {
	doc := FromMarkdown("Hello **world** and _you_, see [x](https://x.test).\nnext line\n\n- a\n- b `c`\n\n```sh\necho hi\n```\n")
	b, _ := json.Marshal(doc)
	got := string(b)
	for _, want := range []string{
		`{"type":"text","text":"world","marks":[{"type":"strong"}]}`,
		`{"type":"text","text":"you","marks":[{"type":"em"}]}`,
		`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://x.test"}}]}`,
		`"text":". next line"`,
		`{"type":"bulletList","content":[{"type":"listItem"`,
		`{"type":"text","text":"c","marks":[{"type":"code"}]}`,
		`{"type":"codeBlock","attrs":{"language":"sh"},"content":[{"type":"text","text":"echo hi"}]}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if doc.Type != "doc" || doc.Version != 1 {
		t.Errorf("bad doc header %+v", doc)
	}
}

func TestRoundTripKeepsText(t *testing.T) {
	src := "# Title\n\nSome **bold** text with `code` and a [link](https://a.test).\n\n1. first\n2. second\n\n> quoted\n"
	if got := ToMarkdown(FromMarkdown(src)); got != src {
		t.Errorf("round trip:\n%s\nwant:\n%s", got, src)
	}
}
