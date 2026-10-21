package adf

import (
	"encoding/json"
	"strings"
	"testing"
)

func docJSON(t *testing.T, n Node) string {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPersonMentionBecomesANode(t *testing.T) {
	doc := FromMarkdownWith("ping @Khanh Truong, please look", []Sub{PersonMention("acc-1", "Khanh Truong")})
	j := docJSON(t, doc)
	for _, want := range []string{`"type":"mention"`, `"id":"acc-1"`, `"text":"@Khanh Truong"`, `"text":"ping "`, `"text":", please look"`} {
		if !strings.Contains(j, want) {
			t.Errorf("document lacks %s:\n%s", want, j)
		}
	}
}

func TestTeamMentionIsWrittenAsTheWebDoes(t *testing.T) {
	team := TeamMention("x.atlassian.net", "uuid-1", "MITS11 Testers", []Member{{"a", "Ann"}, {"b", "Bob"}})
	doc := FromMarkdownWith("@MITS11 Testers please test", []Sub{team})
	j := docJSON(t, doc)
	for _, want := range []string{
		`"text":"MITS11 Testers"`, `"href":"https://x.atlassian.net/jira/people/team/uuid-1?ref=jira\u0026src=issue"`,
		`"text":" ("`, `"id":"a"`, `"id":"b"`, `"text":")"`, `"text":" please test"`,
	} {
		if !strings.Contains(j, want) {
			t.Errorf("document lacks %s:\n%s", want, j)
		}
	}
	if strings.Index(j, `"id":"a"`) > strings.Index(j, `"id":"b"`) {
		t.Error("members should keep their order")
	}
}

func TestTokensInsideWordsOrCodeStayText(t *testing.T) {
	subs := []Sub{PersonMention("acc-1", "Ann")}
	for _, src := range []string{"mail@Ann.com", "see `@Ann` here", "```\n@Ann\n```", "@Annabel"} {
		j := docJSON(t, FromMarkdownWith(src, subs))
		if strings.Contains(j, `"type":"mention"`) {
			t.Errorf("%q became a mention:\n%s", src, j)
		}
		if !strings.Contains(j, "Ann") || strings.ContainsRune(j, phOpen) {
			t.Errorf("%q lost its text or kept a placeholder:\n%s", src, j)
		}
	}
}

func TestNamesWithMarkdownCharactersSurvive(t *testing.T) {
	doc := FromMarkdownWith("hi @yuhua_wang(Michael) and **@haibo_chen**", []Sub{
		PersonMention("a", "yuhua_wang(Michael)"), PersonMention("b", "haibo_chen"),
	})
	j := docJSON(t, doc)
	if strings.Count(j, `"type":"mention"`) != 2 {
		t.Errorf("want two mentions:\n%s", j)
	}
}

func TestLongestTokenWins(t *testing.T) {
	doc := FromMarkdownWith("@Khanh Truong and @Khanh", []Sub{PersonMention("short", "Khanh"), PersonMention("long", "Khanh Truong")})
	j := docJSON(t, doc)
	if !strings.Contains(j, `"id":"long"`) || !strings.Contains(j, `"id":"short"`) {
		t.Errorf("both should resolve to their own person:\n%s", j)
	}
}

func TestNoSubsIsPlainFromMarkdown(t *testing.T) {
	if docJSON(t, FromMarkdownWith("a **b**", nil)) != docJSON(t, FromMarkdown("a **b**")) {
		t.Error("no subs should change nothing")
	}
}
