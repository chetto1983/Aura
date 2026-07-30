package tools

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

// In-process Okapi BM25 ranker over deferred-tool "search documents". This is
// the in-house D-04 resolution: no DB, no embedding sidecar, no new dependency —
// stdlib only. The corpus is the (immutable, N≤~50) set of deferred tools, so a
// naive O(N·terms) scan is sub-millisecond and no inverted index is warranted.

const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// schemaNode is the minimal recursive JSON-Schema shape searchDocument walks. A1
// (verified during planning) confirms no current tool uses oneOf/allOf/$ref, so
// Description+Properties+Items+AnyOf cover every registered Parameters payload.
type schemaNode struct {
	Description string                `json:"description"`
	Properties  map[string]schemaNode `json:"properties"`
	Items       *schemaNode           `json:"items"`
	AnyOf       []schemaNode          `json:"anyOf"`
}

// searchDocument flattens a tool spec into one whitespace-joined string the BM25
// scorer indexes. Both the raw Name and its underscore→space form are pushed so
// a query "fetch web" matches a tool named web_fetch (D-02 leverage point). On a
// malformed Parameters payload it degrades to Name+Description — never panics.
func searchDocument(s Spec) string {
	return buildSearchDocument(s, s.Description)
}

// summarySearchDocument is the production no-network retrieval document. Long
// tool descriptions contain routing cross-references to other tools, so indexing
// them makes a disclaimer such as "not the public web; use web_search" rank the
// wrong tool for "web search". Summary keeps the capability signal while schema
// names/descriptions preserve argument-level discovery.
func summarySearchDocument(s Spec) string {
	return buildSearchDocument(s, s.Summary)
}

func buildSearchDocument(s Spec, capability string) string {
	var parts []string
	push := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	push(s.Name)
	push(strings.ReplaceAll(s.Name, "_", " "))
	push(capability)
	var node schemaNode
	if err := json.Unmarshal(s.Parameters, &node); err == nil {
		appendSchema(node, push)
	}
	return strings.Join(parts, " ")
}

// appendSchema recursively pushes a schema's description, every property NAME
// (D-02), and the descriptions of nested objects/arrays/unions. Property keys are
// sorted so the produced document is byte-stable across runs (map iteration order
// is otherwise random — determinism matters for snapshot tests, not for scores).
func appendSchema(n schemaNode, push func(string)) {
	push(n.Description)
	keys := make([]string, 0, len(n.Properties))
	for k := range n.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		push(k)
		appendSchema(n.Properties[k], push)
	}
	if n.Items != nil {
		appendSchema(*n.Items, push)
	}
	for _, v := range n.AnyOf {
		appendSchema(v, push)
	}
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}

// scoredDoc pairs a corpus document index with its BM25 score against a query.
type scoredDoc struct {
	doc   int
	score float64
}

// bm25Index precomputes per-document term frequencies, lengths, the average doc
// length, and the idf table once over an immutable corpus. rank then scores a
// query in O(docs·queryTerms) with no further allocation of the corpus.
type bm25Index struct {
	tf    []map[string]int
	docLn []int
	avgdl float64
	idf   map[string]float64
}

func newBM25Index(specs []Spec) *bm25Index {
	return newBM25IndexWithDocument(specs, searchDocument)
}

func newSummaryBM25Index(specs []Spec) *bm25Index {
	return newBM25IndexWithDocument(specs, summarySearchDocument)
}

func newBM25IndexWithDocument(
	specs []Spec,
	document func(Spec) string,
) *bm25Index {
	idx := &bm25Index{
		tf:    make([]map[string]int, len(specs)),
		docLn: make([]int, len(specs)),
		idf:   make(map[string]float64),
	}
	df := make(map[string]int)
	var totalLen int
	for i, s := range specs {
		terms := tokenize(document(s))
		tf := make(map[string]int, len(terms))
		for _, t := range terms {
			tf[t]++
		}
		idx.tf[i] = tf
		idx.docLn[i] = len(terms)
		totalLen += len(terms)
		for t := range tf {
			df[t]++
		}
	}
	n := float64(len(specs))
	if len(specs) > 0 {
		idx.avgdl = float64(totalLen) / n
	}
	for t, nt := range df {
		idx.idf[t] = math.Log((n-float64(nt)+0.5)/(float64(nt)+0.5) + 1)
	}
	return idx
}

// rank scores every document against the query and returns the positive-scoring
// docs sorted by descending score (ties broken by ascending doc index for
// determinism). Docs sharing no query term score 0 and are excluded.
func (idx *bm25Index) rank(query string) []scoredDoc {
	qterms := tokenize(query)
	out := make([]scoredDoc, 0, len(idx.tf))
	for i := range idx.tf {
		if s := idx.scoreDoc(i, qterms); s > 0 {
			out = append(out, scoredDoc{doc: i, score: s})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		return out[a].doc < out[b].doc
	})
	return out
}

func (idx *bm25Index) scoreDoc(doc int, qterms []string) float64 {
	if idx.avgdl == 0 {
		return 0
	}
	tf := idx.tf[doc]
	docLen := float64(idx.docLn[doc])
	var score float64
	for _, t := range qterms {
		f := float64(tf[t])
		if f == 0 {
			continue
		}
		denom := f + bm25K1*(1-bm25B+bm25B*docLen/idx.avgdl)
		score += idx.idf[t] * (f * (bm25K1 + 1)) / denom
	}
	return score
}
