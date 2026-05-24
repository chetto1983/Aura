package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
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

func TestWebChatAskUserPendingAndAnswerResume(t *testing.T) {
	llmClient := &fakeAskUserWebChatLLM{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := toolregistry.NewRegistry(logger)
	registry.Register(&toolregistry.AskUserTool{})
	adapter, db := newTestWebChatAdapter(t, llmClient, registry)
	ctx := identity.WithActorID(
		identity.WithAuthorizer(context.Background(), webChatAllowAuthorizer{}),
		identity.TelegramSessionActorID("alice"),
	)

	first, err := adapter.Chat(ctx, "alice", "default", "needs a choice")
	if err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if first.PendingQuestion == nil {
		t.Fatalf("PendingQuestion nil in %#v", first)
	}
	if first.PendingQuestion.Question != "Which option?" || first.PendingQuestion.Kind != "selection" {
		t.Fatalf("PendingQuestion = %+v", first.PendingQuestion)
	}
	if got := strings.Join(first.PendingQuestion.Options, ","); got != "alpha,beta" {
		t.Fatalf("options = %q", got)
	}
	assertWebChatScalar(t, db, `SELECT COUNT(*) FROM chat_questions WHERE id = ? AND status = 'waiting' AND thread_id = 'web:alice:default' AND channel = 'web'`, 1, first.PendingQuestion.ID)

	second, err := adapter.AnswerChat(ctx, "alice", "default", first.PendingQuestion.ID, "2", []string{"2"})
	if err != nil {
		t.Fatalf("AnswerChat: %v", err)
	}
	if second.Reply != "resumed with beta" {
		t.Fatalf("reply = %q", second.Reply)
	}
	assertWebChatScalar(t, db, `SELECT COUNT(*) FROM chat_questions WHERE id = ? AND status = 'answered' AND answer_run_id <> ''`, 1, first.PendingQuestion.ID)
	assertWebChatScalar(t, db, `SELECT COUNT(*) FROM run_events WHERE type = 'question_answered' AND causation_id = ?`, 1, first.PendingQuestion.ID)

	requests := llmClient.Requests()
	if len(requests) < 2 {
		t.Fatalf("LLM requests = %d, want >=2", len(requests))
	}
	if !containsToolResult(requests[len(requests)-1].Messages, "ask-1", "beta") {
		t.Fatalf("resume request missing ask_user tool result: %+v", requests[len(requests)-1].Messages)
	}
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
