package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/agent"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
	runstore "github.com/aura/aura/internal/storage/runs"
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/testutil"
)

type fakeWebChatLLM struct{}

func (fakeWebChatLLM) Send(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{
		Content: "WEB_HUB_OK",
		Usage: llm.TokenUsage{
			PromptTokens:     11,
			CompletionTokens: 3,
			TotalTokens:      14,
		},
	}, nil
}

func (fakeWebChatLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

type recordingWebChatLLM struct {
	mu        sync.Mutex
	requests  []llm.Request
	responses []llm.Response
}

func (f *recordingWebChatLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, cloneLLMRequest(req))
	idx := len(f.requests) - 1
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	return llm.Response{
		Content: "recording fallback",
		Usage:   llm.TokenUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}, nil
}

func (f *recordingWebChatLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

func (f *recordingWebChatLLM) Requests() []llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]llm.Request, len(f.requests))
	for i, req := range f.requests {
		out[i] = cloneLLMRequest(req)
	}
	return out
}

func cloneLLMRequest(req llm.Request) llm.Request {
	req.Messages = llm.CloneMessages(req.Messages)
	req.Tools = append([]llm.ToolDefinition(nil), req.Tools...)
	return req
}

type fakeToolCallingLLM struct {
	calls int
}

func (f *fakeToolCallingLLM) Send(context.Context, llm.Request) (llm.Response, error) {
	f.calls++
	if f.calls == 1 {
		return llm.Response{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "call-1", Name: "context_probe"}},
		}, nil
	}
	return llm.Response{Content: "adapter ok"}, nil
}

func (f *fakeToolCallingLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

type fakeTextResponseLLM struct {
	calls int
}

func (f *fakeTextResponseLLM) Send(context.Context, llm.Request) (llm.Response, error) {
	f.calls++
	return llm.Response{
		HasToolCalls: true,
		ToolCalls: []llm.ToolCall{{
			ID:        "call-text",
			Name:      "text_response",
			Arguments: map[string]any{"text": "  Ciao Davide, fatto.  "},
		}},
		Usage: llm.TokenUsage{PromptTokens: 7, CompletionTokens: 5, TotalTokens: 12},
	}, nil
}

func (f *fakeTextResponseLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

type fakeStreamingWebChatLLM struct {
	mu           sync.Mutex
	sendCalled   bool
	streamCalled bool
	requests     []llm.Request
}

func (f *fakeStreamingWebChatLLM) Send(context.Context, llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalled = true
	return llm.Response{Content: "send fallback"}, nil
}

func (f *fakeStreamingWebChatLLM) Stream(_ context.Context, req llm.Request) (<-chan llm.Token, error) {
	f.mu.Lock()
	f.streamCalled = true
	f.requests = append(f.requests, cloneLLMRequest(req))
	f.mu.Unlock()
	ch := make(chan llm.Token, 3)
	ch <- llm.Token{Content: "Hel"}
	ch <- llm.Token{Content: "lo"}
	ch <- llm.Token{Done: true, Usage: llm.TokenUsage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}}
	close(ch)
	return ch, nil
}

func (f *fakeStreamingWebChatLLM) Snapshot() (streamCalled bool, sendCalled bool, requests []llm.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]llm.Request, len(f.requests))
	for i, req := range f.requests {
		out[i] = cloneLLMRequest(req)
	}
	return f.streamCalled, f.sendCalled, out
}

func TestHubBackedWebChatPersistsRunAndActor(t *testing.T) {
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := newTestWebChatService(
		&config.Config{LLMModel: "fake-web-chat", AgentLoopMaxSteps: 4},
		&telegram.Deps{
			LLM:      fakeWebChatLLM{},
			RunStore: runStore,
			Tools:    toolregistry.NewRegistry(logger),
			Logger:   logger,
		},
		logger,
	)
	if err != nil {
		t.Fatalf("newTestWebChatService: %v", err)
	}
	if svc == nil {
		t.Fatal("newTestWebChatService returned nil")
	}
	actorID := identity.TelegramSessionActorID("alice")
	reply, err := svc.Chat(identity.WithActorID(context.Background(), actorID), "alice", "default", "hello")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Reply != "WEB_HUB_OK" || reply.LLMCalls != 1 || reply.ToolCalls != 0 || reply.Tokens != 14 {
		t.Fatalf("reply = %+v", reply)
	}
	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM runs
WHERE actor_id = ? AND channel = 'web' AND status = 'completed' AND final_text_preview = 'WEB_HUB_OK'
`, 1, actorID)
	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM run_events
WHERE actor_id = ? AND type IN ('run_started', 'message_done', 'usage', 'done')
`, 4, actorID)
}

func TestWebChatStreamUsesLLMStreamAndUIMessageFrames(t *testing.T) {
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	llmClient := &fakeStreamingWebChatLLM{}
	shared, err := newSharedChatHub(
		&config.Config{LLMModel: "fake-stream", AuraBotMaxIterations: 2},
		&telegram.Deps{
			LLM:      llmClient,
			RunStore: runStore,
			Tools:    toolregistry.NewRegistry(logger),
			Logger:   logger,
		},
		nil,
		logger,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("newSharedChatHub: %v", err)
	}
	svc := newWebStreamChatService(shared.hub, shared.webStreamRouter)
	if svc == nil {
		t.Fatal("newWebStreamChatService returned nil")
	}
	var buf bytes.Buffer
	flushes := 0
	if err := svc.ChatStream(context.Background(), "alice", "default", "hello stream", &buf, func() { flushes++ }); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	streamCalled, sendCalled, requests := llmClient.Snapshot()
	if !streamCalled || sendCalled {
		t.Fatalf("streamCalled=%v sendCalled=%v", streamCalled, sendCalled)
	}
	if len(requests) != 1 || !containsMessageContent(requests[0].Messages, "hello stream") {
		t.Fatalf("requests = %+v", requests)
	}
	if flushes < 4 {
		t.Fatalf("flushes = %d, want >=4", flushes)
	}
	body := buf.String()
	if !strings.Contains(body, `"type":"text-delta"`) || !strings.Contains(body, `"textDelta":"Hel"`) || !strings.Contains(body, `"textDelta":"lo"`) {
		t.Fatalf("stream body missing text deltas:\n%s", body)
	}
	if strings.Count(body, `"type":"text-delta"`) != 2 {
		t.Fatalf("text-delta frame count = %d, body:\n%s", strings.Count(body, `"type":"text-delta"`), body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("stream missing [DONE]:\n%s", body)
	}
	if !strings.Contains(body, `"type":"start"`) || !strings.Contains(body, `"type":"finish"`) {
		t.Fatalf("stream missing start/finish frames:\n%s", body)
	}
	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM runs
WHERE channel = 'web' AND thread_id = 'web:alice:default' AND status = 'completed'
`, 1)
}

func TestHubBackedWebChatReportsSoftBudgetWarning(t *testing.T) {
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := budget.NewTracker(budget.Config{
		SoftBudget:           0.000001,
		HardBudget:           1,
		InputCostPerMTokens:  1,
		OutputCostPerMTokens: 1,
	}, logger)
	svc, err := newTestWebChatService(
		&config.Config{
			LLMModel:             "fake-web-chat",
			AuraBotMaxIterations: 4,
			CostInputPerMTokens:  1,
			CostOutputPerMTokens: 1,
		},
		&telegram.Deps{
			LLM:      fakeWebChatLLM{},
			RunStore: runStore,
			Tools:    toolregistry.NewRegistry(logger),
			Logger:   logger,
			Budget:   tracker,
		},
		logger,
	)
	if err != nil {
		t.Fatalf("newTestWebChatService: %v", err)
	}

	reply, err := svc.Chat(context.Background(), "alice", "default", "hello")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(reply.BudgetWarning, "Soft budget reached") {
		t.Fatalf("BudgetWarning = %q", reply.BudgetWarning)
	}
}

func TestHubBackedWebChatArchivesConversationTurns(t *testing.T) {
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	archiveStore, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := newTestWebChatServiceWithArchive(
		&config.Config{LLMModel: "fake-web-chat", AuraBotMaxIterations: 4},
		&telegram.Deps{
			LLM:      fakeWebChatLLM{},
			RunStore: runStore,
			Tools:    toolregistry.NewRegistry(logger),
			Logger:   logger,
		},
		logger,
		archiveStore,
		archiveStore,
	)
	if err != nil {
		t.Fatalf("newTestWebChatServiceWithArchive: %v", err)
	}
	if _, err := svc.Chat(context.Background(), "alice", "default", "archive this web turn"); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM conversations
WHERE channel = 'web' AND role = 'user' AND content = 'archive this web turn'
`, 1)
	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM conversations
WHERE channel = 'web' AND role = 'assistant' AND content = 'WEB_HUB_OK' AND llm_calls = 1
`, 1)
}

func TestHubBackedWebChatRecordsToolAttempts(t *testing.T) {
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	probe := &webChatContextProbeTool{}
	registry.Register(probe)
	svc, err := newTestWebChatService(
		&config.Config{LLMModel: "fake-web-chat", AgentLoopMaxSteps: 4},
		&telegram.Deps{
			LLM:      &fakeToolCallingLLM{},
			Pool:     db,
			RunStore: runStore,
			Tools:    registry,
			Logger:   logger,
		},
		logger,
	)
	if err != nil {
		t.Fatalf("newTestWebChatService: %v", err)
	}
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)
	if _, err := svc.Chat(ctx, "alice", "default", "call the probe"); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !slices.Contains(probe.allowed, "context_probe") {
		t.Fatalf("allowed = %+v, want context_probe visible", probe.allowed)
	}
	if probe.userID != "alice" {
		t.Fatalf("userID = %q, want alice", probe.userID)
	}
	if probe.conversationID != "web:alice:default" {
		t.Fatalf("conversationID = %q, want web:alice:default", probe.conversationID)
	}
	if probe.runID == "" {
		t.Fatal("probe runID is empty")
	}
	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM tool_attempts ta
JOIN runs r ON r.id = ta.run_id
WHERE r.channel = 'web' AND ta.tool_name = 'context_probe' AND ta.outcome = 'ok'
`, 1)
}

func TestHubBackedWebChatTerminatesOnTextResponseTool(t *testing.T) {
	t.Setenv("AURA_TOOL_ALLOWLIST", "")
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	registry.Register(&toolregistry.TextResponseTool{})
	llmClient := &fakeTextResponseLLM{}
	svc, err := newTestWebChatService(
		&config.Config{LLMModel: "fake-web-chat", AgentLoopMaxSteps: 4, TerminalToolPolicy: "on"},
		&telegram.Deps{
			LLM:      llmClient,
			Pool:     db,
			RunStore: runStore,
			Tools:    registry,
			Logger:   logger,
		},
		logger,
	)
	if err != nil {
		t.Fatalf("newTestWebChatService: %v", err)
	}
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)
	reply, err := svc.Chat(ctx, "alice", "default", "rispondi via text_response")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Reply != "Ciao Davide, fatto." {
		t.Fatalf("reply = %q, want terminal text_response text", reply.Reply)
	}
	if llmClient.calls != 1 {
		t.Fatalf("LLM calls = %d, want 1 terminal turn", llmClient.calls)
	}
	assertWebChatScalar(t, db, `
SELECT COUNT(*)
FROM tool_attempts ta
JOIN runs r ON r.id = ta.run_id
WHERE r.channel = 'web' AND ta.tool_name = 'text_response' AND ta.outcome = 'ok'
`, 1)
}

func TestHubBackedWebChatUsesSessionStoreContext(t *testing.T) {
	llmClient := &recordingWebChatLLM{responses: []llm.Response{
		{Content: "answer from first turn", Usage: llm.TokenUsage{TotalTokens: 10}},
		{Content: "answer from second turn", Usage: llm.TokenUsage{TotalTokens: 20}},
		{Content: "answer from third turn", Usage: llm.TokenUsage{TotalTokens: 30}},
	}}
	adapter, _ := newTestWebChatAdapter(t, llmClient, nil)
	ctx := identity.WithActorID(context.Background(), identity.TelegramSessionActorID("alice"))

	for _, message := range []string{"first web turn", "second web turn", "third web turn"} {
		if _, err := adapter.Chat(ctx, "alice", "default", message); err != nil {
			t.Fatalf("Chat(%q): %v", message, err)
		}
	}

	reqs := llmClient.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	thirdPrompt := reqs[2].Messages
	for _, want := range []string{
		"first web turn",
		"answer from first turn",
		"second web turn",
		"answer from second turn",
		"third web turn",
	} {
		if !containsMessageContent(thirdPrompt, want) {
			t.Fatalf("third prompt missing %q\nmessages=%+v", want, thirdPrompt)
		}
	}
	convCtx, ok := adapter.sessionStore.Load("web:alice:default")
	if !ok {
		t.Fatal("session store missing web:alice:default")
	}
	if got := convCtx.TotalTokensUsed(); got != 60 {
		t.Fatalf("TotalTokensUsed = %d, want 60", got)
	}
	if adapter.sessionStore.IsActive("web:alice:default") {
		t.Fatal("web session active marker was not cleared after final event")
	}
}

func TestHubBackedWebChatSessionStoreScopesThreads(t *testing.T) {
	llmClient := &recordingWebChatLLM{responses: []llm.Response{
		{Content: "answer from thread a"},
		{Content: "answer from thread b"},
		{Content: "follow-up from thread a"},
	}}
	adapter, _ := newTestWebChatAdapter(t, llmClient, nil)
	ctx := identity.WithActorID(context.Background(), identity.TelegramSessionActorID("alice"))

	if _, err := adapter.Chat(ctx, "alice", "thread-a", "hello a"); err != nil {
		t.Fatalf("Chat thread-a: %v", err)
	}
	if _, err := adapter.Chat(ctx, "alice", "thread-b", "hello b"); err != nil {
		t.Fatalf("Chat thread-b: %v", err)
	}
	if _, err := adapter.Chat(ctx, "alice", "thread-a", "next a"); err != nil {
		t.Fatalf("Chat thread-a follow-up: %v", err)
	}

	reqs := llmClient.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	threadAFollowUp := reqs[2].Messages
	if !containsMessageContent(threadAFollowUp, "answer from thread a") {
		t.Fatal("thread-a follow-up did not retain thread-a history")
	}
	if containsMessageContent(threadAFollowUp, "answer from thread b") {
		t.Fatal("thread-a follow-up inherited thread-b history")
	}
	if _, ok := adapter.sessionStore.Load("web:alice:thread-a"); !ok {
		t.Fatal("session store missing thread-a context")
	}
	if _, ok := adapter.sessionStore.Load("web:alice:thread-b"); !ok {
		t.Fatal("session store missing thread-b context")
	}
}

func TestHubBackedWebChatCompactsToolResultsBeforeNextTurn(t *testing.T) {
	llmClient := &recordingWebChatLLM{responses: []llm.Response{
		{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "call-large-1", Name: "large_context_probe"},
				{ID: "call-large-2", Name: "large_context_probe"},
				{ID: "call-large-3", Name: "large_context_probe"},
			},
		},
		{Content: "tool turn complete"},
		{Content: "next turn complete"},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	registry.Register(largeContextProbeTool{})
	adapter, _ := newTestWebChatAdapter(t, llmClient, registry)
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)

	if _, err := adapter.Chat(ctx, "alice", "default", "call the large probe"); err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if _, err := adapter.Chat(ctx, "alice", "default", "continue after compaction"); err != nil {
		t.Fatalf("second Chat: %v", err)
	}

	reqs := llmClient.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	toolMessages := filterToolMessages(reqs[2].Messages)
	if len(toolMessages) != 3 {
		t.Fatalf("tool messages = %d, want 3\nmessages=%+v", len(toolMessages), reqs[2].Messages)
	}
	var compacted int
	for _, msg := range toolMessages {
		if strings.Contains(msg.Content, "[tool result compacted]") {
			compacted++
			if len(msg.Content) > 1200 {
				t.Fatalf("compacted tool result length = %d, want <=1200", len(msg.Content))
			}
		}
	}
	if compacted != 1 {
		t.Fatalf("compacted tool results = %d, want 1", compacted)
	}
}

type webChatAllowAuthorizer struct{}

func (webChatAllowAuthorizer) Authorize(_ context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return identity.AuthorizationDecision{
		ActorID:    params.ActorID,
		Capability: params.Capability,
		Resource:   params.Resource,
		Decision:   identity.DecisionAllow,
		Reason:     "test_allow",
	}, nil
}

type webChatContextProbeTool struct {
	allowed        []string
	userID         string
	conversationID string
	runID          string
}

func (t *webChatContextProbeTool) Name() string { return "context_probe" }
func (t *webChatContextProbeTool) Description() string {
	return "records tool execution context"
}
func (t *webChatContextProbeTool) Parameters() map[string]any { return map[string]any{} }
func (t *webChatContextProbeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	t.allowed = toolregistry.AllowedToolNamesFromContext(ctx)
	t.userID = toolregistry.UserIDFromContext(ctx)
	t.conversationID = toolregistry.ConversationIDFromContext(ctx)
	t.runID = identity.RunIDFromContext(ctx)
	return "ok", nil
}

type largeContextProbeTool struct{}

func (largeContextProbeTool) Name() string { return "large_context_probe" }
func (largeContextProbeTool) Description() string {
	return "returns a large deterministic payload"
}
func (largeContextProbeTool) Parameters() map[string]any { return map[string]any{} }
func (largeContextProbeTool) Execute(context.Context, map[string]any) (string, error) {
	return strings.Repeat("large-result-payload ", 220), nil
}

func TestExtractLastTextResponseArgUsesNewestNonEmptyCall(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				Name:      "text_response",
				Arguments: map[string]any{"text": "old"},
			}},
		},
		{Role: "tool", Content: "wrapped old result", ToolCallID: "old"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{Name: "search_memory", Arguments: map[string]any{"query": "ignored"}},
				{Name: "text_response", Arguments: map[string]any{"text": "   "}},
				{Name: "text_response", Arguments: map[string]any{"text": "  latest  "}},
			},
		},
	}
	if got := extractLastTextResponseArg(msgs); got != "latest" {
		t.Fatalf("extractLastTextResponseArg() = %q, want latest", got)
	}
}

func TestExtractLastTextResponseArgIgnoresWrongShapes(t *testing.T) {
	msgs := []llm.Message{
		{Role: "tool", Content: `{"text":"not assistant"}`},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{Name: "text_response", Arguments: map[string]any{"text": 123}},
				{Name: "search_memory", Arguments: map[string]any{"text": "wrong tool"}},
			},
		},
	}
	if got := extractLastTextResponseArg(msgs); got != "" {
		t.Fatalf("extractLastTextResponseArg() = %q, want empty", got)
	}
}

func TestAgentJobRunnerAdapterPassesRequestRunID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	probe := &webChatContextProbeTool{}
	registry.Register(probe)
	adapter := &agentJobRunnerAdapter{getDeps: func() agent.RunTaskDeps {
		return agent.RunTaskDeps{
			LLM:           &fakeToolCallingLLM{},
			Tools:         registry,
			MaxIterations: 3,
			Logger:        logger,
		}
	}}
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)
	_, err := adapter.RunJob(ctx, cron.JobRequest{
		RunID:         "cron-run-123",
		Prompt:        "call the probe",
		ToolAllowlist: []string{"context_probe"},
		UserID:        "alice",
	})
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	if probe.runID != "cron-run-123" {
		t.Fatalf("probe runID = %q, want cron-run-123", probe.runID)
	}
}

func TestSwarmRunnerAdapterPassesContextRunID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	probe := &webChatContextProbeTool{}
	registry.Register(probe)
	adapter := &swarmRunnerAdapter{getDeps: func() agent.RunTaskDeps {
		return agent.RunTaskDeps{
			LLM:           &fakeToolCallingLLM{},
			Tools:         registry,
			MaxIterations: 3,
			Logger:        logger,
		}
	}}
	ctx := identity.WithRunID(
		identity.WithActorID(
			identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
			identity.TelegramSessionActorID("alice"),
		),
		"swarm-run-123",
	)
	_, err := adapter.Run(ctx, agent.Task{
		Prompt:        "call the probe",
		ToolAllowlist: []string{"context_probe"},
		UserID:        "alice",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if probe.runID != "swarm-run-123" {
		t.Fatalf("probe runID = %q, want swarm-run-123", probe.runID)
	}
}

func newTestWebChatService(cfg *config.Config, deps *telegram.Deps, logger *slog.Logger) (api.ChatService, error) {
	return newTestWebChatServiceWithArchive(cfg, deps, logger, nil, nil)
}

func newTestWebChatServiceWithArchive(
	cfg *config.Config,
	deps *telegram.Deps,
	logger *slog.Logger,
	archiveRepo conversation.ArchiveRepository,
	archiveAppender conversation.TurnAppender,
) (api.ChatService, error) {
	shared, err := newSharedChatHub(cfg, deps, nil, logger, archiveRepo, archiveAppender)
	if err != nil {
		return nil, err
	}
	return newWebChatService(shared.hub, shared.webRouter, shared.webSessionStore)
}

func newTestWebChatAdapter(t *testing.T, llmClient llm.Client, registry *toolregistry.Registry) (*apiChatServiceAdapter, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if registry == nil {
		registry = toolregistry.NewRegistry(logger)
	}
	svc, err := newTestWebChatService(
		&config.Config{
			LLMModel:             "fake-web-chat",
			AuraBotMaxIterations: 4,
			MaxHistoryMessages:   50,
			MaxContextTokens:     16000,
			TerminalToolPolicy:   "on",
		},
		&telegram.Deps{
			LLM:      llmClient,
			Pool:     db,
			RunStore: runStore,
			Tools:    registry,
			Logger:   logger,
		},
		logger,
	)
	if err != nil {
		t.Fatalf("newTestWebChatService: %v", err)
	}
	adapter, ok := svc.(*apiChatServiceAdapter)
	if !ok {
		t.Fatalf("service type = %T, want *apiChatServiceAdapter", svc)
	}
	return adapter, db
}

func assertWebChatScalar(t *testing.T, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query scalar: %v\nquery: %s", err, query)
	}
	if got != want {
		t.Fatalf("scalar = %d, want %d\nquery: %s", got, want, query)
	}
}

func containsMessageContent(messages []llm.Message, want string) bool {
	for _, msg := range messages {
		if msg.Content == want {
			return true
		}
	}
	return false
}

func filterToolMessages(messages []llm.Message) []llm.Message {
	var out []llm.Message
	for _, msg := range messages {
		if msg.Role == "tool" {
			out = append(out, msg)
		}
	}
	return out
}
