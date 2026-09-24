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
	memory denseFloors
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
var embeddingGemmaFloors = calibration{
	memory: denseFloors{maxDistance: 0.72, minRelevance: 0.28},
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
