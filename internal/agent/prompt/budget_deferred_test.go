package prompt

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestBudgetDeferredToolsTailInject mirrors TestBudgetSourcesTailInject, because the
// deferred-tool roster is the same KIND of thing as the numbered source list: a
// volatile block that must ride the trailing hint and never messages[0].
//
// The reason it is not merely tidy is measured. The roster used to be concatenated
// into tool_search's Description, which ships in the cached manifest — so the model
// read "load a deferred tool before calling it", with the full list, at token ~0 of
// a 5.8k-token prefix, and then decided which tool to call thousands of tokens
// later. On a live turn 2026-08-03 it called shell_exec unloaded anyway and spent a
// 2.6s round trip on the refusal. Position was the defect, not wording.
func TestBudgetDeferredToolsTailInject(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	hist := seedHistory()
	histLenBefore := len(hist)

	roster := "- built-in: fs_read, shell_exec\n- memory: memory__recall"
	withRoster := b.Build(hist, reg, "openrouter", cfg, Budget{DeferredTools: roster}, nil)
	none := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)

	if len(hist) != histLenBefore {
		t.Fatalf("Build mutated the caller history slice: len %d -> %d", histLenBefore, len(hist))
	}

	hRoster, err := PrefixHash(withRoster.Messages, []int{0})
	if err != nil {
		t.Fatalf("hash with roster: %v", err)
	}
	hNone, err := PrefixHash(none.Messages, []int{0})
	if err != nil {
		t.Fatalf("hash no roster: %v", err)
	}
	if hRoster != hNone {
		t.Fatalf("messages[0] drifted when DeferredTools was set: %s vs %s", hRoster, hNone)
	}
	if !reflect.DeepEqual(withRoster.Messages[0], none.Messages[0]) {
		t.Fatalf("messages[0] not byte-identical with DeferredTools set:\n got=%+v\nwant=%+v",
			withRoster.Messages[0], none.Messages[0])
	}

	if len(none.Messages) != histLenBefore {
		t.Fatalf("empty Budget appended a trailing message: got %d want %d", len(none.Messages), histLenBefore)
	}
	if len(withRoster.Messages) != histLenBefore+1 {
		t.Fatalf("DeferredTools build did not append exactly one hint: got %d want %d",
			len(withRoster.Messages), histLenBefore+1)
	}
	tail := withRoster.Messages[len(withRoster.Messages)-1]
	if tail.Role != llm.RoleUser {
		t.Fatalf("trailing hint role = %q, want user", tail.Role)
	}
	if !strings.Contains(tail.Content, "<deferred_tools>") || !strings.Contains(tail.Content, roster) {
		t.Fatalf("trailing hint missing the roster, got: %q", tail.Content)
	}
	if strings.Contains(withRoster.Messages[0].Content, roster) {
		t.Fatalf("volatile roster leaked into messages[0]: %q", withRoster.Messages[0].Content)
	}
}

// TestBudgetDeferredToolsRubric pins the three things the block must carry — they
// are the three Claude Code's own deferred-tool reminder carries, and each one is
// load-bearing on its own:
//
//	the consequence  — without it the model treats a direct call as worth a try;
//	the plural form  — "select:<name>[,<name>...]" is what makes ONE search load a
//	                   whole turn's worth of tools instead of one per round trip;
//	the roster last  — nearest the decision it governs.
func TestBudgetDeferredToolsRubric(t *testing.T) {
	t.Parallel()
	if !(Budget{DeferredTools: "- built-in: fs_read"}).present() {
		t.Fatal("Budget with DeferredTools should be present()")
	}
	if got := (Budget{}).block(); got != "" {
		t.Fatalf("zero Budget block() should be empty, got %q", got)
	}

	block := (Budget{DeferredTools: "- built-in: fs_read"}).block()
	for what, want := range map[string]string{
		"the refusal consequence":   "the call is refused",
		"the batched select syntax": "select:<name>[,<name>...]",
		"the per-tool round trip":   "one round trip per tool",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block() dropped %s (%q):\n%s", what, want, block)
		}
	}
	if i, j := strings.Index(block, "Schemas NOT loaded"), strings.Index(block, "- built-in: fs_read"); i < 0 || j < i {
		t.Errorf("the rubric must precede the roster it explains:\n%s", block)
	}

	// A block with other hints but no roster renders no <deferred_tools> at all —
	// an empty roster means everything is loaded, which is nothing to say.
	if other := (Budget{CurrentTime: "2026-08-03T00:00:00Z"}).block(); strings.Contains(other, "<deferred_tools>") {
		t.Errorf("empty DeferredTools still rendered a block: %q", other)
	}

	// The roster is LAST in the block: closest to the turn it governs.
	full := Budget{Used: 1, Remaining: 2, Workspace: "/w", CurrentTime: "t", Today: "d",
		Sources: "[1] x", DeferredTools: "- built-in: fs_read"}.block()
	if !strings.HasSuffix(full, "</deferred_tools>") {
		t.Errorf("the roster must be the last hint in the block:\n%s", full)
	}
}
