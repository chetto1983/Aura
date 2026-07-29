package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// TestRunnerInjectsSharedBreakerIntoEveryTurn proves the B-05 wiring: the Runner owns
// ONE breaker and injects it into each per-turn agent (buildAgent). With that breaker
// already open — as a prior failed turn would leave it — a fresh turn must
// short-circuit before reaching the provider. The old per-turn breaker was reset on
// every rebuild, so this cross-turn protection did not exist.
func TestRunnerInjectsSharedBreakerIntoEveryTurn(t *testing.T) {
	open := llm.NewBreaker(1, time.Hour)
	open.Failure(errors.New("provider down")) // stands in for a prior tripped turn

	client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "must never reach the provider"))
	r := newBreakerRunner(t, client, open)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	_, _ = drain(r.Turn(context.Background(), convID, new("hi")))
	if client.CallCount() != 0 {
		t.Fatalf("a turn under the Runner's OPEN shared breaker must not call the provider, calls=%d", client.CallCount())
	}
}

// newBreakerRunner builds a Runner over the in-memory fakes with a caller-supplied
// breaker injected through Deps (mirrors newTestRunnerCfg but exercises Deps.Breaker).
func newBreakerRunner(t *testing.T, client llm.Client, breaker *llm.Breaker) *Runner {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	reg.Register(&tools.ReadToolOutput{})
	return New(Deps{
		Conv:            newFakeConvStore(),
		Pause:           newFakePauseStore(),
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
		Breaker:         breaker,
	})
}
