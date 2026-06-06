package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// newRunnerWithCacheFake builds a Runner whose CacheMetricStore is an inspectable fake,
// so the persist-seam tests can assert one metric row per completed assistant turn.
func newRunnerWithCacheFake(t *testing.T, client llm.Client, cache *fakeCacheMetricStore) (*Runner, *fakeConvStore) {
	t.Helper()
	conv := newFakeConvStore()
	conv.cache = cache
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	r := New(Deps{
		Conv:            conv,
		Pause:           newFakePauseStore(),
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    cache,
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    time.Second,
		StopTimeout:     time.Second,
	})
	return r, conv
}

// TestPersistAssistantAnswer_WritesOneCacheMetric proves D-02/D-02a: each completed
// assistant turn writes exactly one append-only cache_metrics row from the
// already-computed usage (no wire-path touch — the same numbers feed the conv turn).
func TestPersistAssistantAnswer_WritesOneCacheMetric(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "All done.")),
	)
	cache := newFakeCacheMetricStore()
	r, _ := newRunnerWithCacheFake(t, client, cache)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := cache.count(); got != 1 {
		t.Fatalf("want exactly 1 cache_metrics row for one completed turn, got %d", got)
	}
	if cache.inserts[0].Seq == 0 {
		t.Error("cache metric seq: want the assistant turn seq (>0), got 0")
	}
}

// TestPersistAssistantAnswer_CacheMetricErrorSurfaces proves the persist path propagates
// a metric-store failure (no silent drop — no-skip discipline).
func TestPersistAssistantAnswer_CacheMetricErrorSurfaces(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "All done.")),
	)
	cache := newFakeCacheMetricStore()
	cache.insertEr = errFake
	r, conv := newRunnerWithCacheFake(t, client, cache)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	_, err := drain(r.Turn(ctx, convID, userPtr("hi")))
	if err == nil {
		t.Fatal("want the cache-metric insert error to surface, got nil")
	}
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := len(conv.turns[convID]); got != 1 {
		t.Fatalf("want only the user turn after rolled-back assistant metric failure, got %d turns", got)
	}
}

// TestPersistCacheMetric_NilStoreFailsLoud proves a nil CacheMetricStore is a wiring bug
// surfaced loudly (the prod composition root MUST inject a Store).
func TestPersistCacheMetric_NilStoreFailsLoud(t *testing.T) {
	r := &Runner{}
	_, err := r.cacheMetricParams(newConvID(t), 3, llm.Usage{PromptTokens: 10, CachedTokens: 5}, 0.01)
	if err == nil {
		t.Fatal("want a loud error when cacheMetrics store is nil, got nil")
	}
}
