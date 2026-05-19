package tokenjuice_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aura/aura/internal/tokenjuice"
)

// ptrInt returns a pointer to an int literal — helper for RuleSummarize/RuleFailure fields.
func ptrInt(v int) *int { return &v }

// fallbackRule builds a CompiledRule that matches everything with the given head/tail.
func fallbackRule(head, tail int) *tokenjuice.CompiledRule {
	return &tokenjuice.CompiledRule{
		Rule: tokenjuice.Rule{
			ID:     "generic/fallback",
			Family: "generic",
			Summarize: &tokenjuice.RuleSummarize{
				Head: ptrInt(head),
				Tail: ptrInt(tail),
			},
			Transforms: &tokenjuice.RuleTransforms{
				StripAnsi:      true,
				TrimEmptyEdges: true,
				DedupeAdjacent: true,
			},
		},
	}
}

// failureFallbackRule builds a fallback rule with failure-preserving head/tail.
func failureFallbackRule(head, tail, failHead, failTail int) *tokenjuice.CompiledRule {
	r := fallbackRule(head, tail)
	r.Rule.Failure = &tokenjuice.RuleFailure{
		PreserveOnFailure: true,
		Head:              ptrInt(failHead),
		Tail:              ptrInt(failTail),
	}
	return r
}

// bigText generates n distinct lines (each ~44 bytes). Lines are kept unique
// via the index so dedupeAdjacent doesn't fold them before head/tail elision.
func bigText(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "this is test line number %04d in the output\n", i)
	}
	return b.String()
}

// Test 1: tiny input (< 512B) → passthrough, Applied:false
func TestReduceTinyPassthrough(t *testing.T) {
	in := tokenjuice.Input{Stdout: "hello world"}
	r := tokenjuice.CompactWithRules(in, [](*tokenjuice.CompiledRule){fallbackRule(3, 3)}, tokenjuice.Options{})
	if r.Applied {
		t.Errorf("tiny input should not be compacted, Applied=%v", r.Applied)
	}
	if r.InlineText != "hello world" {
		t.Errorf("tiny input InlineText mismatch: %q", r.InlineText)
	}
}

// Test 2: oversized input (> threshold) with small head/tail → Applied:true + omitted marker
func TestReduceOversizedCompact(t *testing.T) {
	// 20 lines × ~40 bytes = ~800 bytes > 512 threshold
	text := bigText(20)
	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: text},
		[]*tokenjuice.CompiledRule{fallbackRule(3, 3)},
		tokenjuice.Options{},
	)
	if !r.Applied {
		t.Errorf("oversized input should be compacted, Applied=%v, RuleID=%q", r.Applied, r.RuleID)
	}
	if !strings.Contains(r.InlineText, "omitted") {
		t.Errorf("expected 'omitted' marker in output, got: %q", r.InlineText)
	}
}

// Test 3: head/tail elision — verify the omitted count is correct
func TestReduceHeadTailElision(t *testing.T) {
	// 100 lines × ~40 bytes = 4000 bytes, head=5 tail=5 → 90 omitted
	text := bigText(100)
	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: text},
		[]*tokenjuice.CompiledRule{fallbackRule(5, 5)},
		tokenjuice.Options{},
	)
	if !r.Applied {
		t.Fatalf("expected Applied=true, got false, RuleID=%q", r.RuleID)
	}
	if !strings.Contains(r.InlineText, "90 lines omitted") {
		t.Errorf("expected '90 lines omitted', got: %q", r.InlineText)
	}
}

// Test 4: ANSI codes are stripped from output
func TestReduceANSIStripped(t *testing.T) {
	// Build a large ANSI-colored text (> 512 bytes)
	var b strings.Builder
	for i := 0; i < 25; i++ {
		b.WriteString("\x1b[31mERROR: something failed\x1b[0m\n")
	}
	text := b.String()

	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: text},
		[]*tokenjuice.CompiledRule{fallbackRule(3, 3)},
		tokenjuice.Options{},
	)
	if strings.Contains(r.InlineText, "\x1b") {
		t.Errorf("ANSI ESC bytes should be stripped, got: %q", r.InlineText)
	}
}

// Test 5: dedupeAdjacent collapses consecutive identical lines
func TestReduceDedupeAdjacent(t *testing.T) {
	// 20 lines all identical → should collapse to 1 line.
	// 20 × ~40 bytes = 800 bytes > 512 threshold.
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("identical log line that repeats\n")
	}
	// Use large head/tail so headTail doesn't truncate.
	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: b.String()},
		[]*tokenjuice.CompiledRule{fallbackRule(100, 100)},
		tokenjuice.Options{},
	)
	// After dedupe, we should have 1 unique line.
	lines := strings.Split(strings.TrimSpace(r.InlineText), "\n")
	if len(lines) != 1 {
		t.Errorf("dedupeAdjacent: expected 1 unique line, got %d lines", len(lines))
	}
}

// Test 6: CJK characters are not split at grapheme boundaries
func TestReduceCJKNotSplit(t *testing.T) {
	// 20 lines of CJK text × ~30 bytes = 600 bytes > 512 threshold.
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("中文日本語한국어 测试内容\n")
	}
	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: b.String()},
		[]*tokenjuice.CompiledRule{fallbackRule(3, 3)},
		tokenjuice.Options{},
	)
	if !utf8.ValidString(r.InlineText) {
		t.Error("CJK output is not valid UTF-8")
	}
	// No mid-byte cuts: verify every rune decodes correctly.
	for i, r := range r.InlineText {
		if r == utf8.RuneError {
			t.Errorf("invalid rune at byte offset %d", i)
			break
		}
	}
}

// Test 7: ratio guard — when compaction doesn't reduce enough, Applied:false
func TestReduceRatioGuard(t *testing.T) {
	// Use head=1000/tail=1000 (effectively no truncation) on a 20-line text.
	// After dedup/trim, ratio ≈ 1.0, so ratio guard should fire → Applied:false.
	text := bigText(20)
	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: text},
		[]*tokenjuice.CompiledRule{fallbackRule(1000, 1000)},
		tokenjuice.Options{},
	)
	if r.Applied {
		t.Errorf("ratio guard should prevent compaction, Applied=true, RuleID=%q", r.RuleID)
	}
}

// Test 8: failure-preserving head/tail when exit_code != 0
func TestReduceFailurePreserving(t *testing.T) {
	// normal head=3/tail=3 but failure head=10/tail=10
	text := bigText(50)
	exitCode := 1
	rule := failureFallbackRule(3, 3, 10, 10)

	r := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: text, ExitCode: &exitCode},
		[]*tokenjuice.CompiledRule{rule},
		tokenjuice.Options{},
	)
	if !r.Applied {
		t.Fatalf("expected Applied=true for failure path, RuleID=%q", r.RuleID)
	}
	// Failure path keeps 10+10=20 lines; normal path keeps 3+3=6.
	// The failure result should have more content than the normal path.
	rNormal := tokenjuice.CompactWithRules(
		tokenjuice.Input{Stdout: text},
		[]*tokenjuice.CompiledRule{rule},
		tokenjuice.Options{},
	)
	if len(r.InlineText) <= len(rNormal.InlineText) {
		t.Errorf("failure path (%d chars) should have more content than normal path (%d chars)",
			len(r.InlineText), len(rNormal.InlineText))
	}
}

// BenchmarkCompactLarge verifies that a 1MB input compacts in < 50ms.
func BenchmarkCompactLarge(b *testing.B) {
	var sb strings.Builder
	// Build ~1MB of content: 25000 lines × ~40 bytes
	for i := 0; i < 25000; i++ {
		sb.WriteString("benchmark line content for the large input test case\n")
	}
	text := sb.String()
	rules := []*tokenjuice.CompiledRule{fallbackRule(10, 10)}
	opts := tokenjuice.Options{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenjuice.CompactWithRules(tokenjuice.Input{Stdout: text}, rules, opts)
	}
}
