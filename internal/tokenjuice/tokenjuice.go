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
// If rules contains no "generic/fallback" entry an in-memory fallback is
// synthesized so the engine never panics (boot non-fatal per Aura convention).
// Full reduce pipeline implemented in US-TJ02/TJ03; skeleton returns passthrough.
func CompactWithRules(in Input, rules []*CompiledRule, opts Options) Result {
	minBytes := opts.MinInputBytes
	if minBytes == 0 {
		minBytes = defaultMinInputBytes
	}

	origBytes := len(in.Stdout)

	if origBytes < minBytes {
		return Result{
			InlineText: in.Stdout,
			Applied:    false,
			RuleID:     "none/too-small",
			Stats: Stats{
				ToolName:       in.ToolName,
				OriginalBytes:  origBytes,
				CompactedBytes: origBytes,
				OriginalChars:  countTextChars(in.Stdout),
				CompactedChars: countTextChars(in.Stdout),
				Confidence:     0,
			},
		}
	}

	// Classify + reduce pipeline (US-TJ02/TJ03).
	// Skeleton: passthrough until rules and reduce are wired in.
	return Result{
		InlineText: in.Stdout,
		Applied:    false,
		RuleID:     "none/no-rules",
		Stats: Stats{
			ToolName:       in.ToolName,
			OriginalBytes:  origBytes,
			CompactedBytes: origBytes,
			OriginalChars:  countTextChars(in.Stdout),
			CompactedChars: countTextChars(in.Stdout),
			Confidence:     0,
		},
	}
}
