package bm25

import (
	"math"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in   string
		want []string
	}{
		"lowercases and splits on punctuation": {"Ragione Sociale, Località!", []string{"ragione", "sociale", "localit"}},
		"digits survive":                       {"F038 597691", []string{"f038", "597691"}},
		"stop words drop":                      {"the customer and the town", []string{"customer", "town"}},
		"plurals fold to the singular":         {"venditori clienti", []string{FoldPlural("venditori"), FoldPlural("clienti")}},
		"empty":                                {"   ", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Tokenize(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("Tokenize(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Tokenize(%q) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

// Non-ASCII is dropped by the tokenizer, which is a real limit and not an
// accident: `Località` becomes `localit`. It is consistent between corpus and
// query — both go through Tokenize — so a search for the accented word still
// matches the accented document. It would NOT match a document that spelled it
// without the accent, which is why the document library folds accents in SQL
// instead of relying on this.
func TestTokenizeIsConsistentOnAccents(t *testing.T) {
	t.Parallel()
	index := New([]string{"cliente in Località Torino"})
	if hits := index.Rank("Località"); len(hits) != 1 {
		t.Fatalf("an accented query did not match the accented document: %v", hits)
	}
	if hits := index.Rank("Localita"); len(hits) != 0 {
		t.Fatalf("unaccented %q matched — the tokenizer is no longer dropping non-ASCII, "+
			"which would be an improvement worth updating this test for", "Localita")
	}
}

func TestFoldPlural(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"clients":  "client",
		"boxes":    "box",
		"is":       "is", // too short to fold: stripping would leave one letter
		"venditor": "venditor",
	} {
		if got := FoldPlural(in); got != want {
			t.Errorf("FoldPlural(%q) = %q, want %q", in, got, want)
		}
	}
}

// The ranking property that matters: a term that appears in EVERY document
// carries no information and must not decide the order. That is IDF, and it is
// what the term-count scoring this package replaced got wrong.
func TestRankIgnoresATermEveryDocumentHas(t *testing.T) {
	t.Parallel()
	index := New([]string{
		"customer record torino",
		"customer record genova",
		"customer record asti torino torino",
	})
	hits := index.Rank("customer torino")
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Doc != 2 {
		t.Fatalf("top hit = doc %d, want 2 (the only one where the rare term repeats)", hits[0].Doc)
	}
	// Doc 1 lacks the only informative term, so it must rank LAST — not score
	// zero. A term present in every document still carries a small positive IDF
	// (log((N-n+0.5)/(n+0.5)+1) = log(1.143) here), which is the correct shape:
	// near-worthless, not forbidden.
	if hits[len(hits)-1].Doc != 1 {
		t.Errorf("doc 1 ranked %d of %d; it matches only the term all three share",
			len(hits), len(hits))
	}
	if common := hits[len(hits)-1].Score; common > hits[0].Score/2 {
		t.Errorf("the all-documents term scored %.4f against a top of %.4f — IDF is not damping it",
			common, hits[0].Score)
	}
}

func TestRankOrdersByScoreDescending(t *testing.T) {
	t.Parallel()
	index := New([]string{"alpha", "alpha alpha beta", "gamma"})
	hits := index.Rank("alpha")
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want exactly the two documents containing the term", hits)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i-1].Score < hits[i].Score {
			t.Fatalf("hits are not sorted by score: %v", hits)
		}
	}
	// NOT "the one with more occurrences wins": with k1=1.5 and b=0.75 the
	// one-word document scores 1.22 against 1.14 for the three-word one that
	// contains the term twice. Length normalisation outweighing a small term
	// frequency gain is BM25 behaving correctly, and asserting the opposite is
	// how this test failed the first time it ran.
	if hits[0].Doc != 0 {
		t.Fatalf("top hit = doc %d, want 0 (shorter document, same informative term)", hits[0].Doc)
	}
}

func TestRankOnEmptyInputs(t *testing.T) {
	t.Parallel()
	if hits := New(nil).Rank("anything"); len(hits) != 0 {
		t.Errorf("an empty index returned %v", hits)
	}
	index := New([]string{"a document"})
	if hits := index.Rank("   "); len(hits) != 0 {
		t.Errorf("a blank query returned %v", hits)
	}
	if hits := index.Rank("the and of"); len(hits) != 0 {
		t.Errorf("a query of nothing but stop words returned %v", hits)
	}
}

func TestScoreAndScoreTerms(t *testing.T) {
	t.Parallel()
	index := New([]string{"torino cliente", "genova cliente"})

	direct := index.Score(0, "torino")
	terms := index.ScoreTerms(0, Tokenize("torino"))
	if math.Abs(direct-terms) > 1e-9 {
		t.Fatalf("Score = %.6f but ScoreTerms = %.6f on the same query", direct, terms)
	}
	if direct <= 0 {
		t.Fatalf("Score(0, %q) = %.6f, want a positive score", "torino", direct)
	}
	if absent := index.Score(0, "genova"); absent != 0 {
		t.Fatalf("Score for a term the document lacks = %.6f, want 0", absent)
	}
	// An out-of-range document is a caller bug, and must not panic.
	if got := index.Score(99, "torino"); got != 0 {
		t.Fatalf("Score on an out-of-range doc = %.6f, want 0", got)
	}
	if got := index.Score(-1, "torino"); got != 0 {
		t.Fatalf("Score on a negative doc = %.6f, want 0", got)
	}
}

// Length normalisation: the same single match is worth more in a short document
// than in a long one, which is the B parameter doing its job.
func TestShorterDocumentsWinOnEqualMatches(t *testing.T) {
	t.Parallel()
	index := New([]string{
		"torino",
		"torino " + strings.Repeat("filler ", 200),
	})
	short, long := index.Score(0, "torino"), index.Score(1, "torino")
	if short <= long {
		t.Fatalf("short doc scored %.6f, long doc %.6f — length normalisation is not applied", short, long)
	}
}
