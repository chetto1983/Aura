package arcadedb

import (
	"strings"
	"testing"
)

// Lucene's classic parser reads bare uppercase AND/OR/NOT as operators, so a natural
// question that happens to contain one is a syntax error rather than a search. Measured
// 2026-09-09: document_search for "SEARCH_INDEX full-text query syntax operator AND OR
// minimum should match analyzer stopwords" came back as HTTP 500 "Cannot parse ...
// Encountered \" <OR> \"OR \"\"", and the whole tool call failed hard rather than
// degrading. Escaping punctuation is not enough while the keywords stay live.
func TestEscapeLuceneDisarmsBooleanKeywords(t *testing.T) {
	for _, keyword := range []string{"AND", "OR", "NOT"} {
		got := escapeLucene("operator " + keyword + " stopwords")
		if strings.Contains(got, keyword) {
			t.Errorf("escapeLucene kept the bare operator %q: %q", keyword, got)
		}
	}
}

// Lowercasing only the standalone keyword must not touch a word that merely contains it,
// nor a lowercase term the analyzer would have handled anyway.
func TestEscapeLuceneKeepsOrdinaryTerms(t *testing.T) {
	got := escapeLucene("ANDREA nota NOTAIO or and")
	for _, want := range []string{"ANDREA", "NOTAIO", "nota", "or", "and"} {
		if !strings.Contains(got, want) {
			t.Errorf("escapeLucene dropped %q from %q", want, got)
		}
	}
}
