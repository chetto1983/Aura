package agent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

// benchEpoch is a fixed clock anchor far in the past; benchFarFuture pins now to it
// while the deadline (zero value below) is never set, so the wallclock branch of
// ConsumeStep is a cheap After(zero) compare that never trips during the bench.
var benchEpoch = time.Unix(1, 0)

// These benchmarks target the agent core's real per-step hot path — the budget
// gate every agent step passes through and the two-tier dedup that runs on every
// tool call — NOT the tool-search ranking path (the pre-existing
// internal/agent/tools.BenchmarkToolSearchRank measures a once-per-turn retrieval
// query, not the per-step loop spine). See docs/audit/T-01-test-apex.md.

func benchBudget(maxSteps int32, window int) *Budget {
	var steps atomic.Int32
	steps.Store(maxSteps)
	return &Budget{
		steps:             &steps,
		deadlineWallclock: benchEpoch.Add(time.Hour),
		dedupWindow:       window,
		dedupRing:         newDedupRing(window),
		resultCap:         defaultDedupResultCap,
		softFrac:          defaultBranchSoftFrac,
		now:               benchFarFuture,
	}
}

// benchFarFuture is a fixed-now clock pinned before the deadline so ConsumeStep's
// wallclock check never trips and the benchmark times only the atomic
// decrement-check-restore (D-11).
var benchFarFuture = func() time.Time { return benchEpoch }

// BenchmarkConsumeStep measures the TOCTOU-safe decrement gate in isolation. The
// counter is refilled before the budget bottoms out so we always time the
// success path (the dominant production case), never the restore-on-overshoot.
func BenchmarkConsumeStep(b *testing.B) {
	bud := benchBudget(1<<30, 3)
	b.ReportAllocs()
	for b.Loop() {
		if ok, _ := bud.ConsumeStep(); !ok {
			bud.SetMaxSteps(1 << 30)
		}
	}
}

// BenchmarkConsumeStepParallel stresses the shared-atomic gate under contention —
// the real shape when a ParallelAgent fan-out hammers the single shared counter
// (D-10). It exercises both the success decrement and the overshoot-restore path.
func BenchmarkConsumeStepParallel(b *testing.B) {
	bud := benchBudget(1<<30, 3)
	var refill sync.Once
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if ok, _ := bud.ConsumeStep(); !ok {
				refill.Do(func() { bud.SetMaxSteps(1 << 30) })
			}
		}
	})
}

// BenchmarkDedupBeforeToolCall_Distinct measures the common no-repeat dedup path:
// each call has distinct args, so the fingerprint never matches and the loop never
// trips. This is the per-tool-call cost the agent pays on a healthy (progressing)
// conversation — the dominant case.
func BenchmarkDedupBeforeToolCall_Distinct(b *testing.B) {
	bud := benchBudget(1<<30, 3)
	args := make([][]byte, 256)
	for i := range args {
		canon, err := canonicaljson.Marshal(map[string]any{"q": i, "page": i % 7})
		if err != nil {
			b.Fatalf("canon: %v", err)
		}
		args[i] = canon
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		bud.BeforeToolCall("web_search", args[i&255])
		i++
	}
}

// BenchmarkDedupRoundTrip measures the full two-tier round trip on REPEATED args
// with a CHANGING result — the progress-veto path (D-18) that keeps a
// volatile-result tool from fail-opening. This is the most work the dedup does per
// step: fingerprint + ring scan (period-1 + ping-pong) + result-hash veto update.
func BenchmarkDedupRoundTrip(b *testing.B) {
	bud := benchBudget(1<<30, 3)
	args, err := canonicaljson.Marshal(map[string]any{"url": "https://example.test/feed"})
	if err != nil {
		b.Fatalf("canon: %v", err)
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		bud.BeforeToolCall("web_fetch", args)
		// A changing result preview each iteration (volatile-result tool) keeps the
		// veto resetting, exercising the map write + sha256 on every round trip.
		bud.AfterToolResult("web_fetch", args, []byte{byte(i), byte(i >> 8), byte(i >> 16)})
		i++
	}
}
