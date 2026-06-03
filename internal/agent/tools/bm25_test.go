package tools

import (
	"context"
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// bm25Tool is a deferred fixture whose Name, Description, and Parameters are all
// controllable so a ranking test can target each field of searchDocument.
type bm25Tool struct {
	name, desc string
	params     json.RawMessage
}

func (b bm25Tool) Spec() Spec {
	p := b.params
	if len(p) == 0 {
		p = json.RawMessage(`{"type":"object"}`)
	}
	return Spec{Name: b.name, Summary: "s", Description: b.desc, Parameters: p, Deferred: true}
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
		bm25Tool{name: "web_fetch", desc: "retrieve a url and return rendered markdown"}.Spec(),
		bm25Tool{name: "calculator", desc: "evaluate arithmetic expressions and numbers"}.Spec(),
		bm25Tool{name: "calendar", desc: "list upcoming meetings and appointments"}.Spec(),
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
		bm25Tool{name: "web_fetch", desc: "retrieve a resource"}.Spec(),
		bm25Tool{name: "calculator", desc: "arithmetic"}.Spec(),
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

// TestSearchDocument: nested param names + descriptions are recursively indexed,
// the output is byte-stable across repeated calls (sorted property keys), and a
// malformed Parameters payload degrades to Name+Description without panicking.
func TestSearchDocument(t *testing.T) {
	spec := bm25Tool{
		name: "web_fetch",
		desc: "retrieve a url",
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
	// Nested param descriptions are indexed recursively.
	for _, want := range []string{"absolute", "endpoint", "deadline", "seconds"} {
		if !containsToken(doc, want) {
			t.Errorf("searchDocument missing nested description word %q: %q", want, doc)
		}
	}
	// Determinism: two calls are byte-identical (sorted keys, Pitfall 3).
	if doc2 := searchDocument(spec); doc != doc2 {
		t.Errorf("searchDocument not deterministic:\n a=%q\n b=%q", doc, doc2)
	}

	// Malformed Parameters degrade to Name+Description, no panic.
	bad := bm25Tool{name: "broken_tool", desc: "still here", params: json.RawMessage(`{not json`)}.Spec()
	got := searchDocument(bad)
	if !containsToken(got, "broken") || !containsToken(got, "still") {
		t.Errorf("malformed schema did not degrade to Name+Description: %q", got)
	}
}

func containsToken(doc, tok string) bool {
	for _, t := range tokenize(doc) {
		if t == tok {
			return true
		}
	}
	return false
}

// TestBM25Properties (rapid): idf is never negative; a query sharing no terms
// with any doc yields no positive-scoring match; adding a matching query term
// never lowers the score of the doc that owns it.
func TestBM25Properties(t *testing.T) {
	corpus := []Spec{
		bm25Tool{name: "web_fetch", desc: "retrieve url markdown"}.Spec(),
		bm25Tool{name: "calculator", desc: "evaluate arithmetic"}.Spec(),
		bm25Tool{name: "calendar", desc: "list meetings"}.Spec(),
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
