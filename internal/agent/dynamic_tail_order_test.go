package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func TestDynamicTailStaysOutOfAdaptiveRouterUntilDelivery(t *testing.T) {
	t.Parallel()
	order := []string{}
	client := &dynamicTailClient{
		order: &order,
		turns: []dynamicTailTurn{
			dynamicTailTextTurn("stop", `{"tier":"low"}`),
			dynamicTailTextTurn("stop", "done"),
		},
	}
	exposure := validDynamicTailExposure()
	exposure.Tail.BeforeCurrentUser = false
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		order = append(order, "delivery")
		if !outcome.Delivered {
			t.Fatalf("delivery outcome = %#v, want delivered", outcome)
		}
		return nil
	}
	agent := NewLlmAgent(LlmAgentConfig{
		Client: client,
		LLM: llm.Config{
			Model: "test-model", Provider: "openrouter",
			BaseURL:       "https://openrouter.ai/api/v1",
			ContextWindow: 128_000, MaxTokens: 256, TotalTimeoutSec: 30,
			AdaptiveReasoning: true,
		},
		Registry: tools.NewRegistry(), PreviewCap: 1024, RunDir: t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: []llm.Message{
			{Role: llm.RoleUser, Content: "prior genuine user request"},
			{Role: llm.RoleAssistant, Content: "prior answer"},
			{
				Role: llm.RoleUser, Content: exposure.Tail.Content,
				DynamicTailID: exposure.Tail.ID,
			},
		},
		DynamicTail: exposure,
	})

	runDynamicTailAgent(t, agent)

	if !slices.Equal(order, []string{"stream", "delivery", "stream"}) {
		t.Fatalf("order = %v, want router then delivery then primary stream", order)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want router and primary", len(client.requests))
	}
	if countDynamicTail(client.requests[0].Messages, exposure.Tail.Content) != 0 {
		t.Fatalf("adaptive router saw uncommitted dynamic tail: %#v", client.requests[0].Messages)
	}
	if countDynamicTail(client.requests[1].Messages, exposure.Tail.Content) != 1 {
		t.Fatalf("primary request dynamic tail count != 1: %#v", client.requests[1].Messages)
	}
}

func TestDynamicTailHookRemovalFallbackPreservesCurrentUser(t *testing.T) {
	t.Parallel()
	client := &dynamicTailClient{
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		if outcome.Delivered || outcome.FallbackReason != DynamicTailFallbackInvalid {
			t.Fatalf("fallback outcome = %#v", outcome)
		}
		return nil
	}
	hook := NewHookManager(dynamicTailMutatingHook{mutate: func(request *llm.Request) {
		filtered := request.Messages[:0]
		for _, message := range request.Messages {
			if message.Content != exposure.Tail.Content {
				filtered = append(filtered, message)
			}
		}
		request.Messages = filtered
	}})
	agent := newDynamicTailAgent(t, client, exposure, hook)

	runDynamicTailAgent(t, agent)

	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want one static fallback", len(client.requests))
	}
	messages := client.requests[0].Messages
	if countDynamicTail(messages, exposure.Tail.Content) != 0 {
		t.Fatalf("static fallback leaked dynamic tail: %#v", messages)
	}
	if countDynamicTail(messages, "current user") != 1 {
		t.Fatalf("static fallback removed current user: %#v", messages)
	}
}

func TestDynamicTailFallbackPreservesPersistedDuplicateContent(t *testing.T) {
	t.Parallel()
	client := &dynamicTailClient{
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.Included = false
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		if outcome.Delivered ||
			outcome.FallbackReason != DynamicTailFallbackContextBudget {
			t.Fatalf("fallback outcome = %#v", outcome)
		}
		return nil
	}
	agent := NewLlmAgent(LlmAgentConfig{
		Client: client,
		LLM: llm.Config{
			Model: "test-model", Provider: "openrouter",
			ContextWindow: 128_000, MaxTokens: 256, TotalTimeoutSec: 30,
		},
		Registry: tools.NewRegistry(), PreviewCap: 1024, RunDir: t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: []llm.Message{
			{Role: llm.RoleUser, Content: exposure.Tail.Content},
			{Role: llm.RoleUser, Content: exposure.Tail.Content},
			{
				Role: llm.RoleUser, Content: exposure.Tail.Content,
				DynamicTailID: exposure.Tail.ID,
			},
			{Role: llm.RoleUser, Content: "current user"},
		},
		DynamicTail: exposure,
	})

	runDynamicTailAgent(t, agent)

	if got := countDynamicTail(
		client.requests[0].Messages,
		exposure.Tail.Content,
	); got != 2 {
		t.Fatalf("persisted duplicate count = %d, want 2", got)
	}
}

func TestDynamicTailEarlyHookResultCommitsContextBudgetOmission(t *testing.T) {
	t.Parallel()
	client := &dynamicTailClient{
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "unreached")},
	}
	exposure := validDynamicTailExposure()
	exposure.Included = false
	var commits int
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		commits++
		if outcome.Delivered || outcome.FallbackReason != DynamicTailFallbackContextBudget {
			t.Fatalf("budget outcome = %#v", outcome)
		}
		return nil
	}
	hook := NewHookManager(dynamicTailMutatingHook{
		result: &ModelHookResult{Content: "synthetic", FinishReason: "hook"},
	})
	agent := newDynamicTailAgent(t, client, exposure, hook)

	runDynamicTailAgent(t, agent)

	if commits != 1 {
		t.Fatalf("context-budget commits = %d, want 1", commits)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model calls = %d, want none after hook result", len(client.requests))
	}
}
