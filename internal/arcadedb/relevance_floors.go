package arcadedb

import (
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// ReasonUncalibratedFloors marks a dense answer admitted by floors never measured for the
// space it ran in (spec §9). It is not a degradation -- the dense leg ran -- so it travels in a
// field of its own, apart from the reason a read left the dense path.
const ReasonUncalibratedFloors = "uncalibrated_floors"

// denseFloors is one family's dense admission: how far a neighbour may lie, and how low the
// reranked cosine of a kept candidate may fall. vector.neighbors returns its k nearest however
// far away they are, so without these nothing is ever "no qualified candidates".
type denseFloors struct {
	maxDistance  float64
	minRelevance float64
}

// calibration is one space's floors, per family.
type calibration struct {
	memory    denseFloors
	documents denseFloors
}

// embeddingGemmaFloors are the first floors measured, on EmbeddingGemma-300M at 768 dimensions,
// and the ones a family without a row for its space is served with.
//
// Memory, 2026-09-02, a live 102-fact memory: the nearest neighbour for a question the memory
// COULD answer sat at 0.514 and 0.667; for one it could not ("ricetta della pizza napoletana",
// "chi ha vinto il mondiale 1982") at 0.777 and 0.890, and 0.72 is the midpoint of that band.
// The previous 0.55 fell INSIDE the true-match band, so it discarded correct facts while
// admitting nothing useful -- the dense leg came back empty and "hybrid" retrieval ran on its
// lexical leg alone. Not established by that measure: conversation turns, other identities'
// corpora, or a memory much larger than 102 facts.
//
// Documents, 2026-09-09, the live 1079-passage corpus:
//
// 0.72 is the midpoint of a measured band, not a guess. The nearest neighbour for five
// answerable questions sat at 0.247, 0.400, 0.420, 0.651 and 0.712; for five the corpus does
// not hold ("ricetta della carbonara", "chi ha vinto il mondiale 1982", potatura, traghetti,
// vitamina B12) at 0.706, 0.731, 0.794, 0.798 and 0.799. It keeps all five true matches and
// drops four of the five others. The margin is thin — 0.7124 against 0.7063 — so this is a
// knob with its measurement written beside it, not a clean separation: "orari dei traghetti
// per la Sardegna" is still admitted, which is what the relevance floor finally rejects.
//
// The relevance floor is the abstention gate, and it sits AFTER the rerank on purpose. The
// distance bounds the dense leg only; a lexical hit on an incidental word still entered the
// result set with nothing left to reject it, because `vector.fuse` scores by reciprocal rank
// -- 1/(60+rank), identical for every source's rank 1 -- and a rank carries no relevance. All
// five out-of-corpus questions scored exactly 0.016393442, to the digit. No threshold on that
// number can separate anything. `vector.rerank` re-scores the fused candidates against their
// full-precision vectors and emits a real cosine, which can be thresholded.
//
// 0.32 is the midpoint of 0.2990..0.3445, measured the same day on the same corpus. Worst true
// match: "w2-ee2226e3", a bare worker identifier, at 0.3445. Best false match: "orari dei
// traghetti per la Sardegna" at 0.2990 -- the one the distance admitted. The rest sat far from
// the edge: answerable 0.4508..0.7530, out-of-corpus 0.1737..0.2171 ("ricetta della carbonara"
// 0.1803).
//
// The identifier group is why the floor is 0.32 and not the 0.3749 the prose questions alone
// suggested: an exact identifier is a short query against long prose, so its cosine runs low
// even when the match is exact and correct. Memory solves the same tension with a query-shape
// exemption (lexicalScoreFloor returns 0 for a one-token query); here the measured band does
// not need one, and a floor chosen without those five queries would have rejected a correct
// lookup.
//
// What the measurement does NOT support: a claim beyond 15 queries, one corpus, one embedder,
// one day, and top-1 only. The band is 0.0455 wide. That memory measured 0.28 on a different
// corpus is a second, independent measurement, not a shared constant.
var embeddingGemmaFloors = calibration{
	memory:    denseFloors{maxDistance: 0.72, minRelevance: 0.28},
	documents: denseFloors{maxDistance: 0.72, minRelevance: 0.32},
}

// embeddingGemmaSpace is the space those measurements ran in: the local sidecar serving
// EmbeddingGemma-300M Q8_0 at 768 dimensions, recipe 1, as its /v1/models attests it (lab VM,
// 2026-09-24). The key is the space, not the model name, because the space also names the
// width and the recipe, and either changes the vectors a floor was measured on.
var embeddingGemmaSpace = embeddings.SpaceFor(config.EmbedLocal, "", "", vectorDimensions,
	"embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0").ID

// pplxEmbed4BSpace is the lab VM's cloud route as the daemon logs it (2026-10-23): OpenRouter's
// perplexity/pplx-embed-v1-4b at 768 dimensions, recipe 1.
var pplxEmbed4BSpace = embeddings.SpaceFor(config.EmbedOpenRouter, "perplexity/pplx-embed-v1-4b", "",
	vectorDimensions, "").ID

// pplxEmbed4BDocumentFloors were measured 2026-10-23 on the operator's 8-document library, 29
// questions, top-1 (docs/superpowers/verification/2026-10-23-document-floors-pplx.md). True
// matches scored 0.4047..0.6523 and out-of-corpus questions 0.2637..0.2961. "fattura", which
// returned an unrelated Markdown example by its card, scored 0.3833.
//
// 0.39 is the midpoint of "fattura" and the worst true match, the operator's choice over 0.35,
// the midpoint of true and out-of-corpus. It drops "fattura" and no measured true match, on a
// band 0.0214 wide. The distance dropped no true match, so it stays at 0.72.
//
// Memory was not measured on this space: it has no row in memoryFloorsBySpace.
var pplxEmbed4BDocumentFloors = denseFloors{maxDistance: 0.72, minRelevance: 0.39}

// memoryFloorsBySpace and documentFloorsBySpace hold one row per space whose floors were
// measured, one table per family, because a model can be measured for one and not the other.
// Calibrating a model is the measurement behind embeddingGemmaFloors, repeated, and a row added.
var (
	memoryFloorsBySpace = map[string]denseFloors{
		embeddingGemmaSpace: embeddingGemmaFloors.memory,
	}
	documentFloorsBySpace = map[string]denseFloors{
		embeddingGemmaSpace: embeddingGemmaFloors.documents,
		pplxEmbed4BSpace:    pplxEmbed4BDocumentFloors,
	}
)

// measuredFloors is space's row in one family's table and whether it has one. A space without
// one is served with fallback, EmbeddingGemma's, which may abstain too often or too rarely for
// another model -- which is what ReasonUncalibratedFloors says.
func measuredFloors(table map[string]denseFloors, space string, fallback denseFloors) (denseFloors, bool) {
	floors, measured := table[space]
	if !measured {
		return fallback, false
	}
	return floors, true
}

// MemoryFloorsCalibrated reports whether the dense memory floors were measured for space.
func MemoryFloorsCalibrated(space string) bool {
	_, measured := memoryFloorsBySpace[space]
	return measured
}

// DocumentFloorsCalibrated reports whether the dense document floors were measured for space.
func DocumentFloorsCalibrated(space string) bool {
	_, measured := documentFloorsBySpace[space]
	return measured
}

// documentFloors are the dense document floors for space. A positive limit is an operator
// override (AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO, AURA_DOCUMENT_MIN_RELEVANCE) and wins.
func (cfg DocumentIndexConfig) documentFloors(space string) denseFloors {
	floors, _ := measuredFloors(documentFloorsBySpace, space, embeddingGemmaFloors.documents)
	if cfg.DenseMaxDistance > 0 {
		floors.maxDistance = cfg.DenseMaxDistance
	}
	if cfg.MinRelevance > 0 {
		floors.minRelevance = cfg.MinRelevance
	}
	return floors
}

// bindDenseFloors sets a dense memory read's floors for space, and returns
// ReasonUncalibratedFloors when memoryFloorsBySpace has no row for it. A positive limit is an
// operator override (AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO, AURA_MEMORY_MIN_RELEVANCE) and wins.
func (limits MemoryLimits) bindDenseFloors(params map[string]any, space string) string {
	floors, measured := measuredFloors(memoryFloorsBySpace, space, embeddingGemmaFloors.memory)
	if limits.DenseMaxDistance > 0 {
		floors.maxDistance = limits.DenseMaxDistance
	}
	if limits.MinRelevance > 0 {
		floors.minRelevance = limits.MinRelevance
	}
	params["max_distance"], params["min_relevance"] = floors.maxDistance, floors.minRelevance
	if measured {
		return ""
	}
	return ReasonUncalibratedFloors
}
