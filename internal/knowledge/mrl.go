package knowledge

import (
	"fmt"
	"math"
)

// TruncateMRL narrows a wider embedding to the requested width using Matryoshka
// Representation Learning: keep the leading dim components, renormalise to unit
// length. The width is never decided here — it is whatever AURA_EMBED_DIMENSIONS
// says the vector index was built for.
//
// It lives beside pingEmbed because the two halves are one contract: boot refuses a
// sidecar NARROWER than the index, and every write narrows a wider one. Split across
// packages, a caller can satisfy the boot check and still write an un-narrowed vector
// — which Neo4j accepts and silently leaves out of the index.
//
// MRL-trained embedders publish a native width larger than the vector index expects
// and are explicitly trained so that a leading slice is itself a valid embedding. The
// truncation has to happen client-side because the llama.cpp sidecar ignores the
// OpenAI `dimensions` request parameter — verified: asking for 256 returns the
// model's native width unchanged — so nothing upstream can narrow it for us.
// Renormalising matters: a truncated slice is no longer unit length, and cosine over
// unnormalised vectors would silently skew every score.
//
// A shorter-than-contract embedding is an error, not something to pad: it means the
// sidecar is serving a different model than the index was built for.
func TruncateMRL(vec []float64, dim int) ([]float64, error) {
	if len(vec) == dim {
		return vec, nil
	}
	if len(vec) < dim {
		return nil, fmt.Errorf("has dimension %d, want %d", len(vec), dim)
	}
	out := make([]float64, dim)
	copy(out, vec[:dim])
	var sum float64
	for _, v := range out {
		sum += v * v
	}
	if sum == 0 {
		return nil, fmt.Errorf("truncated to %d components that are all zero", dim)
	}
	norm := math.Sqrt(sum)
	for i := range out {
		out[i] /= norm
	}
	return out, nil
}
