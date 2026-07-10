package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestWithReasoningOverride is the ctx-accessor round-trip: a set override reads back
// with ok=true; a bare ctx reads back the zero effort with ok=false (the auto case).
func TestWithReasoningOverride(t *testing.T) {
	t.Parallel()

	t.Run("round_trip_fixed_effort", func(t *testing.T) {
		t.Parallel()
		ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortHigh)
		eff, ok := reasoningOverride(ctx)
		if !ok {
			t.Fatal("reasoningOverride ok = false, want true after WithReasoningOverride")
		}
		if eff != llm.ReasoningEffortHigh {
			t.Fatalf("effort = %q, want high", eff)
		}
	})

	t.Run("bare_ctx_is_auto", func(t *testing.T) {
		t.Parallel()
		eff, ok := reasoningOverride(context.Background())
		if ok {
			t.Fatalf("bare ctx reported an override (%q); want ok=false (auto)", eff)
		}
		if eff != "" {
			t.Fatalf("bare ctx effort = %q, want empty", eff)
		}
	})

	// An explicit empty override is distinguishable from absent: ok=true, effort="".
	// (The run handler only sets a FIXED level, but the accessor must not lie about
	// what ctx carries.)
	t.Run("explicit_empty_is_set", func(t *testing.T) {
		t.Parallel()
		ctx := WithReasoningOverride(context.Background(), "")
		eff, ok := reasoningOverride(ctx)
		if !ok {
			t.Fatal("explicit empty override ok = false, want true")
		}
		if eff != "" {
			t.Fatalf("effort = %q, want empty", eff)
		}
	})
}
