package prompt

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestBudgetSourcesTailInject (DISP-01/05): a non-empty Budget.Sources causes Build
// to append a trailing user-role message carrying the numbered source list to a COPY
// of history; messages[0] is byte-identical to the no-Sources build, and the caller's
// history slice is never mutated.
func TestBudgetSourcesTailInject(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	hist := seedHistory()
	histLenBefore := len(hist)

	srcList := "[1] Forecast — https://example.com/a\n[2] Map — https://example.com/b"
	withSrc := b.Build(hist, reg, "openrouter", cfg, Budget{Sources: srcList})
	noSrc := b.Build(hist, reg, "openrouter", cfg, Budget{})

	// The caller's slice is never mutated by the append-to-copy path.
	if len(hist) != histLenBefore {
		t.Fatalf("Build mutated the caller history slice: len %d -> %d", histLenBefore, len(hist))
	}

	// messages[0] is byte-identical between the source build and the no-source build.
	h0Src, err := PrefixHash(withSrc.Messages, []int{0})
	if err != nil {
		t.Fatalf("hash with sources: %v", err)
	}
	h0None, err := PrefixHash(noSrc.Messages, []int{0})
	if err != nil {
		t.Fatalf("hash no sources: %v", err)
	}
	if h0Src != h0None {
		t.Fatalf("messages[0] drifted when Sources was set: %s vs %s", h0Src, h0None)
	}
	if !reflect.DeepEqual(withSrc.Messages[0], noSrc.Messages[0]) {
		t.Fatalf("messages[0] not byte-identical with Sources set:\n src=%+v\nnone=%+v", withSrc.Messages[0], noSrc.Messages[0])
	}

	// The source build appends exactly one trailing user-role hint message; the no-
	// source build appends none (backward-compatible omit).
	if len(noSrc.Messages) != histLenBefore {
		t.Fatalf("empty Budget appended a trailing message: got %d want %d", len(noSrc.Messages), histLenBefore)
	}
	if len(withSrc.Messages) != histLenBefore+1 {
		t.Fatalf("Sources build did not append exactly one hint: got %d want %d", len(withSrc.Messages), histLenBefore+1)
	}
	tail := withSrc.Messages[len(withSrc.Messages)-1]
	if tail.Role != llm.RoleUser {
		t.Fatalf("trailing hint role = %q, want user", tail.Role)
	}
	if !strings.Contains(tail.Content, "<sources>") || !strings.Contains(tail.Content, srcList) {
		t.Fatalf("trailing hint missing the numbered source list, got: %q", tail.Content)
	}
	// The volatile list rides the TAIL, never messages[0].
	if strings.Contains(withSrc.Messages[0].Content, srcList) {
		t.Fatalf("volatile source list leaked into messages[0]: %q", withSrc.Messages[0].Content)
	}
}

// TestBudgetSourcesPresentAndOmit: a non-empty Sources makes present() true (the
// hint is injected); an empty Sources omits the line and a zero Budget emits no
// trailing message at all (backward-compatible).
func TestBudgetSourcesPresentAndOmit(t *testing.T) {
	t.Parallel()
	if !(Budget{Sources: "[1] x — https://x.test"}).present() {
		t.Fatalf("Budget with Sources should be present()")
	}
	if (Budget{}).present() {
		t.Fatalf("zero Budget must omit the trailing hint")
	}
	if got := (Budget{}).block(); got != "" {
		t.Fatalf("zero Budget block() should be empty, got %q", got)
	}
	block := (Budget{Sources: "[1] x — https://x.test"}).block()
	if !strings.Contains(block, "<sources>") || !strings.Contains(block, "[1] x — https://x.test") {
		t.Fatalf("Sources block() missing the wrapped numbered list, got %q", block)
	}
	// The source block must NOT be emitted when Sources is empty even if other hints are.
	noSrcBlock := (Budget{CurrentTime: "2026-06-18T00:00:00Z"}).block()
	if strings.Contains(noSrcBlock, "<sources>") {
		t.Fatalf("empty Sources still rendered a <sources> line: %q", noSrcBlock)
	}
}
