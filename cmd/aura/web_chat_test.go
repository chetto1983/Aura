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
	reply, err := svc.Chat(identity.WithActorID(context.Background(), actorID), "alice", "hello")
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
