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

// TestBudget_Dedup_Period1_WindowParameterized pins the EXACT call index at which
// period-1 dedup terminates across window in {1,2,3,4,5} (WR-03). A constant
// args+result sequence is a real period-1 loop; dedup fires on call N==window, but
// never before the result has been recorded once (seen), so the earliest possible
// termination is call 2. Expected index is therefore max(2, window).
func TestBudget_Dedup_Period1_WindowParameterized(t *testing.T) {
	cases := []struct {
		window        int
		wantDedupCall int // 1-based BeforeToolCall index that first returns dedup=true
	}{
		{window: 1, wantDedupCall: 2},
		{window: 2, wantDedupCall: 2},
		{window: 3, wantDedupCall: 3},
		{window: 4, wantDedupCall: 4},
		{window: 5, wantDedupCall: 5},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("window=%d", tc.window), func(t *testing.T) {
			b := newTestBudget(1000, tc.window)
			args := canon(t, map[string]any{"x": 1})
			result := []byte("constant")

			gotCall := 0
			for call := 1; call <= tc.window+3; call++ {
				dedup, reason := b.BeforeToolCall("tool", args)
				if dedup {
					gotCall = call
					if reason != "dedup" {
						t.Fatalf("window=%d call=%d: reason want dedup, got %q", tc.window, call, reason)
					}
					break
				}
				b.AfterToolResult("tool", args, result)
			}
			if gotCall != tc.wantDedupCall {
				t.Fatalf("window=%d: period-1 terminated on call %d, want %d", tc.window, gotCall, tc.wantDedupCall)
			}
		})
	}
}

// TestBudget_Dedup_Period2_WindowIndependent pins that period-2 (A-B-A-B) dedup
// fires on the 4th BeforeToolCall for EVERY window >= 2 (WR-03). isPingPong is a
// fixed period-2 detector (last three == A,B,A) and its progress veto needs only
// one stable repeat of A (repeats >= 1), so it must be DECOUPLED from the period-1
// window threshold. The old code gated it on repeats+2 >= window, which wrongly
// suppressed ping-pong for window >= 4 — this test would fail under that formula.
//
// window=1 is excluded: at window=1 period-1 is so aggressive that the second
// sighting of A in the A,B,A warmup (call 3) already terminates via period-1,
// before the period-2 shape completes. That is correct (and covered by the
// period-1 table) — period-2 as a DISTINCT phenomenon only exists for window >= 2.
func TestBudget_Dedup_Period2_WindowIndependent(t *testing.T) {
	for _, window := range []int{2, 3, 4, 5} {
		t.Run(fmt.Sprintf("window=%d", window), func(t *testing.T) {
			b := newTestBudget(1000, window)
			a := canon(t, map[string]any{"k": "A"})
			bb := canon(t, map[string]any{"k": "B"})
			res := []byte("r")

			// A, B, A warmup → the 4th call (A) forms A-B-A-B.
			seq := [][]byte{a, bb, a}
			for i, args := range seq {
				if dedup, _ := b.BeforeToolCall("tool", args); dedup {
					t.Fatalf("window=%d: unexpected dedup during warmup call %d", window, i+1)
				}
				b.AfterToolResult("tool", args, res)
			}
			dedup, reason := b.BeforeToolCall("tool", a)
			if !dedup {
				t.Fatalf("window=%d: A-B-A-B must trigger period-2 dedup on call 4 (window-independent)", window)
			}
			if reason != "dedup" {
				t.Fatalf("window=%d: reason want dedup, got %q", window, reason)
			}
		})
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

func TestBudget_Dedup_ResultCapZeroMeansUnbounded(t *testing.T) {
	b := newTestBudget(100, 3)
	b.resultCap = 0
	args := canon(t, map[string]any{"x": 1})

	if dedup, _ := b.BeforeToolCall("tool", args); dedup {
		t.Fatal("first call must not dedup")
	}
	b.AfterToolResult("tool", args, []byte("abcdef"))
	if dedup, _ := b.BeforeToolCall("tool", args); dedup {
		t.Fatal("second call must not dedup before the window")
	}
	b.AfterToolResult("tool", args, []byte("abcXYZ"))
	if dedup, _ := b.BeforeToolCall("tool", args); dedup {
		t.Fatal("resultCap=0 is unbounded; changed full results must veto dedup")
	}
}

func TestBudget_Dedup_ResultCapTruncatesPastLimit(t *testing.T) {
	b := newTestBudget(100, 3)
	b.resultCap = 3
	args := canon(t, map[string]any{"x": 1})

	if dedup, _ := b.BeforeToolCall("tool", args); dedup {
		t.Fatal("first call must not dedup")
	}
	b.AfterToolResult("tool", args, []byte("abcdef"))
	if dedup, _ := b.BeforeToolCall("tool", args); dedup {
		t.Fatal("second call must not dedup before the window")
	}
	b.AfterToolResult("tool", args, []byte("abcXYZ"))
	dedup, reason := b.BeforeToolCall("tool", args)
	if !dedup {
		t.Fatal("equal capped result previews must allow dedup at the window")
	}
	if reason != "dedup" {
		t.Fatalf("reason: want dedup, got %q", reason)
	}
}

func TestBudget_Dedup_PingPongRequiresStableRepeat(t *testing.T) {
	b := newTestBudget(100, 3)
	a := canon(t, map[string]any{"k": "A"})
	bb := canon(t, map[string]any{"k": "B"})

	if dedup, _ := b.BeforeToolCall("tool", a); dedup {
		t.Fatal("first A must not dedup")
	}
	b.AfterToolResult("tool", a, []byte("A-1"))
	if dedup, _ := b.BeforeToolCall("tool", bb); dedup {
		t.Fatal("B warmup must not dedup")
	}
	b.AfterToolResult("tool", bb, []byte("B"))
	if dedup, _ := b.BeforeToolCall("tool", a); dedup {
		t.Fatal("second A warmup must not dedup")
	}
	b.AfterToolResult("tool", a, []byte("A-2"))

	if dedup, _ := b.BeforeToolCall("tool", a); dedup {
		t.Fatal("A-B-A-A with changed A result is progress and must not ping-pong dedup")
	}
}

func TestBudget_Dedup_Period3CycleTerminates(t *testing.T) {
	b := newTestBudget(100, 6)
	args := map[string][]byte{
		"A": canon(t, map[string]any{"k": "A"}),
		"B": canon(t, map[string]any{"k": "B"}),
		"C": canon(t, map[string]any{"k": "C"}),
	}
	result := []byte("stable")

	for i, key := range []string{"A", "B", "C", "A", "B", "C"} {
		if dedup, reason := b.BeforeToolCall("tool", args[key]); dedup {
			t.Fatalf("warmup call %d (%s) deduped early with reason %q", i+1, key, reason)
		}
		b.AfterToolResult("tool", args[key], result)
	}

	dedup, reason := b.BeforeToolCall("tool", args["A"])
	if !dedup {
		t.Fatal("A-B-C-A-B-C-A should trigger period-3 cycle dedup before another full cycle burns budget")
	}
	if reason != "dedup" {
		t.Fatalf("reason: want dedup, got %q", reason)
	}
}

func TestBudget_BeforeAfterToolResult_Concurrent(t *testing.T) {
	b := newTestBudget(10000, 6)

	const goroutines = 16
	const iterations = 200
	argPool := make([][][]byte, goroutines)
	for g := 0; g < goroutines; g++ {
		argPool[g] = make([][]byte, 12)
		for i := 0; i < 12; i++ {
			argPool[g][i] = canon(t, map[string]any{
				"g": g,
				"i": i,
			})
		}
	}
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				args := argPool[g][i%len(argPool[g])]
				dedup, _ := b.BeforeToolCall("tool", args)
				if !dedup {
					b.AfterToolResult("tool", args, []byte(fmt.Sprintf("result-%d", i%3)))
				}
			}
		}()
	}
	wg.Wait()
}

func TestDedupRingPush_PrunesEvictedResult(t *testing.T) {
	b := newTestBudget(100, 3)
	var fps []fingerprint
	for i := 0; i < 5; i++ {
		args := canon(t, map[string]any{"i": i})
		fp := computeFingerprint("tool", args)
		fps = append(fps, fp)
		if dedup, _ := b.BeforeToolCall("tool", args); dedup {
			t.Fatalf("unique call %d should not dedup", i)
		}
		b.AfterToolResult("tool", args, []byte("result"))
	}

	if _, ok := b.dedupRing.results[fps[0]]; ok {
		t.Fatal("result for evicted fingerprint should be pruned")
	}
	for _, fp := range fps[1:] {
		if _, ok := b.dedupRing.results[fp]; !ok {
			t.Fatalf("live fingerprint %x missing result tracking", fp)
		}
	}
	if got, want := len(b.dedupRing.results), len(b.dedupRing.entries); got != want {
		t.Fatalf("results map size = %d, want live ring size %d", got, want)
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

func TestBudget_Dedup_ExemptToolBreaksRepeatSequence(t *testing.T) {
	b := newTestBudget(100, 3)
	b.exemptTools = map[string]struct{}{"poll": {}}
	searchArgs := canon(t, map[string]any{"q": "same"})
	pollArgs := canon(t, map[string]any{"cursor": "next"})
	result := []byte("same-result")

	if dedup, _ := b.BeforeToolCall("search", searchArgs); dedup {
		t.Fatal("first search must not dedup")
	}
	b.AfterToolResult("search", searchArgs, result)
	if dedup, _ := b.BeforeToolCall("search", searchArgs); dedup {
		t.Fatal("second search must not dedup before the window")
	}
	b.AfterToolResult("search", searchArgs, result)
	if dedup, _ := b.BeforeToolCall("poll", pollArgs); dedup {
		t.Fatal("exempt poll must never dedup")
	}
	b.AfterToolResult("poll", pollArgs, []byte("poll-result"))
	if dedup, _ := b.BeforeToolCall("search", searchArgs); dedup {
		t.Fatal("exempt poll should break the consecutive search sequence")
	}
}

func TestBudget_Dedup_ExemptToolDoesNotRecordResults(t *testing.T) {
	b := newTestBudget(100, 3)
	b.exemptTools = map[string]struct{}{"poll": {}}
	args := canon(t, map[string]any{"cursor": "next"})

	if dedup, _ := b.BeforeToolCall("poll", args); dedup {
		t.Fatal("exempt poll must never dedup")
	}
	b.AfterToolResult("poll", args, []byte("same"))
	if _, ok := b.dedupRing.results[computeFingerprint("poll", args)]; ok {
		t.Fatal("exempt tool results must not be recorded for progress veto")
	}
}

func TestBudget_Dedup_ExemptToolIgnoresExistingDedupState(t *testing.T) {
	b := newTestBudget(100, 3)
	args := canon(t, map[string]any{"cursor": "next"})
	result := []byte("same-result")

	for i := 0; i < 2; i++ {
		if dedup, _ := b.BeforeToolCall("poll", args); dedup {
			t.Fatalf("warmup poll call %d must not dedup", i+1)
		}
		b.AfterToolResult("poll", args, result)
	}

	b.exemptTools = map[string]struct{}{"poll": {}}
	if dedup, reason := b.BeforeToolCall("poll", args); dedup {
		t.Fatalf("exempt poll must ignore pre-existing dedup state, reason %q", reason)
	}
}

func TestExemptToolsFromEnv_MergesEnvAndExtra(t *testing.T) {
	t.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", "search, web_fetch")
	got := ExemptToolsFromEnv("noop")
	for _, want := range []string{"search", "web_fetch", "noop"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("exempt set must contain %q (env merged with extra), got %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("want 3 exempt tools (2 env + 1 extra), got %d: %v", len(got), got)
	}
}

func TestExemptToolsFromEnv_EmptyEnv_ExtraOnly(t *testing.T) {
	t.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", "")
	got := ExemptToolsFromEnv("noop")
	if len(got) != 1 {
		t.Fatalf("empty env → only the extra tool, got %d: %v", len(got), got)
	}
	if _, ok := got["noop"]; !ok {
		t.Fatal("extra tool noop must be present")
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

func TestNewDedupRing_NormalizesNonPositiveWindow(t *testing.T) {
	r := newDedupRing(0)
	if r.window != 1 {
		t.Fatalf("non-positive window should normalize to 1, got %d", r.window)
	}
	if c := cap(r.entries); c != 4 {
		t.Fatalf("non-positive window should still allocate period-2 capacity 4, got %d", c)
	}
}

func TestDedupRingPush_EvictsAtCapacity(t *testing.T) {
	r := newDedupRing(3)
	fps := make([]fingerprint, 5)
	for i := range fps {
		fps[i] = computeFingerprint("tool", canon(t, map[string]any{"i": i}))
		r.push(fps[i])
	}

	if len(r.entries) != 4 {
		t.Fatalf("ring len after overflow: want 4, got %d", len(r.entries))
	}
	for i, want := range fps[1:] {
		if got := r.entries[i]; got != want {
			t.Fatalf("entry %d after eviction:\n want %x\n got  %x", i, want, got)
		}
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
