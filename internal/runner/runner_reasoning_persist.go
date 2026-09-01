package runner

import (
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent"
)

// runner_reasoning_persist.go is the fix-plan 1.12 / amendment #91 accumulation
// seam: the runner (NOT the agent — #57's stream-only agent contract is untouched)
// folds the turn's LLMResponse.Reasoning deltas into a bounded per-turn buffer on
// the turnTracker and persistAssistantAnswer lands the joined text + the first→last
// delta wall time on the final assistant answer row, display-only.

// reasoningTruncationMarker is appended once when the accumulated CoT hits the
// AURA_REASONING_PERSIST_MAX_RUNES cap: the HEAD is kept, the tail dropped.
const reasoningTruncationMarker = "\n… [reasoning truncated]"

// observeReasoning accumulates one reasoning delta into the turn's bounded buffer.
// Two gates keep HARDEN-05's posture:
//
//   - reasoningPersistMaxRunes <= 0 → persistence disabled entirely (the knob's
//     documented off switch and the hand-built test-Deps zero value);
//   - !cfg.ShowReasoning → the stream is redacted at the source (the agent replaces
//     every delta with its redaction constant — llm_agent_events.go), so NOTHING is
//     accumulated: a hidden stream must never become a stored verbatim trace. This
//     reads the SAME immutable runtime snapshot the runner hands the agent, so the
//     two gates cannot diverge — no string comparison against
//     the redaction constant is needed.
//
// Timestamps are stamped on EVERY delta (even after the rune cap truncates the
// text) so the persisted duration covers the whole reasoning phase the user
// watched, not just the stored head.
func (r *Runner) observeReasoning(tr *turnTracker, ev *agent.Event) {
	if r.reasoningPersistMaxRunes <= 0 || !r.trackerLLMSnapshot(tr).Config.ShowReasoning {
		return
	}
	r.observeReasoningGraph(tr, ev)
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC() // defensive: the agent always stamps, but never persist a zero time math
	}
	if tr.reasoningFirst.IsZero() {
		tr.reasoningFirst = ts
	}
	tr.reasoningLast = ts
	if tr.reasoningTruncated {
		return
	}
	delta := ev.LLMResponse.Reasoning
	remaining := r.reasoningPersistMaxRunes - tr.reasoningRunes
	n := utf8.RuneCountInString(delta)
	if n <= remaining {
		tr.reasoning.WriteString(delta)
		tr.reasoningRunes += n
		return
	}
	tr.reasoning.WriteString(headRunes(delta, remaining))
	tr.reasoning.WriteString(reasoningTruncationMarker)
	tr.reasoningRunes += remaining
	tr.reasoningTruncated = true
}

// resetReasoning drops everything accumulated so far (B-12 DiscardStreamed: a
// mid-stream retry repudiates the already-streamed deltas, so the persisted CoT
// restarts from the retry's blank slate — mirroring what the consumer renders).
func (t *turnTracker) resetReasoning() {
	t.reasoning.Reset()
	t.reasoningRunes = 0
	t.reasoningTruncated = false
	t.reasoningFirst = time.Time{}
	t.reasoningLast = time.Time{}
}

// persistedReasoning returns the turn's accumulated CoT and its first→last delta
// wall time in ms, both zero-valued when nothing was accumulated ("" / 0 map to
// SQL NULL at the store boundary). A single-delta turn yields duration 0 (also
// NULL): a sub-delta "Thought for 0s" label is meaningless.
func (t *turnTracker) persistedReasoning() (string, int64) {
	text := t.reasoning.String()
	if text == "" {
		return "", 0
	}
	d := max(t.reasoningLast.Sub(t.reasoningFirst).Milliseconds(),
		// defensive: wall clocks can step backwards; never persist a negative duration
		0)
	return text, d
}

// headRunes returns the first n runes of s (the whole string when it is shorter).
func headRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
