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

// searchDocument flattens a tool spec into the one whitespace-joined string the
// scorer indexes: the name (weighted), the Summary, and the argument names. Both
// the raw Name and its underscore→space form are pushed so a query "fetch web"
// matches a tool named web_fetch (D-02 leverage point). On a malformed Parameters
// payload it degrades to name+Summary — never panics.
//
// The long Description is deliberately NOT indexed. It is written to be read at
// USE time, so it carries routing advice about neighbouring tools, and a retriever
// cannot honour negation: document_search's "...NOT the public web (use web
// search/fetch)" made it the rank-1 answer to "web search for <part> specs" seven
// times in production. 22 of 54 descriptions name another tool. The retrieval
// document and the usage document are different documents; this is the retrieval
// one, and Description still ships verbatim once a tool is loaded.
func searchDocument(s Spec) string {
	return buildSearchDocument(s, s.Summary)
}

// nameFieldWeight repeats the name field so it outweighs the schema. BM25 has no
// notion of fields, and repetition is how a flat index expresses one: measured,
// "list files matching a glob pattern" ranked fs_grep over fs_glob because fs_grep
// takes `glob` and `pattern` as arguments while fs_glob merely IS the glob tool. A
// tool named for the capability is the answer to a query naming that capability.
const nameFieldWeight = 3

func buildSearchDocument(s Spec, capability string) string {
	var parts []string
	push := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	for range nameFieldWeight {
		push(s.Name)
		push(strings.ReplaceAll(s.Name, "_", " "))
	}
	push(capability)
	var node schemaNode
	if err := json.Unmarshal(s.Parameters, &node); err == nil {
		appendSchema(node, push)
	}
	return strings.Join(parts, " ")
}

// appendSchema recursively pushes every property NAME (D-02) — and nothing else.
// Argument PROSE is excluded for the same reason the tool Description is: it is
// written to be read at use time, so it discusses neighbouring tools and modes.
// Measured: shell_exec outranked shell_poll for "check the output of a background
// command" because its `background` argument is documented in terms of polling for
// output. Names are the capability signal; sentences about them are not.
// Property keys are sorted so the document is byte-stable across runs (map
// iteration order is otherwise random).
func appendSchema(n schemaNode, push func(string)) {
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

// stopWords are English function words. A model writes its discovery query as a
// sentence ("what time is it", "read a file from disk") while a tool summary is a
// capability line, so a function word that leaks into two or three summaries earns
// a HIGH idf for carrying no meaning at all. Measured: "what time is it" ranked
// web_search first — on "is" and "it" — with current_time only fourth. Dropped from
// the corpus and the query alike, so no document can win on grammar.
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

// foldPlural strips the regular English plural so a query written "files" reaches a
// summary written "file" — measured: "file" and "files" returned disjoint result
// sets. It stays a suffix rule rather than a real stemmer on purpose: the corpus is
// 54 short capability lines, and aggressive stemming folds distinct tool verbs
// (index/indexing, search/searches) into collisions this ranker cannot afford.
func foldPlural(t string) string {
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

// tokenize lowercases, splits on non-alphanumerics, drops function words and folds
// plurals. Corpus and query pass through the same function, so the index and the
// query always speak the same vocabulary.
func tokenize(s string) []string {
	raw := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if _, skip := stopWords[t]; skip {
			continue
		}
		out = append(out, foldPlural(t))
	}
	return out
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
	idx := &bm25Index{
		tf:    make([]map[string]int, len(specs)),
		docLn: make([]int, len(specs)),
		idf:   make(map[string]float64),
	}
	df := make(map[string]int)
	var totalLen int
	for i, s := range specs {
		terms := tokenize(searchDocument(s))
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
