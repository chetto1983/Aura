// Package tokenjuice implements a rule-driven terminal-output compactor.
// Tool outputs are classified against JSON rules, then reduced before being
// committed to the LLM conversation window (algorithm spec: docs/tokenjuice-algorithm-spec.md).
//
// Wiring point: internal/agent/executor.go before WrapUntrustedToolResult.
// Feature flag: AURA_TOKENJUICE_ENABLED.
package tokenjuice

const (
	defaultMinInputBytes  = 512
	defaultMinRatio       = 0.95
	defaultMaxInlineChars = 1200
)

// Compact runs the engine with the builtin rule set.
// Safe to call concurrently.
func Compact(in Input, opts Options) Result {
	return CompactWithRules(in, LoadBuiltinRules(), opts)
}

// CompactWithRules runs the engine with an explicit rule slice.
// If rules contains no "generic/fallback" entry the engine degrades gracefully
// to passthrough (boot non-fatal per Aura convention).
func CompactWithRules(in Input, rules []*CompiledRule, opts Options) Result {
	return ReduceExecutionWithRules(in, rules, opts)
}
