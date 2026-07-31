package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// bm25Tool is a deferred fixture whose Name, Summary, and Parameters are all
// controllable so a ranking test can target each field of searchDocument. Its
// Description is filler that shares no token with any capability text: the
// retrieval document is built from the Summary, and these tests must fail if that
// ever silently reverts to the Description.
type bm25Tool struct {
	name, summary string
	params        json.RawMessage
}

func (b bm25Tool) Spec() Spec {
	p := b.params
	if len(p) == 0 {
		p = json.RawMessage(`{"type":"object"}`)
	}
	return Spec{
		Name:        b.name,
		Summary:     b.summary,
		Description: "usage prose",
		Parameters:  p,
		Deferred:    true,
	}
}
func (bm25Tool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// topName ranks the corpus against query and returns the #1 spec's Name (empty
// when nothing scores > 0).
func topName(specs []Spec, query string) string {
	idx := newBM25Index(specs)
	ranked := idx.rank(query)
	if len(ranked) == 0 {
		return ""
	}
	return specs[ranked[0].doc].Name
}

// TestBM25Rank: a query equal to a tool's name ranks that tool #1, and a query
// of words distinctive to one tool's Description ranks the owning tool #1.
func TestBM25Rank(t *testing.T) {
	corpus := []Spec{
		bm25Tool{name: "web_fetch", summary: "retrieve a url and return rendered markdown"}.Spec(),
		bm25Tool{name: "calculator", summary: "evaluate arithmetic expressions and numbers"}.Spec(),
		bm25Tool{name: "calendar", summary: "list upcoming meetings and appointments"}.Spec(),
	}
	cases := []struct {
		query string
		want  string
	}{
		{"web_fetch", "web_fetch"},               // exact name → that tool
		{"arithmetic expressions", "calculator"}, // description words → owning tool
		{"meetings appointments", "calendar"},    // description words → owning tool
	}
	for i, c := range cases {
		if got := topName(corpus, c.query); got != c.want {
			t.Errorf("[%d] topName(%q) = %q, want %q", i, c.query, got, c.want)
		}
	}
}

// TestUnderscoreSpacing proves Name AND ReplaceAll(Name,"_"," ") are both
// indexed: a multi-word query "fetch web" matches web_fetch and ranks it above a
// non-matching tool. Kills the missing-underscore-replace mutant (Pitfall 2).
func TestUnderscoreSpacing(t *testing.T) {
	corpus := []Spec{
		bm25Tool{name: "web_fetch", summary: "retrieve a resource"}.Spec(),
		bm25Tool{name: "calculator", summary: "arithmetic"}.Spec(),
	}
	idx := newBM25Index(corpus)
	for _, q := range []string{"fetch web", "web fetch"} {
		ranked := idx.rank(q)
		if len(ranked) == 0 {
			t.Fatalf("query %q matched nothing — underscore form not indexed", q)
		}
		if corpus[ranked[0].doc].Name != "web_fetch" {
			t.Errorf("query %q ranked %q first, want web_fetch", q, corpus[ranked[0].doc].Name)
		}
		// score must be strictly positive (the underscore tokens contributed).
		if ranked[0].score <= 0 {
			t.Errorf("query %q top score = %v, want > 0", q, ranked[0].score)
		}
	}
}

func TestSummaryBM25IgnoresCrossToolDescriptionContamination(t *testing.T) {
	corpus := []Spec{
		{
			Name:        "document_search",
			Summary:     "Search the user's uploaded and indexed documents.",
			Description: "This is not for the public web; use web_search for that.",
			Parameters:  json.RawMessage(`{"type":"object"}`),
			Deferred:    true,
		},
		{
			Name:        "web_search",
			Summary:     "Search the current public web for prices, news, and specifications.",
			Description: "Run a metasearch query.",
			Parameters:  json.RawMessage(`{"type":"object"}`),
			Deferred:    true,
		},
	}
	ranked := newBM25Index(corpus).rank(
		"web search for product specifications",
	)
	if len(ranked) == 0 {
		t.Fatal("summary index returned no match")
	}
	if got := corpus[ranked[0].doc].Name; got != "web_search" {
		t.Fatalf("summary index top result = %q, want web_search", got)
	}
}

// TestSearchDocument: nested param NAMES are recursively indexed and their PROSE
// is not, the name field is repeated so it outweighs the schema, the output is
// byte-stable across repeated calls (sorted property keys), and a malformed
// Parameters payload degrades to Name+Description without panicking.
func TestSearchDocument(t *testing.T) {
	spec := bm25Tool{
		name:    "web_fetch",
		summary: "retrieve a url",
		params: json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "absolute http endpoint"},
    "options": {
      "type": "object",
      "properties": {
        "timeout": {"type": "integer", "description": "deadline seconds"}
      }
    }
  }
}`),
	}.Spec()

	doc := searchDocument(spec)

	// Param NAMES are indexed (D-02).
	for _, want := range []string{"url", "options", "timeout"} {
		if !containsToken(doc, want) {
			t.Errorf("searchDocument missing param name %q: %q", want, doc)
		}
	}
	// Argument PROSE is NOT indexed: it is written for use time, so it names
	// neighbouring tools and modes and drags the wrong tool to rank 1.
	for _, unwanted := range []string{"absolute", "endpoint", "deadline", "seconds"} {
		if containsToken(doc, unwanted) {
			t.Errorf("searchDocument indexed argument prose %q: %q", unwanted, doc)
		}
	}
	// The name field is repeated so a tool named for a capability outranks a tool
	// that merely takes it as an argument.
	if got := strings.Count(doc, "web_fetch"); got != nameFieldWeight {
		t.Errorf("name field appears %d times, want %d: %q", got, nameFieldWeight, doc)
	}
	// Determinism: two calls are byte-identical (sorted keys, Pitfall 3).
	if doc2 := searchDocument(spec); doc != doc2 {
		t.Errorf("searchDocument not deterministic:\n a=%q\n b=%q", doc, doc2)
	}

	// Malformed Parameters degrade to Name+Description, no panic.
	bad := bm25Tool{name: "broken_tool", summary: "still here", params: json.RawMessage(`{not json`)}.Spec()
	got := searchDocument(bad)
	if !containsToken(got, "broken") || !containsToken(got, "still") {
		t.Errorf("malformed schema did not degrade to Name+Description: %q", got)
	}
}

// containsToken passes the wanted word through the SAME tokenizer as the document,
// so a caller may write "options" and still match the folded "option" the index
// stores.
func containsToken(doc, tok string) bool {
	want := tokenize(tok)
	if len(want) != 1 {
		return false
	}
	return slices.Contains(tokenize(doc), want[0])
}

// TestBM25Properties (rapid): idf is never negative; a query sharing no terms
// with any doc yields no positive-scoring match; adding a matching query term
// never lowers the score of the doc that owns it.
func TestBM25Properties(t *testing.T) {
	corpus := []Spec{
		bm25Tool{name: "web_fetch", summary: "retrieve url markdown"}.Spec(),
		bm25Tool{name: "calculator", summary: "evaluate arithmetic"}.Spec(),
		bm25Tool{name: "calendar", summary: "list meetings"}.Spec(),
	}
	idx := newBM25Index(corpus)

	// idf is never negative (Lucene +1 clamp).
	for term, v := range idx.idf {
		if v < 0 {
			t.Fatalf("idf[%q] = %v, want >= 0", term, v)
		}
	}

	rapid.Check(t, func(rt *rapid.T) {
		// A query built only from non-corpus gibberish tokens yields no match.
		gibberish := rapid.SliceOfN(rapid.StringMatching(`[xyzqkw]{4,8}`), 1, 4).Draw(rt, "gib")
		q := ""
		for _, g := range gibberish {
			q += g + " "
		}
		ranked := idx.rank(q)
		for _, r := range ranked {
			if r.score > 0 {
				rt.Fatalf("gibberish query %q scored doc %d at %v > 0", q, r.doc, r.score)
			}
		}
	})

	rapid.Check(t, func(rt *rapid.T) {
		// Monotonicity: a single owned term scores the owner ≤ that same term
		// repeated (more matching terms ⇒ score is monotonically ≥).
		base := idx.rank("arithmetic")
		more := idx.rank("arithmetic arithmetic")
		baseScore := scoreOfDoc(base, ownerDoc(corpus, "calculator"))
		moreScore := scoreOfDoc(more, ownerDoc(corpus, "calculator"))
		if moreScore < baseScore {
			rt.Fatalf("repeated matching term lowered score: %v < %v", moreScore, baseScore)
		}
	})
}

func ownerDoc(specs []Spec, name string) int {
	for i, s := range specs {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func scoreOfDoc(ranked []scoredDoc, doc int) float64 {
	for _, r := range ranked {
		if r.doc == doc {
			return r.score
		}
	}
	return 0
}
