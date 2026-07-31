package documents

import "sort"

// fuseSeeds merges two independently-ranked candidate lists into one, reusing the
// rrfK damping constant the rerank guard already blends with.
//
// The two legs answer different questions and neither subsumes the other: the dense
// leg matches meaning, so it survives paraphrase and translation; the lexical leg
// matches strings, so it is the only one that can find a name, a code, or a part
// number. Their scores live on incompatible scales (BM25 ~2.8 against cosine ~0.85),
// so they cannot be compared — only their RANKS can, which is what reciprocal rank
// fusion consumes.
//
// A chunk both legs return outranks a chunk either returns alone. That is the point:
// two retrievers that disagree about everything else agreeing on one chunk is the
// strongest evidence available before a reranker reads a single token.
func fuseSeeds(lexical, dense []SearchHit) []SearchHit {
	// With one leg empty there is nothing to fuse and no scale conflict to resolve,
	// so the surviving leg passes through with its own scores intact. Rewriting them
	// would replace a meaningful cosine with a rank constant for no gain.
	if len(lexical) == 0 {
		return dense
	}
	if len(dense) == 0 {
		return lexical
	}
	type fused struct {
		hit   SearchHit
		score float64
		order int
	}
	byChunk := make(map[string]*fused, len(lexical)+len(dense))
	ordered := make([]*fused, 0, len(lexical)+len(dense))

	add := func(hits []SearchHit) {
		for rank, hit := range hits {
			contribution := 1.0 / (float64(rrfK) + float64(rank) + 1.0)
			if existing, ok := byChunk[hit.ChunkID]; ok {
				existing.score += contribution
				continue
			}
			entry := &fused{hit: hit, score: contribution, order: len(ordered)}
			byChunk[hit.ChunkID] = entry
			ordered = append(ordered, entry)
		}
	}
	// Lexical first so that, on an exact tie, the leg that matched the literal wins.
	add(lexical)
	add(dense)

	sort.SliceStable(ordered, func(a, b int) bool {
		if ordered[a].score != ordered[b].score {
			return ordered[a].score > ordered[b].score
		}
		return ordered[a].order < ordered[b].order
	})

	out := make([]SearchHit, 0, len(ordered))
	for _, entry := range ordered {
		// The fused score replaces the per-leg one because the two scales are not
		// comparable and a caller sorting by Score must get the fused order. The
		// reranker overwrites it again downstream whenever it is healthy.
		hit := entry.hit
		hit.Score = entry.score
		out = append(out, hit)
	}
	return out
}
