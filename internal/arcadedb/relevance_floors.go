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

// calibration is one space's measured floors, per family.
type calibration struct {
	memory    denseFloors
	documents denseFloors
}

// embeddingGemmaFloors are the only floors ever measured, all on EmbeddingGemma-300M at 768
// dimensions.
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

// calibratedFloors holds one row per space whose floors were measured. Calibrating a new model
// is the measurement behind embeddingGemmaFloors, repeated, and a row added here.
var calibratedFloors = map[string]calibration{
	embeddingGemmaSpace: embeddingGemmaFloors,
}

// floorsFor is space's calibration and whether it was measured. A space without a row is
// served with EmbeddingGemma's floors, which may abstain too often or too rarely for another
// model -- which is what the reason says.
func floorsFor(space string) (calibration, bool) {
	floors, measured := calibratedFloors[space]
	if !measured {
		return embeddingGemmaFloors, false
	}
	return floors, true
}

// FloorsCalibrated reports whether the dense floors were measured for space.
func FloorsCalibrated(space string) bool {
	_, measured := calibratedFloors[space]
	return measured
}

// documentFloors are the dense document floors for space.
func documentFloors(space string) denseFloors {
	calibrated, _ := floorsFor(space)
	return calibrated.documents
}

// bindDenseFloors sets a dense memory read's floors for space, and returns
// ReasonUncalibratedFloors when the table has no row for it. A positive limit is an operator
// override (AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO, AURA_MEMORY_MIN_RELEVANCE) and wins.
func (limits MemoryLimits) bindDenseFloors(params map[string]any, space string) string {
	calibrated, measured := floorsFor(space)
	floors := calibrated.memory
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
