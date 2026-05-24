package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
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

type fakeAskUserWebChatLLM struct {
	mu       sync.Mutex
	requests []llm.Request
	calls    int
}

func (f *fakeAskUserWebChatLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.requests = append(f.requests, cloneLLMRequest(req))
	if f.calls == 1 {
		return llm.Response{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{{
				ID:   "ask-1",
				Name: "ask_user",
				Arguments: map[string]any{
					"question": "Which option?",
					"options":  []any{"alpha", "beta"},
					"kind":     "selection",
				},
			}},
			Usage: llm.TokenUsage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10},
		}, nil
	}
	return llm.Response{
		Content: "resumed with beta",
		Usage:   llm.TokenUsage{PromptTokens: 9, CompletionTokens: 3, TotalTokens: 12},
	}, nil
}

func (f *fakeAskUserWebChatLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

func (f *fakeAskUserWebChatLLM) Requests() []llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]llm.Request, len(f.requests))
	for i, req := range f.requests {
		out[i] = cloneLLMRequest(req)
	}
	return out
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

func newTestWebChatAdapter(t *testing.T, llmClient llm.Client, toolRegistry *toolregistry.Registry) (*apiChatServiceAdapter, *sql.DB) {
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
	if toolRegistry == nil {
		toolRegistry = toolregistry.NewRegistry(logger)
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
			Tools:    toolRegistry,
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

func containsToolResult(messages []llm.Message, callID, content string) bool {
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID == callID && msg.Content == content {
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
