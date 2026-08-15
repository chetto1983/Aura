// Same-message tool-call repair (D-12/D-13), split out of llm_agent.go per
// RESEARCH #1 so neither file approaches the 600-LOC no-god-class cap
// (CLAUDE.md).
//
// WHY deterministic, never random: uniquifyToolCallIDs' blank-id fallback and
// its collision suffix are both derived only from the call's own name,
// arguments and position — never a random or time-based source. messages[0]
// must stay byte-identical across turns for the KV/prompt cache prefix to
// hold (PROJECT.md constraint); a random id would poison every subsequent
// request's cache prefix and cost the operator money on every turn after it.
// This is a HARD INVARIANT, enforced structurally here (no randomness API is
// imported) rather than by review.
//
// Two independent repairs live in this file, called at hermes' two positions
// in the agent loop (llm_agent.go):
//   - uniquifyToolCallIDs runs the moment the model's response is consumed,
//     before anything downstream (argument validation, the reservation key,
//     dispatch, history) sees the batch.
//   - dedupeSameMessageCalls runs immediately before the assistant message is
//     appended to history, so a dropped duplicate never reaches the wire.
//
// Neither function is the loop-guard dedup in budget_dedup.go: that one
// compares a call against calls from EARLIER ROUNDS (a cross-turn runaway-
// loop detector with a result-change progress veto, D-18). These two compare
// calls WITHIN one assistant message only — a same-message model error, not a
// multi-round pattern — and neither reads nor writes Budget state.
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/chetto1983/aura/internal/canonicaljson"
	"github.com/chetto1983/aura/internal/llm"
)

// uniquifyToolCallIDs repairs the two same-message id failure modes a
// degraded model turn can produce (D-13): a blank id, and two calls sharing
// one id. nil/empty/single-element input is identity — no repair needed and
// none attempted. The input slice is never mutated in place; the caller
// reassigns to the returned slice so the "before any downstream consumer"
// guarantee does not depend on aliasing.
func uniquifyToolCallIDs(calls []llm.ToolCall) []llm.ToolCall {
	if len(calls) < 2 {
		return calls
	}
	out := make([]llm.ToolCall, len(calls))
	copy(out, calls)

	// Step 1: coalesce every blank id into a deterministic, content-derived
	// id FIRST, so it then participates in collision detection exactly like
	// any provider-supplied id below.
	for i := range out {
		if out[i].ID == "" {
			out[i].ID = deterministicBlankCallID(out[i].Function.Name, out[i].Function.Arguments, i)
		}
	}

	// Step 2: repair collisions in slice order — the first occurrence of an
	// id keeps it. Every later occurrence, INCLUDING a later call whose own
	// original id happens to equal an id a prior call was just repaired to,
	// gets `<id>_d<n>`, incrementing n until the candidate is actually free
	// in the taken set. A counter keyed by base name alone (rather than a
	// real membership check against every id assigned so far) would miss
	// exactly this transitive case.
	taken := make(map[string]struct{}, len(out))
	for i := range out {
		id := out[i].ID
		if _, exists := taken[id]; !exists {
			taken[id] = struct{}{}
			continue
		}
		n := 2
		for {
			candidate := fmt.Sprintf("%s_d%d", id, n)
			if _, exists := taken[candidate]; !exists {
				out[i].ID = candidate
				taken[candidate] = struct{}{}
				break
			}
			n++
		}
	}
	return out
}

// deterministicBlankCallID derives call_<first 12 hex chars of
// sha256(name:args:index)> for a tool call with no provider-supplied id
// (D-13). index is the call's position in the ORIGINAL slice, not a
// per-name counter — it is what lets two calls with identical name and
// arguments at different positions in the same message still diverge.
func deterministicBlankCallID(name, args string, index int) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%d", name, args, index))
	return "call_" + hex.EncodeToString(sum[:])[:12]
}

// dedupeSameMessageCalls drops a later call in the SAME assistant message
// whose (name, arguments) exactly repeats an earlier call in that message
// (D-12). Order of the surviving calls is preserved. The dropped call is
// removed entirely — never left in the slice with a marker and never
// answered with a synthesized result — because the provider contract
// requires every tool_call to have exactly one matching result and vice
// versa; a fabricated result would be the harness inventing an outcome.
//
// This is NOT the cross-round loop guard in budget_dedup.go (D-18): that one
// is a runaway-loop veto over calls from DIFFERENT rounds, gated on the
// RESULT staying unchanged across repeats. This function has no notion of
// rounds or results — it only ever looks at calls that arrived together in
// one message — so two identical calls in two separate rounds both survive
// untouched; that discrimination belongs to plan 45-02's round-ordinal work,
// not here. Neither function subsumes the other.
func dedupeSameMessageCalls(calls []llm.ToolCall) []llm.ToolCall {
	if len(calls) < 2 {
		return calls
	}
	seen := make(map[string]struct{}, len(calls))
	out := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		// The separator is a NUL byte, not a plain colon or space: it can
		// never appear in either a tool name or canonical JSON, so two
		// (name, arguments) pairs cannot collide by concatenation alone.
		key := call.Function.Name + "\x00" + string(canonicaljson.CanonicalArgs(call.Function.Arguments))
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, call)
	}
	return out
}
