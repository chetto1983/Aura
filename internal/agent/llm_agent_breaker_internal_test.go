package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// TestInjectedBreakerIsSharedNotPerAgent proves B-05: a breaker injected via
// LlmAgentConfig.Breaker is SHARED state, not minted per agent. The Runner builds a
// FRESH LlmAgent every turn (runner.go buildAgent); the old per-turn breaker reset
// each turn, so a provider outage never tripped cross-turn protection. With the
// breaker injected, opening it (as a failed turn would) must short-circuit EVERY
// agent that holds it — including agents built before it opened — without calling
// the provider. A control agent with no injected breaker gets a fresh closed one and
// proceeds, proving the field actually changes wiring.
func TestInjectedBreakerIsSharedNotPerAgent(t *testing.T) {
	shared := llm.NewBreaker(1, time.Hour) // opens on the first recorded failure

	mk := func(c llm.Client, b *llm.Breaker) *LlmAgent {
		return NewLlmAgent(LlmAgentConfig{
			Client:   c,
			LLM:      llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
			Registry: tools.NewRegistry(),
			Breaker:  b,
		})
	}

	// Two per-turn agents sharing ONE breaker, both built while it is still closed.
	c1 := &retryClient{chunks: []llm.Chunk{{Text: "would succeed"}}}
	c2 := &retryClient{chunks: []llm.Chunk{{Text: "would succeed"}}}
	a1 := mk(c1, shared)
	a2 := mk(c2, shared)

	// A failed turn trips the shared breaker AFTER both agents were constructed.
	shared.Failure(errors.New("provider down"))

	for _, tc := range []struct {
		name string
		a    *LlmAgent
		c    *retryClient
	}{
		{"agent-1", a1, c1},
		{"agent-2", a2, c2},
	} {
		_, err := tc.a.streamWithOpenRetry(context.Background(), llm.Request{}, tc.name)
		if !errors.Is(err, llm.ErrBreakerOpen) {
			t.Fatalf("%s must short-circuit on the shared open breaker, got %v", tc.name, err)
		}
		if tc.c.calls != 0 {
			t.Fatalf("%s must NOT call its provider while the shared breaker is open, calls=%d", tc.name, tc.c.calls)
		}
	}

	// Control: no injected breaker → a fresh closed breaker → the call proceeds.
	c3 := &retryClient{chunks: []llm.Chunk{{Text: "ok"}}}
	a3 := mk(c3, nil)
	if _, err := a3.streamWithOpenRetry(context.Background(), llm.Request{}, "control"); err != nil {
		t.Fatalf("control agent with its own fresh breaker must proceed, got %v", err)
	}
	if c3.calls != 1 {
		t.Fatalf("control agent must call its provider exactly once, calls=%d", c3.calls)
	}
}
