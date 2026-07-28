package tools

import (
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/semindex"
)

// Free-text tool_search is EMBEDDING-PRIMARY (spike-056): embedding cosine
// over the bm25-doc text is the order. BM25 contributes ONLY as a guarded tiebreak,
// never as naive RRF — on Aura's dominant Italian-query traffic BM25 has zero signal
// (Italian query vs English tool description = no lexical overlap), so fusing it as a
// co-equal ranker drags correct embedding-#1 hits down (spike-056: "che tempo fa"
// 1→4). The guard makes BM25 a strict no-op precisely when it is noisy and lets it
// break ties only when it is a CONFIDENT lexical hit.

const (
	// fusionGuardMaxBM25 is the result-set ceiling: BM25 contributes only when it
	// returned a SMALL set (a confident lexical match, not a noisy flood of weak
	// partial-token hits). Hard-coded — correctness-bearing per Req-2 no-regression,
	// spike-056 validated (the guard ≡ pure embedding on the IT corpus, strictly
	// safer for English/keyword queries). Not an env knob (D-02a: no config bloat).
	fusionGuardMaxBM25 = 5
	// fusionGuardTopK is the embedding-intersection gate: a BM25 hit is honored only
	// when the tool is ALSO in embedding's top-15, so BM25 can reorder within
	// embedding's confident region but can never inject a tool embedding rejected.
	fusionGuardTopK = 15
)

// fusionEnv is the ONE env kill-switch (D-02a): AURA_TOOLSEARCH_FUSION=guarded|off.
// Default "guarded"; "off" falls back to pure embedding (the guard never fires).
const fusionEnv = "AURA_TOOLSEARCH_FUSION"

// fusionEnabled reports whether the guarded BM25 tiebreak is active. Any value other
// than an explicit "off" (case-insensitive) keeps the validated default behavior, so
// a typo or unset var never silently disables the safety tiebreak.
func fusionEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(fusionEnv)), "off")
}

// guardedTiebreak reorders the embedding-ranked tool names using BM25 ONLY as a
// confident, intersection-gated tiebreak (NOT naive RRF). embRanked is the
// embedding order (descending cosine); bm25Ranked is the BM25 positive-scoring order
// (descending score). The result is embedding-primary: a BM25 hit is promoted above
// embedding ties only when (a) the BM25 result-set is small (≤5) AND (b) the tool is
// in embedding's top-15. When the guard does not apply (BM25 flooded, the switch is
// off, or a BM25 tool is outside embedding's confident region) the embedding order is
// returned unchanged.
func guardedTiebreak(embRanked []semindex.Scored, bm25Ranked []string) []semindex.Scored {
	if !fusionEnabled() || len(bm25Ranked) == 0 || len(bm25Ranked) > fusionGuardMaxBM25 {
		return embRanked
	}
	// Embedding's confident region: the top-fusionGuardTopK labels. A BM25 hit
	// outside this set is ignored (the intersection gate) so BM25 can never inject a
	// tool embedding ranked low.
	top := make(map[string]struct{}, fusionGuardTopK)
	for i, s := range embRanked {
		if i >= fusionGuardTopK {
			break
		}
		top[s.Label] = struct{}{}
	}
	// Additive boost in BM25 rank order, intersection-gated. The boost is small and
	// applied on top of the embedding cosine so embedding stays primary; it only
	// re-breaks ties / nudges within the confident region.
	boost := make(map[string]float64, len(bm25Ranked))
	for i, name := range bm25Ranked {
		if _, ok := top[name]; !ok {
			continue
		}
		boost[name] = 1.0 / float64(fusionGuardTopK+i+1)
	}
	if len(boost) == 0 {
		return embRanked
	}
	out := make([]semindex.Scored, len(embRanked))
	copy(out, embRanked)
	stableSortScored(out, boost)
	return out
}

// stableSortScored re-sorts the embedding scores by (cosine + guarded BM25 boost),
// descending, preserving the embedding insertion order as the deterministic tie-break
// (a stable sort over an already-embedding-ordered slice). The boost is additive so
// the embedding cosine dominates; the BM25 term only resolves near-ties.
func stableSortScored(s []semindex.Scored, boost map[string]float64) {
	// Insertion sort keeps the slice stable and is trivially fast for the N≤~115 tool
	// corpus (brute-force only, no ANN — Req-8). The fused key is Score+boost.
	for i := 1; i < len(s); i++ {
		cur := s[i]
		curKey := s[i].Score + boost[s[i].Label]
		j := i - 1
		for j >= 0 && s[j].Score+boost[s[j].Label] < curKey {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = cur
	}
}
