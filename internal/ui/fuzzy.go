package ui

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

// rankFuzzy matches query against texts, tightest matches first: "cmpl"
// should find "complete" before "new comment on the pull request", which
// fuzzy alone scores higher for hitting word starts.
func rankFuzzy(query string, texts []string) []fuzzy.Match {
	if isNumberQuery(query) {
		return findLiteral(query, texts)
	}
	span := func(m fuzzy.Match) int {
		return m.MatchedIndexes[len(m.MatchedIndexes)-1] - m.MatchedIndexes[0]
	}
	// Letters scattered across a long description are noise, not a match.
	limit := max(12, 3*len([]rune(query)))
	var matches []fuzzy.Match
	for _, m := range fuzzy.Find(query, texts) {
		if span(m) <= limit {
			matches = append(matches, m)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return span(matches[i]) < span(matches[j]) })
	return matches
}

// isNumberQuery reports a number, like "1204" or "!1204": digits scattered
// through descriptions would fuzzy-match it, so it is matched as written.
func isNumberQuery(query string) bool {
	digits := strings.TrimPrefix(query, "!")
	return digits != "" && strings.Trim(digits, "0123456789") == ""
}

func findLiteral(query string, texts []string) []fuzzy.Match {
	var matches []fuzzy.Match
	for i, t := range texts {
		at := strings.Index(t, query)
		if at < 0 {
			continue
		}
		idx := make([]int, len(query))
		for j := range idx {
			idx[j] = at + j
		}
		matches = append(matches, fuzzy.Match{Str: t, Index: i, MatchedIndexes: idx})
	}
	return matches
}
