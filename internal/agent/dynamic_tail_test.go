package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

func TestDynamicTailCommitsBeforeFirstStreamAndRevalidatesEveryRound(t *testing.T) {
	t.Parallel()
	order := []string{}
	client := &dynamicTailClient{
		order: &order,
		turns: []dynamicTailTurn{
			dynamicTailToolCallTurn(dynamicTailToolCall("call-1", "round_probe", `{}`)),
			dynamicTailTextTurn("stop", "done"),
		},
	}
	exposure := validDynamicTailExposure()
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		order = append(order, "delivery")
		if !outcome.Delivered || outcome.FallbackReason != "" {
			t.Fatalf("delivery outcome = %#v, want successful exposure", outcome)
		}
		return nil
	}
	agent := newDynamicTailAgent(t, client, exposure, nil)

	runDynamicTailAgent(t, agent)

	if !slices.Equal(order, []string{"delivery", "stream", "stream"}) {
		t.Fatalf("order = %v, want delivery before first stream and no duplicate", order)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want two primary rounds", len(client.requests))
	}
	for index, request := range client.requests {
		if countDynamicTail(request.Messages, exposure.Tail.Content) != 1 {
			t.Fatalf("round %d dynamic tail count != 1: %#v", index, request.Messages)
		}
	}
}

func TestDynamicTailHookTamperFallsBackBeforeStream(t *testing.T) {
	t.Parallel()
	order := []string{}
	client := &dynamicTailClient{
		order: &order,
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		order = append(order, "fallback")
		if outcome.Delivered || outcome.FallbackReason != DynamicTailFallbackInvalid {
			t.Fatalf("fallback outcome = %#v", outcome)
		}
		return nil
	}
	hook := NewHookManager(dynamicTailMutatingHook{mutate: func(request *llm.Request) {
		for index := range request.Messages {
			if request.Messages[index].Content == exposure.Tail.Content {
				request.Messages[index].Content += " tampered"
			}
		}
	}})
	agent := newDynamicTailAgent(t, client, exposure, hook)

	runDynamicTailAgent(t, agent)

	if !slices.Equal(order, []string{"fallback", "stream"}) {
		t.Fatalf("order = %v, want fallback delivery before static stream", order)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want one", len(client.requests))
	}
	for _, message := range client.requests[0].Messages {
		if message.Content == exposure.Tail.Content ||
			message.Content == exposure.Tail.Content+" tampered" {
			t.Fatalf("static fallback leaked dynamic tail: %#v", client.requests[0].Messages)
		}
	}
}

func TestDynamicTailDeliveryFailureServesStaticRequest(t *testing.T) {
	t.Parallel()
	client := &dynamicTailClient{
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.Commit = func(context.Context, DynamicTailOutcome) error {
		return errors.New("ledger unavailable")
	}
	agent := newDynamicTailAgent(t, client, exposure, nil)

	runDynamicTailAgent(t, agent)

	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want one", len(client.requests))
	}
	if countDynamicTail(client.requests[0].Messages, exposure.Tail.Content) != 0 {
		t.Fatal("delivery failure exposed learned dynamic tail")
	}
}

func TestDynamicTailContextBudgetOmissionRecordsNoneBeforeStream(t *testing.T) {
	t.Parallel()
	order := []string{}
	client := &dynamicTailClient{
		order: &order,
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.Included = false
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		order = append(order, "fallback")
		if outcome.Delivered || outcome.FallbackReason != DynamicTailFallbackContextBudget {
			t.Fatalf("budget outcome = %#v", outcome)
		}
		return nil
	}
	agent := newDynamicTailAgent(t, client, exposure, nil)

	runDynamicTailAgent(t, agent)

	if !slices.Equal(order, []string{"fallback", "stream"}) {
		t.Fatalf("order = %v, want none/context_budget before stream", order)
	}
	if countDynamicTail(client.requests[0].Messages, "current user") != 1 {
		t.Fatalf("budget fallback removed the current user: %#v", client.requests[0].Messages)
	}
}

func TestDynamicTailHookGrowthPastHardCapFallsBackBeforeStream(t *testing.T) {
	t.Parallel()
	order := []string{}
	client := &dynamicTailClient{
		order: &order,
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.HardCapTokens = 30
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		order = append(order, "fallback")
		if outcome.Delivered || outcome.FallbackReason != DynamicTailFallbackInvalid {
			t.Fatalf("growth fallback outcome = %#v", outcome)
		}
		return nil
	}
	hook := NewHookManager(dynamicTailMutatingHook{mutate: func(request *llm.Request) {
		request.Messages = append(request.Messages, llm.Message{
			Role: llm.RoleUser, Content: strings.Repeat("hook growth ", 200),
		})
	}})
	agent := newDynamicTailAgent(t, client, exposure, hook)

	runDynamicTailAgent(t, agent)

	if !slices.Equal(order, []string{"fallback", "stream"}) {
		t.Fatalf("order = %v, want hard-cap fallback before stream", order)
	}
	if countDynamicTail(client.requests[0].Messages, exposure.Tail.Content) != 0 {
		t.Fatal("hard-cap growth exposed learned dynamic tail")
	}
}

func TestDynamicTailTransportRetryReusesCommittedExposure(t *testing.T) {
	t.Parallel()
	order := []string{}
	client := &dynamicTailClient{
		order: &order,
		turns: []dynamicTailTurn{
			{err: &openai_compat.HTTPError{StatusCode: 502}},
			dynamicTailTextTurn("stop", "done"),
		},
	}
	exposure := validDynamicTailExposure()
	exposure.Commit = func(_ context.Context, outcome DynamicTailOutcome) error {
		order = append(order, "delivery")
		if !outcome.Delivered {
			t.Fatalf("retry delivery outcome = %#v", outcome)
		}
		return nil
	}
	agent := newDynamicTailAgent(t, client, exposure, nil)

	runDynamicTailAgent(t, agent)

	if !slices.Equal(order, []string{"delivery", "stream", "stream"}) {
		t.Fatalf("order = %v, want one delivery and two transport attempts", order)
	}
	if len(client.requests) != 2 {
		t.Fatalf("retry requests = %d, want 2", len(client.requests))
	}
	for index, request := range client.requests {
		if countDynamicTail(request.Messages, exposure.Tail.Content) != 1 {
			t.Fatalf("retry %d lost the committed dynamic tail", index)
		}
	}
}

func TestDynamicTailResumeMovesExactItemToFinalRequestTail(t *testing.T) {
	t.Parallel()
	client := &dynamicTailClient{
		turns: []dynamicTailTurn{dynamicTailTextTurn("stop", "done")},
	}
	exposure := validDynamicTailExposure()
	exposure.Tail.BeforeCurrentUser = false
	exposure.Commit = func(context.Context, DynamicTailOutcome) error {
		return nil
	}
	agent := newDynamicTailAgent(t, client, exposure, nil)

	runDynamicTailAgent(t, agent)

	messages := client.requests[0].Messages
	if messages[len(messages)-1].Content != exposure.Tail.Content {
		t.Fatalf("resumed dynamic tail is not final: %#v", messages)
	}
}

func TestDynamicTailMetadataValidationRejectsEveryTypedBoundary(t *testing.T) {
	valid := validDynamicTailExposure()
	valid.Commit = func(context.Context, DynamicTailOutcome) error { return nil }
	if err := valid.ValidateMetadata(); err != nil {
		t.Fatalf("valid metadata: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*DynamicTailExposure)
	}{
		{name: "missing commit", mutate: func(exposure *DynamicTailExposure) {
			exposure.Commit = nil
		}},
		{name: "not visible", mutate: func(exposure *DynamicTailExposure) {
			exposure.Included = false
		}},
		{name: "incoherent", mutate: func(exposure *DynamicTailExposure) {
			exposure.Coherent = false
		}},
		{name: "missing epoch", mutate: func(exposure *DynamicTailExposure) {
			exposure.CorpusEpochBefore = nil
		}},
		{name: "bad revision", mutate: func(exposure *DynamicTailExposure) {
			exposure.Revisions.Index = " index "
		}},
		{name: "missing results", mutate: func(exposure *DynamicTailExposure) {
			exposure.Results = nil
		}},
		{name: "wrong order", mutate: func(exposure *DynamicTailExposure) {
			exposure.Results[0].Order = 3
		}},
		{name: "non long-term kind", mutate: func(exposure *DynamicTailExposure) {
			exposure.Results[0].Kind = "memory_message"
		}},
		{name: "invalid id", mutate: func(exposure *DynamicTailExposure) {
			exposure.Results[0].ID = "not-a-uuid"
		}},
		{name: "duplicate id", mutate: func(exposure *DynamicTailExposure) {
			exposure.Results[1].ID = exposure.Results[0].ID
		}},
		{name: "missing limit", mutate: func(exposure *DynamicTailExposure) {
			delete(exposure.Limits, "memory_entity")
		}},
		{name: "zero requested limit", mutate: func(exposure *DynamicTailExposure) {
			limit := exposure.Limits["memory_entity"]
			limit.RequestedK = 0
			exposure.Limits["memory_entity"] = limit
		}},
		{name: "count mismatch", mutate: func(exposure *DynamicTailExposure) {
			limit := exposure.Limits["memory_entity"]
			limit.Count = 0
			exposure.Limits["memory_entity"] = limit
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exposure := validDynamicTailExposure()
			exposure.Commit = func(context.Context, DynamicTailOutcome) error {
				return nil
			}
			test.mutate(exposure)
			if err := exposure.ValidateMetadata(); err == nil {
				t.Fatal("ValidateMetadata error = nil")
			}
		})
	}
}

func TestDynamicTailPlacementAndFallbackHelpersFailClosed(t *testing.T) {
	exposure := validDynamicTailExposure()
	exposure.Tail.BeforeCurrentUser = false
	messages := []llm.Message{
		{
			Role: llm.RoleUser, Content: exposure.Tail.Content,
			DynamicTailID: exposure.Tail.ID,
		},
		{
			Role: llm.RoleUser, Content: exposure.Tail.Content,
			DynamicTailID: exposure.Tail.ID,
		},
		{Role: llm.RoleUser, Content: "other"},
	}
	got := placeDynamicTailAtRequestTail(messages, exposure)
	if len(got) != len(messages) || got[2].Content != "other" {
		t.Fatal("duplicate helper rewrote ambiguous dynamic tails")
	}
	if got := placeDynamicTailAtRequestTail(messages, nil); len(got) != len(messages) ||
		got[2].Content != "other" {
		t.Fatal("nil exposure changed messages")
	}

	var commits int
	exposure.Commit = func(context.Context, DynamicTailOutcome) error {
		commits++
		return errors.New("unavailable")
	}
	exposure.commitFallback(context.Background(), DynamicTailFallbackInvalid)
	exposure.deliveryCommitted = true
	exposure.commitFallback(context.Background(), DynamicTailFallbackInvalid)
	exposure.Commit = nil
	exposure.deliveryCommitted = false
	exposure.commitFallback(context.Background(), DynamicTailFallbackInvalid)
	if commits != 1 {
		t.Fatalf("fallback commits = %d, want 1", commits)
	}
}

type dynamicTailClient struct {
	order    *[]string
	turns    []dynamicTailTurn
	requests []llm.Request
}

type dynamicTailTurn struct {
	chunks []llm.Chunk
	err    error
}

func (client *dynamicTailClient) Stream(
	_ context.Context,
	request llm.Request,
) (<-chan llm.Chunk, error) {
	if client.order != nil {
		*client.order = append(*client.order, "stream")
	}
	copy := request
	copy.Messages = append([]llm.Message(nil), request.Messages...)
	client.requests = append(client.requests, copy)
	turn := client.turns[0]
	client.turns = client.turns[1:]
	channel := make(chan llm.Chunk, len(turn.chunks))
	for _, chunk := range turn.chunks {
		channel <- chunk
	}
	close(channel)
	return channel, turn.err
}

type dynamicTailMutatingHook struct {
	mutate func(*llm.Request)
	result *ModelHookResult
}

func (hook dynamicTailMutatingHook) OnTurnStart(context.Context, HookTurn) error {
	return nil
}

func (hook dynamicTailMutatingHook) BeforeModel(
	_ context.Context,
	request *llm.Request,
) (*ModelHookResult, error) {
	if hook.mutate != nil {
		hook.mutate(request)
	}
	return hook.result, nil
}

func (hook dynamicTailMutatingHook) BeforeTool(
	context.Context,
	llm.ToolCall,
) (*ToolHookResult, error) {
	return nil, nil
}

func (hook dynamicTailMutatingHook) AfterTool(
	context.Context,
	llm.ToolCall,
	tools.ToolResult,
) (*ToolResultHookResult, error) {
	return nil, nil
}

func (hook dynamicTailMutatingHook) OnTurnEnd(context.Context, HookTurn) error {
	return nil
}

func newDynamicTailAgent(
	t *testing.T,
	client llm.Client,
	exposure *DynamicTailExposure,
	hooks *HookManager,
) *LlmAgent {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(modelRoundProbeTool{})
	userTurns := []llm.Message{
		{
			Role: llm.RoleUser, Content: exposure.Tail.Content,
			DynamicTailID: exposure.Tail.ID,
		},
	}
	if exposure.Tail.BeforeCurrentUser {
		userTurns = append(userTurns, llm.Message{
			Role: llm.RoleUser, Content: "current user",
		})
	}
	return NewLlmAgent(LlmAgentConfig{
		Client: client,
		LLM: llm.Config{
			Model: "test-model", Provider: "openrouter",
			ContextWindow: 128_000, MaxTokens: 256, TotalTimeoutSec: 30,
		},
		Registry: registry, PreviewCap: 1024, RunDir: t.TempDir(),
		SessionID:   uuid.Must(uuid.NewV7()).String(),
		UserTurns:   userTurns,
		HookManager: hooks,
		DynamicTail: exposure,
	})
}

func validDynamicTailExposure() *DynamicTailExposure {
	before := uint64(42)
	after := uint64(42)
	exposure := &DynamicTailExposure{
		Tail: DynamicTail{
			ID:                "dynamic-tail-test",
			Content:           "<memory>exact recalled bytes</memory>",
			BeforeCurrentUser: true,
		},
		HardCapTokens: 100_000,
		Included:      true,
		Results: []DynamicTailResult{
			{
				Kind:  "memory_preference",
				ID:    "11111111-1111-4111-8111-111111111111",
				Order: 0,
			},
			{
				Kind:  "memory_entity",
				ID:    "22222222-2222-4222-8222-222222222222",
				Order: 1,
			},
		},
		Limits: map[string]DynamicTailLimit{
			"memory_preference": {RequestedK: 4, EffectiveK: 4, Count: 1},
			"memory_entity":     {RequestedK: 4, EffectiveK: 4, Count: 1},
		},
		Revisions: DynamicTailRevisions{
			Retriever: "neo4j-agent-memory-long-term-v1",
			Reranker:  "none-v1",
			Embedding: "ollama-granite-embedding-278m@768",
			Index:     "entity-embedding-index-v1",
		},
		CorpusEpochBefore: &before,
		CorpusEpochAfter:  &after,
		Coherent:          true,
	}
	exposure.ValidateRequest = func(messages []llm.Message, hardCap int) error {
		return conversations.ValidateDynamicTailMessages(
			messages,
			conversations.DynamicTail{
				ID:                exposure.Tail.ID,
				Content:           exposure.Tail.Content,
				BeforeCurrentUser: exposure.Tail.BeforeCurrentUser,
			},
			hardCap,
		)
	}
	return exposure
}

func runDynamicTailAgent(t *testing.T, agent *LlmAgent) {
	t.Helper()
	maxSteps := 4
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	requestID := uuid.Must(uuid.NewV7())
	for _, runErr := range agent.Run(InvocationContext{
		Ctx: context.Background(), Agent: agent,
		RequestID: requestID, Branch: "root", Budget: budget,
	}) {
		if runErr != nil {
			t.Fatalf("Run: %v", runErr)
		}
	}
}

func countDynamicTail(messages []llm.Message, content string) int {
	count := 0
	for _, message := range messages {
		if message.Content == content {
			count++
		}
	}
	return count
}

func dynamicTailTextTurn(finish, text string) dynamicTailTurn {
	return dynamicTailTurn{chunks: []llm.Chunk{
		{Text: text},
		{FinishReason: finish},
	}}
}

func dynamicTailToolCallTurn(call llm.ToolCall) dynamicTailTurn {
	return dynamicTailTurn{chunks: []llm.Chunk{
		{ToolCall: &call},
		{FinishReason: "tool_calls"},
	}}
}

func dynamicTailToolCall(id, name, arguments string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}
