// Small-window hard-cap floor coverage for finding M-03. The pre-M-03 hardCap()
// clamped a non-positive SPEC-Req#10 formula to 0, and applyContextLadder then
// returned RAW history with L2/L2.5 protection entirely off — a bug on any model
// whose window is below the ~33k fixed reservation (e.g. a Slice 13 local vLLM).
// These tests pin the nanobot-style floor (formula <= 0 -> ContextWindow/2) so
// protection stays active. Shares fakeRotEmitter/mustEncoderRaw from
// context_unit_test.go.
package conversations

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestLadder_SmallWindowFloor_ProtectsNotRaw pins the M-03 fix: a small-window
// config whose SPEC formula (ContextWindow - max(MaxOutputTokens,20000) - 13000)
// is <= 0 must NOT disable protection. hardCap() must floor to a positive
// ContextWindow/2, and an over-floor history must be truncated by L2.5 (a rot
// event written, fewer turns returned) rather than returned raw.
func TestLadder_SmallWindowFloor_ProtectsNotRaw(t *testing.T) {
	enc := mustEncoderRaw(t)
	// 16000-token window: formula = 16000 - 20000 - 13000 = -17000 (<= 0) -> the
	// pre-M-03 code clamped to 0 and returned raw history. Floor = 16000/2 = 8000.
	const window = 16000
	cfg := ContextConfig{ContextWindow: window, MaxOutputTokens: 1, ToolEvictAfterTurns: 1_000_000}
	if got, want := cfg.hardCap(), window/2; got != want {
		t.Fatalf("a small-window formula <=0 must floor hardCap to ContextWindow/2 (%d), got %d", want, got)
	}

	// Build a history comfortably above the 8000-token floor across several
	// droppable pairs so L2.5 has room to reduce it under the floor.
	big := strings.Repeat("word ", 2500)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: big},
		{Seq: 5, Role: llm.RoleAssistant, Content: big},
		{Seq: 6, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 7, Role: llm.RoleAssistant, Content: "recent a"},
	}
	if totalTokens(enc, turns) <= cfg.hardCap() {
		t.Fatalf("test setup: history must exceed the floor (%d) to exercise L2.5", cfg.hardCap())
	}

	emit := &fakeRotEmitter{}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("small-window L2.5 reduction must succeed, got %v", err)
	}
	if len(emit.calls) == 0 {
		t.Fatalf("a small-window over-floor history must be truncated by L2.5 (rot event), got none — protection is OFF (M-03 regression)")
	}
	if len(msgs) >= len(turns) {
		t.Fatalf("L2.5 must drop turns under the floor; got %d msgs from %d turns (raw history returned — M-03 regression)", len(msgs), len(turns))
	}
	if got := totalTokens(enc, turnsFromMessages(msgs)); got > cfg.hardCap() {
		t.Fatalf("reduced history must fit the floor (%d), got %d tokens", cfg.hardCap(), got)
	}
}

// TestSmallWindowHardCapFloor pins the helper directly: a positive window floors
// to window/2; a degenerate window <= 0 yields 0 (the only remaining hardCap==0
// path). Kills `window <= 0` -> `window < 0` (0 would then divide to 0 anyway, so
// the arm is asserted at the boundary) and `window / 2` -> other arithmetic.
func TestSmallWindowHardCapFloor(t *testing.T) {
	cases := []struct {
		window int
		want   int
	}{
		{window: 16000, want: 8000},
		{window: 1, want: 0}, // 1/2 == 0: a 1-token window has no usable budget
		{window: 2, want: 1},
		{window: 0, want: 0},
		{window: -100, want: 0},
	}
	for _, tc := range cases {
		if got := smallWindowHardCapFloor(tc.window); got != tc.want {
			t.Errorf("smallWindowHardCapFloor(%d) = %d, want %d", tc.window, got, tc.want)
		}
	}
}

// turnsFromMessages is a thin re-projection for token re-measurement in the M-03
// floor test (the ladder returns llm.Message; totalTokens consumes Turn).
func turnsFromMessages(msgs []llm.Message) []Turn {
	out := make([]Turn, len(msgs))
	for i, m := range msgs {
		out[i] = Turn{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
	}
	return out
}
