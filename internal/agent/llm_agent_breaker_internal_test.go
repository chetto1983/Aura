package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
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

// TestBreakerOpenRoutesToFinalize proves B-06: an open breaker at the start of a turn
// is graceful degradation, not an infra failure. The loop must route to finalize() —
// a NON-EMPTY terminal Event — instead of the iter.Seq2 error slot, and must never
// call the provider (the breaker short-circuits the stream open). finalize's own
// synthesis hits the same open breaker and falls back to the deterministic stub
// digest, which is non-empty even with no gathered results.
func TestBreakerOpenRoutesToFinalize(t *testing.T) {
	open := llm.NewBreaker(1, time.Hour)
	open.Failure(errors.New("provider down"))

	client := &retryClient{chunks: []llm.Chunk{{Text: "must never be called"}}}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	a := NewLlmAgent(LlmAgentConfig{
		Client:    client,
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:  reg,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Breaker:   open,
	})
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}

	evs, err := collectInternal(a.Run(InvocationContext{
		Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget,
	}))
	if err != nil {
		t.Fatalf("an open breaker must NOT surface via the iter.Seq2 error slot, got %v", err)
	}
	final := finalInternal(t, evs)
	if strings.TrimSpace(final) == "" {
		t.Fatal("an open-breaker turn must still emit a non-empty terminal answer (finalize)")
	}
	if client.calls != 0 {
		t.Fatalf("an open breaker must short-circuit before calling the provider, calls=%d", client.calls)
	}
}
