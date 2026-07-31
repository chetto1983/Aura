// Package bm25 is Aura's in-process Okapi BM25 ranker over an immutable corpus.
//
// It was written for deferred-tool search (internal/agent/tools), where it
// replaced a 330 ms embedding round trip with a 17 µs scan and took top-1 from
// 50% to 96%. The scoring is unchanged by the move here; what changed is that a
// second caller needed it -- ranking retrieved passages -- and the alternative to
// extracting it was a second copy of the same arithmetic.
//
// Stdlib only, no index structure: the callers' corpora are small enough that an
// O(docs·terms) scan is faster than maintaining a postings list, and the absence
// of a build step is what makes it usable on a per-query candidate pool.
package bm25

import (
	"math"
	"sort"
	"strings"
)

// K1 and B are Okapi's saturation and length-normalisation constants. K1 is 1.5
// rather than the more common 1.2 because that is what the tool-search corpus was
// tuned and measured against.
const (
	K1 = 1.5
	B  = 0.75
)

// stopWords are English function words, dropped from corpus and query alike.
//
// They are removed rather than down-weighted because BM25's own idf gets them
// exactly backwards on a small corpus: a function word appearing in two documents
// out of fifty earns a HIGH idf for carrying no meaning at all. Measured on tool
// search: "what time is it" ranked web_search first, on "is" and "it", with
// current_time fourth.
var stopWords = map[string]struct{}{
	"a": {}, "about": {}, "above": {}, "after": {}, "again": {}, "against": {},
	"all": {}, "also": {}, "an": {}, "and": {}, "any": {}, "are": {}, "as": {},
	"at": {}, "be": {}, "been": {}, "before": {}, "below": {}, "between": {},
	"both": {}, "but": {}, "by": {}, "can": {}, "do": {}, "does": {},
	"during": {}, "each": {}, "few": {}, "for": {}, "from": {}, "further": {},
	"had": {}, "has": {}, "have": {}, "he": {}, "her": {}, "here": {}, "his": {},
	"how": {}, "i": {}, "if": {}, "in": {}, "into": {}, "is": {}, "it": {},
	"its": {}, "just": {}, "me": {}, "more": {}, "most": {}, "my": {}, "no": {},
	"not": {}, "of": {}, "on": {}, "once": {}, "one": {}, "only": {}, "or": {},
	"other": {}, "our": {}, "out": {}, "over": {}, "own": {}, "please": {},
	"same": {}, "she": {}, "should": {}, "so": {}, "some": {}, "such": {},
	"than": {}, "that": {}, "the": {}, "their": {}, "them": {}, "then": {},
	"there": {}, "these": {}, "they": {}, "this": {}, "those": {}, "through": {},
	"to": {}, "too": {}, "under": {}, "until": {}, "up": {}, "us": {},
	"very": {}, "was": {}, "we": {}, "were": {}, "what": {}, "when": {},
	"where": {}, "which": {}, "while": {}, "who": {}, "why": {}, "will": {},
	"with": {}, "would": {}, "you": {}, "your": {},
}

// FoldPlural strips the regular English plural so a query written "files"
// reaches text written "file" -- measured: the two returned disjoint result sets.
// It stays a suffix rule rather than a real stemmer on purpose: aggressive
// stemming folds distinct verbs (index/indexing, search/searches) into
// collisions a corpus this small cannot afford.
func FoldPlural(t string) string {
	switch {
	case len(t) > 4 && strings.HasSuffix(t, "ies"):
		return t[:len(t)-3] + "y"
	case len(t) > 4 && (strings.HasSuffix(t, "ses") ||
		strings.HasSuffix(t, "xes") ||
		strings.HasSuffix(t, "zes") ||
		strings.HasSuffix(t, "ches") ||
		strings.HasSuffix(t, "shes")):
		return t[:len(t)-2]
	case len(t) > 3 && strings.HasSuffix(t, "s") &&
		!strings.HasSuffix(t, "ss") && !strings.HasSuffix(t, "us"):
		return t[:len(t)-1]
	}
	return t
}

// Tokenize lowercases, splits on non-alphanumerics, drops function words and
// folds plurals. Corpus and query pass through this same function, so the index
// and the query always speak one vocabulary.
func Tokenize(s string) []string {
	raw := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if _, skip := stopWords[t]; skip {
			continue
		}
		out = append(out, FoldPlural(t))
	}
	return out
}

// Hit pairs a corpus document index with its score against a query.
type Hit struct {
	Doc   int
	Score float64
}

// Index precomputes per-document term frequencies, lengths, the average length
// and the idf table once over an immutable corpus.
type Index struct {
	tf    []map[string]int
	docLn []int
	avgdl float64
	idf   map[string]float64
}

// New builds the index over already-flattened documents. A caller that weights a
// field does so by repeating it in the string it passes here: BM25 has no notion
// of fields, and repetition is how a flat index expresses one.
func New(documents []string) *Index {
	index := &Index{
		tf:    make([]map[string]int, len(documents)),
		docLn: make([]int, len(documents)),
		idf:   make(map[string]float64),
	}
	df := make(map[string]int)
	total := 0
	for i, document := range documents {
		terms := Tokenize(document)
		tf := make(map[string]int, len(terms))
		for _, term := range terms {
			tf[term]++
		}
		index.tf[i] = tf
		index.docLn[i] = len(terms)
		total += len(terms)
		for term := range tf {
			df[term]++
		}
	}
	n := float64(len(documents))
	if len(documents) > 0 {
		index.avgdl = float64(total) / n
	}
	for term, count := range df {
		index.idf[term] = math.Log((n-float64(count)+0.5)/(float64(count)+0.5) + 1)
	}
	return index
}

// Rank scores every document and returns the positive-scoring ones, descending.
// Ties break on ascending document index so a run is reproducible.
func (index *Index) Rank(query string) []Hit {
	terms := Tokenize(query)
	out := make([]Hit, 0, len(index.tf))
	for i := range index.tf {
		if score := index.ScoreTerms(i, terms); score > 0 {
			out = append(out, Hit{Doc: i, Score: score})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].Doc < out[b].Doc
	})
	return out
}

// Score ranks one document against a raw query string.
func (index *Index) Score(doc int, query string) float64 {
	return index.ScoreTerms(doc, Tokenize(query))
}

// ScoreTerms ranks one document against already-tokenized query terms, for a
// caller scoring many documents against the same query.
func (index *Index) ScoreTerms(doc int, terms []string) float64 {
	if index.avgdl == 0 || doc < 0 || doc >= len(index.tf) {
		return 0
	}
	tf := index.tf[doc]
	length := float64(index.docLn[doc])
	score := 0.0
	for _, term := range terms {
		frequency := float64(tf[term])
		if frequency == 0 {
			continue
		}
		denominator := frequency + K1*(1-B+B*length/index.avgdl)
		score += index.idf[term] * (frequency * (K1 + 1)) / denominator
	}
	return score
}
