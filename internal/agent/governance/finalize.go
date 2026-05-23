package governance

import "strings"

// SynthesisPrompt is the forced-finalization prompt injected when a budget
// signal terminates the turn before the model produces a natural answer
// (US-OUT-09, Hermes pattern). The placeholder {stop_reason} is replaced with
// the concrete StopReason* constant so the user-facing reply names which
// signal fired, enabling the user to understand what happened.
const SynthesisPrompt = "Budget exhausted (signal: {stop_reason}). Synthesize the best answer you have from the context so far. Be honest about what is missing. Do not call any tools."

// FormatSynthesisPrompt returns SynthesisPrompt with {stop_reason} substituted
// by the provided stop-reason string.
func FormatSynthesisPrompt(stopReason string) string {
	return strings.ReplaceAll(SynthesisPrompt, "{stop_reason}", stopReason)
}
