package prompt

import (
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestCacheAuditSourceListMessages0Stable (DISP-01) is the prompt-builder-level twin
// of the scripts/cache_invariant_audit.sh source-list fixture (turn-08): it proves the
// invariant the runtime gate replays end-to-end — across a sequence of turns where the
// tail-injected numbered source list GROWS every turn (and the history grows too), the
// messages[0] hash is byte-identical at every turn. A regression that routed the
// volatile list into messages[0] would diverge here exactly as `aura cache-audit` would
// print "messages[0] mutated at request N".
func TestCacheAuditSourceListMessages0Stable(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	hist := seedHistory()
	var want string
	for turn := 1; turn <= 5; turn++ {
		// Each turn the registry accumulated one more source — the volatile list grows.
		sources := ""
		for n := 1; n <= turn; n++ {
			sources += fmt.Sprintf("[%d] Source %d — https://src.test/%d\n", n, n, n)
		}
		budget := Budget{
			Used:        turn,
			Remaining:   25 - turn,
			CurrentTime: "2026-06-18T12:00:00Z",
			Today:       "2026-06-18",
			Sources:     sources,
		}
		req := b.Build(hist, reg, "openrouter", cfg, budget)
		h0, err := PrefixHash(req.Messages, []int{0})
		if err != nil {
			t.Fatalf("turn %d hash: %v", turn, err)
		}
		if turn == 1 {
			want = h0
		} else if h0 != want {
			t.Fatalf("messages[0] mutated at turn %d -- diff: %s vs %s", turn, want, h0)
		}
		// Grow history like a real turn (assistant + tool result) so the test mirrors
		// the audit's growing-history replay, not a fixed-length prefix.
		hist = append(hist,
			llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("turn %d", turn)},
			llm.Message{Role: llm.RoleTool, ToolCallID: fmt.Sprintf("c%d", turn), Content: "result"},
		)
	}
}
