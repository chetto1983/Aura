package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

// canon is the caller-side canonicalization the B2 contract requires the CALLER
// to run before BeforeToolCall/AfterToolResult.
func canon(t *testing.T, v any) []byte {
	t.Helper()
	b, err := canonicaljson.Marshal(v)
	if err != nil {
		t.Fatalf("canonicaljson.Marshal: %v", err)
	}
	return b
}

func TestBudget_BeforeToolCall_CanonicalHashOrderIndependent(t *testing.T) {
	// The caller canonicalizes both orderings; identical canonical bytes must
	// produce identical fingerprints (D-08 + B2 contract).
	a := canon(t, map[string]any{"a": 1, "b": 2})
	b := canon(t, map[string]any{"b": 2, "a": 1})
	fpA := computeFingerprint("search", a)
	fpB := computeFingerprint("search", b)
	if fpA != fpB {
		t.Fatalf("canonical args order-independent: fingerprints differ\n a=%x\n b=%x", fpA, fpB)
	}
}

func TestBudget_BeforeToolCall_DistinctArgsDistinctFingerprint(t *testing.T) {
	a := canon(t, map[string]any{"q": "x"})
	b := canon(t, map[string]any{"q": "y"})
	if computeFingerprint("t", a) == computeFingerprint("t", b) {
		t.Fatal("distinct args must hash distinctly")
	}
	// Same args, different tool name → distinct fingerprint.
	if computeFingerprint("t1", a) == computeFingerprint("t2", a) {
		t.Fatal("distinct tool names must hash distinctly")
	}
}

func TestBudget_Dedup_Period1_TerminatesOnThreeRepeats(t *testing.T) {
	b := newTestBudget(100, 3)
	args := canon(t, map[string]any{"x": 1})
	result := []byte("same-result")

	// Call 1 + 2: no dedup yet (window=3).
	for i := 0; i < 2; i++ {
		if dedup, _ := b.BeforeToolCall("tool", args); dedup {
			t.Fatalf("call %d should not dedup yet", i+1)
		}
		b.AfterToolResult("tool", args, result)
	}
	// Call 3: same args, unchanged result → dedup fires before re-execute.
	dedup, reason := b.BeforeToolCall("tool", args)
	if !dedup {
		t.Fatal("3rd identical call should dedup (period-1, window=3)")
	}
	if reason != "dedup" {
		t.Fatalf("reason: want dedup, got %q", reason)
	}
}

func TestBudget_Dedup_Period2_PingPong(t *testing.T) {
	b := newTestBudget(100, 3)
	a := canon(t, map[string]any{"k": "A"})
	bb := canon(t, map[string]any{"k": "B"})
	res := []byte("r")

	// A, B, A → then the next A makes A-B-A-B (period-2).
	seq := [][]byte{a, bb, a}
	for _, args := range seq {
		if dedup, _ := b.BeforeToolCall("tool", args); dedup {
			t.Fatal("no dedup expected during A,B,A warmup")
		}
		b.AfterToolResult("tool", args, res)
	}
	dedup, reason := b.BeforeToolCall("tool", a)
	if !dedup {
		t.Fatal("A-B-A-B should trigger period-2 ping-pong dedup")
	}
	if reason != "dedup" {
		t.Fatalf("reason: want dedup, got %q", reason)
	}
}

func TestBudget_Dedup_ResultChanged_SuppressesDedup(t *testing.T) {
	// Same args repeated, but the result keeps CHANGING → progress veto, no dedup
	// even past the window (D-18 fail-safe; volatile results fail SAFE not OPEN).
	b := newTestBudget(100, 3)
	args := canon(t, map[string]any{"x": 1})
	for i := 0; i < 6; i++ {
		dedup, _ := b.BeforeToolCall("tool", args)
		if dedup {
			t.Fatalf("call %d deduped despite changing results (should be progress veto)", i+1)
		}
		b.AfterToolResult("tool", args, []byte(fmt.Sprintf("result-%d", i)))
	}
}

func TestBudget_Child_DistinctDedupRing(t *testing.T) {
	parent := newTestBudget(100, 3)
	args := canon(t, map[string]any{"x": 1})
	res := []byte("r")
	// Saturate the parent ring to the dedup threshold.
	for i := 0; i < 3; i++ {
		parent.BeforeToolCall("tool", args)
		parent.AfterToolResult("tool", args, res)
	}
	// A fresh child must NOT inherit the parent's dedup state.
	child := parent.Child(1)
	if dedup, _ := child.BeforeToolCall("tool", args); dedup {
		t.Fatal("child ring must be distinct from parent (no cross-branch dedup leakage, D-09)")
	}
}

func TestBudget_Dedup_ExemptTool_NeverDedups(t *testing.T) {
	b := newTestBudget(100, 3)
	b.exemptTools = map[string]struct{}{"poll": {}}
	args := canon(t, map[string]any{"x": 1})
	res := []byte("same")
	for i := 0; i < 10; i++ {
		if dedup, _ := b.BeforeToolCall("poll", args); dedup {
			t.Fatalf("exempt tool deduped on call %d (D-19 allowlist violated)", i+1)
		}
		b.AfterToolResult("poll", args, res)
	}
}

func TestParseExemptTools_CSV(t *testing.T) {
	got := parseExemptTools(" web_fetch , web_search ,, ")
	if len(got) != 2 {
		t.Fatalf("want 2 exempt tools, got %d: %v", len(got), got)
	}
	if _, ok := got["web_fetch"]; !ok {
		t.Fatal("web_fetch should be exempt (trimmed)")
	}
	if _, ok := got[""]; ok {
		t.Fatal("blank CSV entries must be dropped")
	}
}

func TestBudget_Dedup_RingCapacity_AtLeastFour(t *testing.T) {
	// WINDOW=3 but ring must hold >=4 to observe period-2 (D-20).
	if c := ringCapacity(3); c != 4 {
		t.Fatalf("ringCapacity(3): want 4, got %d", c)
	}
	if c := ringCapacity(5); c != 5 {
		t.Fatalf("ringCapacity(5): want 5, got %d", c)
	}
}

// TestBudget_Property_TotalConsumedNeverExceedsMax asserts the core budget
// invariant under random concurrent consume sequences (D-21): total successful
// ConsumeStep across all goroutines never exceeds the initial max.
func TestBudget_Property_TotalConsumedNeverExceedsMax(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxSteps := rapid.Int32Range(0, 200).Draw(rt, "maxSteps")
		goroutines := rapid.IntRange(1, 8).Draw(rt, "goroutines")
		attemptsPer := rapid.IntRange(0, 50).Draw(rt, "attemptsPer")

		var steps atomic.Int32
		steps.Store(maxSteps)
		b := &Budget{
			steps:             &steps,
			deadlineWallclock: time.Now().Add(time.Hour),
			now:               time.Now,
			dedupWindow:       3,
			dedupRing:         newDedupRing(3),
		}

		var ok int64
		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < attemptsPer; i++ {
					if got, _ := b.ConsumeStep(); got {
						atomic.AddInt64(&ok, 1)
					}
				}
			}()
		}
		wg.Wait()

		if ok > int64(maxSteps) {
			rt.Fatalf("INVARIANT VIOLATED: consumed %d > max %d", ok, maxSteps)
		}
		if b.Remaining() < 0 {
			rt.Fatalf("remaining went negative: %d", b.Remaining())
		}
	})
}
