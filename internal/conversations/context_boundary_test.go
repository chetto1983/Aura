// Boundary/operator pinning for the L1/L2/L2.5 ladder, added for the Phase-4
// mutation gate (CLAUDE.md >=0.70). These exercise BOTH sides of each guard in
// context.go precisely (just below / at / just above the hard cap and the warn
// band; tool vs non-tool turns at the eviction boundary; system-at-head detection)
// and assert the EXACT resulting message set / rot-event action / WARN emission,
// not just a final length. Pure functions only — no DB (the fake emitter records
// the L2.5 write). Shares fakeRotEmitter/mustEncoderRaw from context_unit_test.go.
package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// windowFor returns a ContextConfig whose hardCap() is EXACTLY want. With
// MaxOutputTokens at the 20000 floor, hardCap = ContextWindow - 20000 - 13000, so
// ContextWindow = want + 33000. ToolEvictAfterTurns is huge so L1 never fires —
// these tests isolate the L2/L2.5 budget math from L1 eviction.
func windowFor(want int) ContextConfig {
	return ContextConfig{ContextWindow: want + l2MinOutputReservation + l2HeadroomTokens, MaxOutputTokens: 1, ToolEvictAfterTurns: 1_000_000}
}

// pairHistory builds [system, user, assistant] where the user+assistant bodies
// are sized so the whole history totals exactly `target` tokens. It returns the
// turns; the caller dials hardCap around target to hit a boundary.
func sizedTurns(t *testing.T, enc *tiktoken.Tiktoken, bodyTokens int) []Turn {
	t.Helper()
	// "word " is 1 token in cl100k_base; repeat to reach the wanted body size.
	body := strings.Repeat("word ", bodyTokens)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: body},
		{Seq: 3, Role: llm.RoleAssistant, Content: body},
		{Seq: 4, Role: llm.RoleUser, Content: "q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "a"},
	}
	return turns
}

func toolCallsJSON(t *testing.T, ids ...string) []byte {
	t.Helper()
	calls := make([]llm.ToolCall, len(ids))
	for i, id := range ids {
		calls[i].ID = id
		calls[i].Type = "function"
		calls[i].Function.Name = "fixture_tool"
		calls[i].Function.Arguments = "{}"
	}
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	return raw
}

func assertToolRoundsIntact(t *testing.T, msgs []llm.Message) {
	t.Helper()
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		if msg.Role == llm.RoleTool {
			t.Fatalf("orphan tool message at index %d: %+v", i, msg)
		}
		if msg.Role != llm.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		for j, call := range msg.ToolCalls {
			idx := i + 1 + j
			if idx >= len(msgs) {
				t.Fatalf("tool call %q at index %d has no following tool result", call.ID, i)
			}
			got := msgs[idx]
			if got.Role != llm.RoleTool || got.ToolCallID != call.ID {
				t.Fatalf("tool call %q at index %d must be followed by its tool result, got index %d %+v", call.ID, i, idx, got)
			}
		}
		i += len(msg.ToolCalls)
	}
}

// captureSlog installs a slog handler recording WARN+ records for the duration of
// the test, then restores the default. It returns a func reporting whether any
// "warn cap" record was emitted.
func captureSlog(t *testing.T) func() bool {
	t.Helper()
	var mu sync.Mutex
	var warned bool
	prev := slog.Default()
	slog.SetDefault(slog.New(&warnCapHandler{mu: &mu, warned: &warned}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		return warned
	}
}

type warnCapHandler struct {
	mu     *sync.Mutex
	warned *bool
}

func (h *warnCapHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *warnCapHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn && strings.Contains(r.Message, "warn cap") {
		h.mu.Lock()
		*h.warned = true
		h.mu.Unlock()
	}
	return nil
}
func (h *warnCapHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnCapHandler) WithGroup(string) slog.Handler      { return h }

// TestLadder_L2WarnBand_Boundaries pins the warn-band guard (context.go:118):
// `hardCap > 0 && tokensAfterL1 > int(0.75*hardCap) && tokensAfterL1 <= hardCap`.
// The WARN is the observable contract. We dial hardCap so tokensAfterL1 lands:
//
//	(a) at/below 0.75*hardCap   -> NO warn   (kills `>`->`>=` upper-arm flips)
//	(b) strictly inside the band -> WARN      (kills `&&`->`||` and true-stubs)
//	(c) exactly at hardCap        -> WARN      (kills `<=`->`<` on the third arm)
//	(d) above hardCap             -> NO warn  (L2.5 path; the band guard is false)
func TestLadder_L2WarnBand_Boundaries(t *testing.T) {
	enc := mustEncoderRaw(t)
	turns := sizedTurns(t, enc, 800)
	tokens := totalTokens(enc, turns)

	cases := []struct {
		name     string
		hardCap  int
		wantWarn bool
	}{
		// 0.75*hardCap == tokens exactly -> tokens > 0.75*hardCap is FALSE -> no warn.
		{"at-warn-threshold", tokens * 4 / 3, false},
		// tokens strictly between 0.75*hardCap and hardCap -> warn.
		{"inside-band", tokens + tokens/10, true},
		// hardCap == tokens exactly -> tokens <= hardCap true, tokens > 0.75*hardCap true -> warn.
		{"at-hard-cap", tokens, true},
		// hardCap < tokens -> over the cap, the warn-band guard is false (L2.5 path).
		{"above-hard-cap", tokens - 50, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warned := captureSlog(t)
			emit := &fakeRotEmitter{}
			_, _ = applyContextLadder(context.Background(), "conv", turns, windowFor(tc.hardCap), enc, emit)
			if got := warned(); got != tc.wantWarn {
				t.Fatalf("hardCap=%d tokens=%d: want warn=%v, got %v", tc.hardCap, tokens, tc.wantWarn, got)
			}
		})
	}
}

// TestLadder_HardCapGate_AtAndAbove pins context.go
// `if hardCap == 0 || tokensAfterL1 <= hardCap { return early }`.
//   - hardCap == tokens EXACTLY -> return the L1 result, ZERO rot events
//     (kills `<=`->`<`, which would fall through to L2.5 and drop a pair).
//   - hardCap == 0 (degenerate ContextWindow <= 0) -> return early with the full
//     history, no error (kills `false || ...`, which would run L2.5 against a 0
//     cap). M-03: a small-but-positive window no longer reaches the hardCap==0
//     branch (it floors to ContextWindow/2 and still protects — see
//     TestLadder_SmallWindowFloor_ProtectsNotRaw); only a ContextWindow<=0
//     misconfig yields hardCap==0, which this sub-case now exercises.
func TestLadder_HardCapGate_AtAndAbove(t *testing.T) {
	enc := mustEncoderRaw(t)
	turns := sizedTurns(t, enc, 400)
	tokens := totalTokens(enc, turns)

	// (a) exactly at the cap: no drop, no rot event, all turns survive.
	emit := &fakeRotEmitter{}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, windowFor(tokens), enc, emit)
	if err != nil {
		t.Fatalf("at-cap ladder: %v", err)
	}
	if len(emit.calls) != 0 {
		t.Fatalf("exactly at hardCap must NOT trigger L2.5, got %d rot events", len(emit.calls))
	}
	if len(msgs) != len(turns) {
		t.Fatalf("exactly at hardCap must keep all %d turns, got %d", len(turns), len(msgs))
	}

	// (b) hardCap == 0 via a degenerate ContextWindow <= 0. Before M-03 this branch
	// was reached by any small window (formula clamped to 0) and silently disabled
	// L2.5 — that protection-OFF behavior was the bug M-03 fixes, so the sub-case is
	// repointed to the only genuine hardCap==0 path that remains: a window<=0
	// misconfig (smallWindowHardCapFloor returns 0 for window<=0). The short-circuit
	// must still return the full history without error.
	degenerate := ContextConfig{ContextWindow: 0, MaxOutputTokens: 1, ToolEvictAfterTurns: 1_000_000}
	if hc := degenerate.hardCap(); hc != 0 {
		t.Fatalf("a ContextWindow<=0 misconfig must yield hardCap==0, got %d", hc)
	}
	emit0 := &fakeRotEmitter{}
	msgs0, err0 := applyContextLadder(context.Background(), "conv", turns, degenerate, enc, emit0)
	if err0 != nil {
		t.Fatalf("hardCap==0 must return early without error, got %v", err0)
	}
	if len(emit0.calls) != 0 || len(msgs0) != len(turns) {
		t.Fatalf("hardCap==0 must bypass L2.5 and keep all turns; rot=%d msgs=%d", len(emit0.calls), len(msgs0))
	}
}

// TestLadder_L25Termination_ExactFit pins context.go:130
// `if pairsDropped == 0 || tokensAfter > hardCap { return ErrContextWindowExceeded }`.
// We size the history so that dropping exactly one pair brings tokensAfter to a
// value <= hardCap (a successful reduction): the ladder must emit ONE rot event
// and return the reduced messages, NOT the error. This kills `>`->`>=` (which at
// an exact fit would call it "still over" and error) and the `false || ...` stub.
func TestLadder_L25Termination_ExactFit(t *testing.T) {
	enc := mustEncoderRaw(t)
	big := strings.Repeat("word ", 600)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "recent a"},
	}
	// After dropping the first (big) pair, only [system, q, a] remain — measure it.
	afterDrop := totalTokens(enc, []Turn{turns[0], turns[3], turns[4]})
	full := totalTokens(enc, turns)
	// hardCap sits between afterDrop and full: one drop is required AND sufficient.
	hardCap := afterDrop // exact-fit: tokensAfter == hardCap, so `> hardCap` is false.
	if hardCap >= full {
		t.Fatalf("test setup: need afterDrop(%d) < full(%d)", afterDrop, full)
	}

	emit := &fakeRotEmitter{}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, windowFor(hardCap), enc, emit)
	if err != nil {
		t.Fatalf("an exact-fit reduction must succeed, got error %v", err)
	}
	if len(emit.calls) != 1 || emit.calls[0].pairsDropped != 1 {
		t.Fatalf("want exactly one rot event dropping 1 pair, got %+v", emit.calls)
	}
	// [system, recent q, recent a] survive; the oldest big pair is gone.
	if len(msgs) != 3 || msgs[0].Role != llm.RoleSystem || msgs[1].Content != "recent q" {
		t.Fatalf("L2.5 must drop the oldest pair, keeping [system, recent q, recent a], got %+v", msgs)
	}
}

// TestL1_EvictBoundary_ZeroDisablesAndSeqThreshold pins applyL1 (context.go:149,
// 156, 159):
//   - evictAfter == 0 disables L1 even when an OLD tool turn exists (kills
//     `<=`->`<` on the evictAfter guard: with `<0`, 0 would NOT disable L1).
//   - the seq==1 system turn is never evicted even if it is (pathologically) a
//     tool role (kills `false || role!=tool`).
//   - a non-tool old turn is never rewritten (kills `seq==1 || false`).
//   - a tool turn EXACTLY at the threshold is NOT evicted (kills `<`->`<=`).
func TestL1_EvictBoundary_ZeroDisablesAndSeqThreshold(t *testing.T) {
	// (a) evictAfter==0 disables L1: an old tool turn (seq 2, far below maxSeq 20)
	// must be left intact.
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleTool, Content: "old tool out", ToolCallID: "c2"},
		{Seq: 20, Role: llm.RoleUser, Content: "now"},
	}
	got := applyL1(turns, 0)
	if got[1].Content != "old tool out" {
		t.Fatalf("evictAfter==0 must disable L1 (old tool turn untouched), got %q", got[1].Content)
	}

	// (b) seq==1 tool-role turn is protected; (c) a non-tool old turn is untouched;
	// (d) a tool turn exactly AT the threshold is kept, one strictly below is evicted.
	// maxSeq=30, evictAfter=10 -> threshold=20: seq<20 evicted, seq==20 kept.
	turns2 := []Turn{
		{Seq: 1, Role: llm.RoleTool, Content: "system-pos tool", ToolCallID: "c1"}, // protected by seq==1
		{Seq: 5, Role: llm.RoleAssistant, Content: "old non-tool"},                 // protected by role!=tool
		{Seq: 19, Role: llm.RoleTool, Content: "below threshold", ToolCallID: "c19", ContentSidecarPath: "/run/sidecar/c19"},
		{Seq: 20, Role: llm.RoleTool, Content: "at threshold", ToolCallID: "c20", ContentSidecarPath: "/run/sidecar/c20"},
		{Seq: 30, Role: llm.RoleUser, Content: "newest"},
	}
	g := applyL1(turns2, 10)
	if g[0].Content != "system-pos tool" {
		t.Errorf("seq==1 must NEVER be evicted even as a tool turn, got %q", g[0].Content)
	}
	if g[1].Content != "old non-tool" {
		t.Errorf("a non-tool old turn must NOT be evicted, got %q", g[1].Content)
	}
	if !strings.Contains(g[2].Content, "read_tool_output(tool_call_id=\"c19\")") {
		t.Errorf("a tool turn strictly below the threshold (seq 19 < 20) must be evicted, got %q", g[2].Content)
	}
	if g[3].Content != "at threshold" {
		t.Errorf("a tool turn EXACTLY at the threshold (seq 20) must be kept, got %q", g[3].Content)
	}
}

// TestL1_EvictClearsSidecarPath pins context.go:161 — an evicted tool turn has its
// ContentSidecarPath cleared (the content is now an inline pointer, the sidecar
// reference must not linger). Removing the assignment (the survivor) leaves the
// stale path, which this asserts gone.
func TestL1_EvictClearsSidecarPath(t *testing.T) {
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleTool, Content: "old", ToolCallID: "c2", ContentSidecarPath: "/run/sidecar/c2"},
		{Seq: 20, Role: llm.RoleUser, Content: "now"},
	}
	got := applyL1(turns, 5) // threshold=15, seq 2 evicted
	if !strings.Contains(got[1].Content, "read_tool_output") {
		t.Fatalf("seq 2 tool turn must be evicted, got %q", got[1].Content)
	}
	if got[1].ContentSidecarPath != "" {
		t.Fatalf("an evicted turn must clear its sidecar path, got %q", got[1].ContentSidecarPath)
	}
}

// TestDropOldestPairs_SystemDetectionAndLoop pins dropOldestPairs (context.go:184,
// 191, 193):
//   - with a leading seq==1 system turn, the system is preserved and only body
//     pairs are dropped (kills the system-at-head `&&` flips / true-stubs).
//   - the loop drops down to the last droppable pair (kills `>= 2`->`> 2`).
//   - the loop stops as soon as the history fits at-or-below hardCap (kills
//     `<=`->`<`).
func TestDropOldestPairs_SystemDetectionAndLoop(t *testing.T) {
	enc := mustEncoderRaw(t)
	big := strings.Repeat("word ", 300)
	// system + 3 user/assistant pairs of big text + a final small pair.
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM"},
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: big},
		{Seq: 5, Role: llm.RoleAssistant, Content: big},
		{Seq: 6, Role: llm.RoleUser, Content: "keep q"},
		{Seq: 7, Role: llm.RoleAssistant, Content: "keep a"},
	}
	// hardCap large enough only for [system, keep q, keep a].
	hardCap := totalTokens(enc, []Turn{turns[0], turns[5], turns[6]}) + 5

	reduced, dropped := dropOldestPairs(enc, turns, hardCap)
	if dropped != 2 {
		t.Fatalf("must drop exactly the 2 oldest big pairs, dropped %d", dropped)
	}
	if reduced[0].Content != "SYSTEM" {
		t.Fatalf("the leading system turn must be preserved, got %q", reduced[0].Content)
	}
	if len(reduced) != 3 || reduced[1].Content != "keep q" || reduced[2].Content != "keep a" {
		t.Fatalf("must keep [system, keep q, keep a], got %+v", reduced)
	}
	if got := totalTokens(enc, reduced); got > hardCap {
		t.Fatalf("loop must stop at/below hardCap (%d), got %d tokens", hardCap, got)
	}

	// No leading system turn: the whole slice is body; the first pair is droppable.
	noSys := []Turn{
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: "tail q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "tail a"},
	}
	hc2 := totalTokens(enc, []Turn{noSys[2], noSys[3]}) + 5
	red2, drop2 := dropOldestPairs(enc, noSys, hc2)
	if drop2 != 1 || len(red2) != 2 || red2[0].Content != "tail q" {
		t.Fatalf("without a system head the oldest body pair must drop, got dropped=%d reduced=%+v", drop2, red2)
	}
}

// TestDropOldestPairs_ToolRoundBoundary pins the H8/D-04 false-green: fixed
// two-turn slicing can stop with a RoleTool at the body head after cutting into
// assistant(tool_calls)->tool->tool->assistant. The boundary-aware drop must
// remove the old tool round whole while preserving any kept tool round intact.
func TestDropOldestPairs_ToolRoundBoundary(t *testing.T) {
	enc := mustEncoderRaw(t)
	bigTool := strings.Repeat("word ", 300)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "old tool calls", ToolCalls: toolCallsJSON(t, "call_old_1", "call_old_2")},
		{Seq: 3, Role: llm.RoleTool, Content: bigTool, ToolCallID: "call_old_1"},
		{Seq: 4, Role: llm.RoleTool, Content: "old second result", ToolCallID: "call_old_2"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "old synthesis"},
		{Seq: 6, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 7, Role: llm.RoleAssistant, Content: "recent tool call", ToolCalls: toolCallsJSON(t, "call_recent")},
		{Seq: 8, Role: llm.RoleTool, Content: "recent result", ToolCallID: "call_recent"},
		{Seq: 9, Role: llm.RoleAssistant, Content: "recent answer"},
	}
	oldStrideReduced := []Turn{turns[0], turns[3], turns[4], turns[5], turns[6], turns[7], turns[8]}
	hardCap := totalTokens(enc, oldStrideReduced) + 5
	if totalTokens(enc, oldStrideReduced) > hardCap || hardCap >= totalTokens(enc, turns) {
		t.Fatalf("test setup must make the old 2-stride stop with a RoleTool head")
	}
	oldStrideMsgs := toMessages(oldStrideReduced)
	if oldStrideMsgs[1].Role != llm.RoleTool {
		t.Fatalf("test setup must model the pre-fix orphan RoleTool head, got %+v", oldStrideMsgs)
	}

	reduced, dropped := dropOldestPairs(enc, turns, hardCap)
	if dropped != 1 {
		t.Fatalf("boundary-aware drop must count the old tool round as one dropped round, got %d", dropped)
	}
	msgs := toMessages(reduced)
	if len(msgs) != 5 || msgs[0].Role != llm.RoleSystem || msgs[1].Role != llm.RoleUser {
		t.Fatalf("must keep [system, recent user, recent tool round], got %+v", msgs)
	}
	if msgs[1].Content != "recent q" {
		t.Fatalf("old tool round must be removed whole, first body content=%q", msgs[1].Content)
	}
	for _, msg := range msgs {
		if msg.ToolCallID == "call_old_1" || msg.ToolCallID == "call_old_2" {
			t.Fatalf("old tool results must not survive partially, got %+v", msgs)
		}
	}
	assertToolRoundsIntact(t, msgs)
	if msgs[3].Role != llm.RoleTool || msgs[3].ToolCallID != "call_recent" {
		t.Fatalf("kept tool round must retain assistant(tool_calls) before tool result, got %+v", msgs)
	}
}

// TestApplyL1_EmptyTurnsNoPanic pins applyL1's `len(out) == 0` guard
// (context.go:149). With evictAfter>0 and ZERO turns, the empty-slice short-circuit
// must return early; removing it (the `|| false` survivor) indexes out[-1] and
// panics. A no-panic empty return kills it.
func TestApplyL1_EmptyTurnsNoPanic(t *testing.T) {
	got := applyL1(nil, 10)
	if len(got) != 0 {
		t.Fatalf("applyL1 on no turns must return empty, got %d", len(got))
	}
}

// TestDropOldestPairs_EmptyAndSystemArms pins dropOldestPairs system-at-head
// detection (context.go:184) on its precise arms:
//   - empty turns: the `len(turns) > 0` guard must short-circuit (kills `>= 0`,
//     which would index turns[0] on an empty slice and panic).
//   - a turn that is RoleSystem but NOT seq==1 must NOT be treated as the
//     protected head (kills `turns[0].Seq == 1` -> true): it is part of the
//     droppable body.
func TestDropOldestPairs_EmptyAndSystemArms(t *testing.T) {
	enc := mustEncoderRaw(t)

	reduced, dropped := dropOldestPairs(enc, nil, 100)
	if len(reduced) != 0 || dropped != 0 {
		t.Fatalf("dropOldestPairs on no turns must be a no-op, got reduced=%d dropped=%d", len(reduced), dropped)
	}

	// turns[0] is RoleSystem but Seq=2 (NOT 1): it is body, not the protected head.
	big := strings.Repeat("word ", 300)
	turns := []Turn{
		{Seq: 2, Role: llm.RoleSystem, Content: big}, // system role, but seq!=1 -> body
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: "tail q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "tail a"},
	}
	hc := totalTokens(enc, []Turn{turns[2], turns[3]}) + 5
	red, drop := dropOldestPairs(enc, turns, hc)
	// With seq!=1, start=0: the first (big) pair drops, leaving the tail pair.
	if drop != 1 || len(red) != 2 || red[0].Content != "tail q" {
		t.Fatalf("a RoleSystem-but-not-seq1 turn must be droppable body, got dropped=%d reduced=%+v", drop, red)
	}
}

// TestDropOldestPairs_OnlyActiveRoundIsNeverDropped is the CTX-002 regression:
// a resumed/current round is required provider state, not historical compaction
// material. If it alone exceeds the cap, the caller must fail explicitly.
func TestDropOldestPairs_OnlyActiveRoundIsNeverDropped(t *testing.T) {
	enc := mustEncoderRaw(t)
	big := strings.Repeat("word ", 300)
	body := []Turn{
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
	}
	hc := totalTokens(enc, body) - 100
	red, drop := dropOldestPairs(enc, body, hc)
	if drop != 0 {
		t.Fatalf("the active round was counted as dropped: %d", drop)
	}
	if len(red) != len(body) || red[0].Content != body[0].Content || red[1].Content != body[1].Content {
		t.Fatalf("the active round changed: got=%+v want=%+v", red, body)
	}

	emit := &fakeRotEmitter{}
	if _, err := applyContextLadder(context.Background(), "conv", body, windowFor(hc), enc, emit); !errors.Is(err, ErrContextWindowExceeded) {
		t.Fatalf("oversized active round error = %v, want ErrContextWindowExceeded", err)
	}
	if len(emit.calls) != 0 {
		t.Fatalf("failed active-round reduction emitted %d rot events", len(emit.calls))
	}
}

// TestLadder_UnreducibleAfterDrop_StillErrors pins the second arm of context.go:130
// (`tokensAfter > hardCap`). When L2.5 drops every droppable pair but the surviving
// system + recent pair STILL exceed the cap, the ladder must return
// ErrContextWindowExceeded and write NO rot event. The `tokensAfter > hardCap` ->
// false survivor would instead emit a rot event and return an over-budget history.
func TestLadder_UnreducibleAfterDrop_StillErrors(t *testing.T) {
	enc := mustEncoderRaw(t)
	hugeSystem := strings.Repeat("word ", 4000) // the undroppable system turn alone blows the cap
	pair := strings.Repeat("word ", 200)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: hugeSystem},
		{Seq: 2, Role: llm.RoleUser, Content: pair},
		{Seq: 3, Role: llm.RoleAssistant, Content: pair},
		{Seq: 4, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "recent a"},
	}
	// hardCap below the (undroppable) system turn: L2.5 will drop EVERY body pair
	// (pairsDropped>0) yet the surviving system turn still exceeds the cap.
	systemOnly := totalTokens(enc, turns[:1])
	full := totalTokens(enc, turns)
	hardCap := systemOnly - 100 // even system-only is over -> unreducible
	if hardCap >= full {
		t.Fatalf("setup: need hardCap(%d) < full(%d)", hardCap, full)
	}

	emit := &fakeRotEmitter{}
	_, err := applyContextLadder(context.Background(), "conv", turns, windowFor(hardCap), enc, emit)
	if err == nil || !strings.Contains(err.Error(), "aura chat new") {
		t.Fatalf("a still-over-cap history after dropping all droppable pairs must error, got %v", err)
	}
	if len(emit.calls) != 0 {
		t.Fatalf("an unreducible history must NOT write a rot event, got %d", len(emit.calls))
	}
}

// errRotEmitter fails the L2.5 rot-event write, so the ladder's
// `if err := emit.insertContextRotEvent(...); err != nil { return nil, err }`
// propagation (context.go:135) is exercised.
type errRotEmitter struct{ err error }

func (e errRotEmitter) insertContextRotEvent(context.Context, string, int, int, int) error {
	return e.err
}

// TestLadder_RotEmitError_Propagates pins context.go:135 — a failing rot-event
// write must abort the ladder with that error. The `return nil, err` survivor
// (replaced by `_ = err`) would swallow it and return a (silently un-audited)
// reduced history; asserting the error surfaces kills it.
func TestLadder_RotEmitError_Propagates(t *testing.T) {
	enc := mustEncoderRaw(t)
	big := strings.Repeat("word ", 600)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "recent a"},
	}
	hardCap := totalTokens(enc, []Turn{turns[0], turns[3], turns[4]}) + 5 // one drop succeeds
	boom := errRotEmitter{err: context.DeadlineExceeded}

	msgs, err := applyContextLadder(context.Background(), "conv", turns, windowFor(hardCap), enc, boom)
	if err == nil {
		t.Fatalf("a failing rot-event write must abort the ladder, got nil error (msgs=%d)", len(msgs))
	}
	if msgs != nil {
		t.Fatalf("on a rot-event write failure the ladder must return nil messages, got %d", len(msgs))
	}
}

// TestDropOldestPairs_Seq1NonSystemIsBody pins the `turns[0].Role == llm.RoleSystem`
// arm of context.go:184. A turn with Seq==1 but a NON-system role must NOT be
// treated as the protected head (it is droppable body). The `&& true` survivor
// (dropping the role check) would wrongly protect it. We assert that such a turn
// participates in the body drop.
func TestDropOldestPairs_Seq1NonSystemIsBody(t *testing.T) {
	enc := mustEncoderRaw(t)
	big := strings.Repeat("word ", 300)
	// turns[0]: Seq==1 but RoleUser (not system) -> must be droppable body.
	turns := []Turn{
		{Seq: 1, Role: llm.RoleUser, Content: big},
		{Seq: 2, Role: llm.RoleAssistant, Content: big},
		{Seq: 3, Role: llm.RoleUser, Content: "tail q"},
		{Seq: 4, Role: llm.RoleAssistant, Content: "tail a"},
	}
	hc := totalTokens(enc, []Turn{turns[2], turns[3]}) + 5
	red, drop := dropOldestPairs(enc, turns, hc)
	if drop != 1 || len(red) != 2 || red[0].Content != "tail q" {
		t.Fatalf("a seq==1 NON-system turn must be droppable body (no head protection), got dropped=%d reduced=%+v", drop, red)
	}
}

// TestTotalTokens_CountsToolCallID pins context.go:210 — totalTokens must include
// the ToolCallID token cost. Removing that term (the survivor) would undercount.
// A turn with ONLY a (multi-token) ToolCallID must produce a strictly positive
// count equal to the ToolCallID's own token count.
func TestTotalTokens_CountsToolCallID(t *testing.T) {
	enc := mustEncoderRaw(t)
	const id = "call_abcdef_0123456789_xyz"
	turns := []Turn{{Seq: 2, Role: llm.RoleTool, ToolCallID: id}}

	got := totalTokens(enc, turns)
	want := countTokens(enc, id)
	if want == 0 {
		t.Fatalf("test setup: ToolCallID must tokenize to >0, got %d", want)
	}
	if got != want {
		t.Fatalf("totalTokens must count the ToolCallID (%d tokens), got %d", want, got)
	}
}
