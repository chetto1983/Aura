package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/aura/aura/internal/agent"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/config"
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
	svc, err := newHubBackedWebChatService(
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
		t.Fatalf("newHubBackedWebChatService: %v", err)
	}
	if svc == nil {
		t.Fatal("newHubBackedWebChatService returned nil")
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
	registry.Register(&webChatContextProbeTool{})
	svc, err := newHubBackedWebChatService(
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
		t.Fatalf("newHubBackedWebChatService: %v", err)
	}
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)
	if _, err := svc.Chat(ctx, "alice", "default", "call the probe"); err != nil {
		t.Fatalf("Chat: %v", err)
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
	svc, err := newHubBackedWebChatService(
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
		t.Fatalf("newHubBackedWebChatService: %v", err)
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
	allowed []string
	userID  string
	runID   string
}

func (t *webChatContextProbeTool) Name() string { return "context_probe" }
func (t *webChatContextProbeTool) Description() string {
	return "records tool execution context"
}
func (t *webChatContextProbeTool) Parameters() map[string]any { return map[string]any{} }
func (t *webChatContextProbeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	t.allowed = toolregistry.AllowedToolNamesFromContext(ctx)
	t.userID = toolregistry.UserIDFromContext(ctx)
	t.runID = identity.RunIDFromContext(ctx)
	return "ok", nil
}

func TestWebToolExecutorCarriesVisibleToolContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	probe := &webChatContextProbeTool{}
	registry.Register(probe)
	state := &webAgentState{}
	exec := &webToolExecutor{
		tools:     registry,
		state:     state,
		logger:    logger,
		allowlist: []string{"context_probe", "other_visible"},
		userID:    "alice",
	}
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)
	summary := exec.ExecuteToolCalls(ctx, []llm.ToolCall{{ID: "call-1", Name: "context_probe"}})
	if summary.Results["call-1"] == "" {
		t.Fatalf("summary = %+v", summary)
	}
	if !slices.Contains(probe.allowed, "context_probe") || !slices.Contains(probe.allowed, "other_visible") {
		t.Fatalf("allowed = %+v", probe.allowed)
	}
	if probe.userID != "alice" {
		t.Fatalf("userID = %q, want alice", probe.userID)
	}
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

func TestWebChatSessionsAreScopedByThread(t *testing.T) {
	sessions := newWebChatSessions()

	threadA := sessions.begin("run-a", "alice", "thread-a", "system a", "hello a")
	threadA.AddAssistantMessage("answer from thread a")
	sessions.commit("run-a", "alice", "thread-a")

	threadB := sessions.begin("run-b", "alice", "thread-b", "system b", "hello b")
	if containsMessageContent(threadB.Messages(), "answer from thread a") {
		t.Fatal("thread-b inherited thread-a history")
	}
	threadB.AddAssistantMessage("answer from thread b")
	sessions.commit("run-b", "alice", "thread-b")

	threadAAgain := sessions.begin("run-c", "alice", "thread-a", "system a2", "next a")
	msgs := threadAAgain.Messages()
	if !containsMessageContent(msgs, "answer from thread a") {
		t.Fatal("thread-a did not retain its own history")
	}
	if containsMessageContent(msgs, "answer from thread b") {
		t.Fatal("thread-a inherited thread-b history")
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
