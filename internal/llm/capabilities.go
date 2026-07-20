package llm

import "math"

// EstimatorCapability records the tokenizer or conservative estimator contract.
// It is the surviving seed of the (removed) Phase-42 capability preflight, kept for
// the Wave-1.10 token-accounting layer (honest per-provider token estimation). The
// dark durable-compaction preflight that consumed the rest of the old capability row
// was deleted with the engine (Amendment #86).
type EstimatorCapability struct {
	ID, Version            string
	ProviderTokenizer      bool
	DeclaredErrorTokens    int
	ConservativeMultiplier float64
	ConservativeAddend     int
}

// ConservativeTokenUpperBound applies the mandated cold-start estimate bound.
func ConservativeTokenUpperBound(estimate int) int {
	if estimate <= 0 {
		return 256
	}
	return int(math.Ceil(float64(estimate)*1.15)) + 256
}
