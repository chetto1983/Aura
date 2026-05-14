package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/chat"
	webadapter "github.com/aura/aura/internal/channels/web"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/agent/tools/registry"
)

// chatPipeService adapts agent.Runner to api.ChatService so cmd/chat (the
// local CLI pipe) can drive a turn against the same live LLM + tool
// registry the Telegram bot uses. The session state is in-memory and
// process-local; it intentionally does NOT share the SessionStore /
// UserGate that the Telegram path owns, otherwise a chat-pipe turn would
// serialize behind a live Telegram message for the same user.
//
// Both runner and tools registry are reused from the bot so a /api/chat
// turn sees the same tool surface a Telegram turn would. The system prompt
// is the canonical conversation.RenderSystemPrompt() — any agent entry
// point that drops it back to a "be conversational" stub silently strips
// Aura's tool-awareness (see memory: feedback_agent_must_know_tools_exist).
type chatPipeService struct {
	runner *agent.Runner
	tools  *tools.Registry

	mu       sync.Mutex
	sessions map[string]*chatPipeSession
}

type chatPipeSession struct {
	messages    []llm.Message
	updatedAt   time.Time
}

const (
	chatPipeMaxMessages = 30
	chatPipeIdleTTL     = 30 * time.Minute
)

// NewChatPipeService wires the chat pipe against an existing runner + tools
// registry. A nil runner returns nil so cmd/aura's wiring can pass through
// unconditionally. The registry is the same one the Telegram bot serves
// from, so /api/chat and Telegram see identical tools.
func NewChatPipeService(runner *agent.Runner, registry *tools.Registry) api.ChatService {
	if runner == nil {
		return nil
	}
	return &chatPipeService{
		runner:   runner,
		tools:    registry,
		sessions: make(map[string]*chatPipeSession),
	}
}

func (s *chatPipeService) Chat(ctx context.Context, userID, message string) (api.ChatReply, error) {
	if s == nil || s.runner == nil {
		return api.ChatReply{}, errors.New("chat pipe: runner unavailable")
	}
	session := s.acquireSession(userID)
	session.messages = append(session.messages, llm.Message{Role: "user", Content: message})

	var allowlist []string
	if s.tools != nil {
		allowlist = s.tools.Names()
	}

	task := agent.Task{
		SystemPrompt:  conversation.RenderSystemPrompt(time.Now(), time.Local),
		Messages:      append([]llm.Message(nil), session.messages...),
		ToolAllowlist: allowlist,
		UserID:        userID,
	}
	result, err := s.runner.Run(ctx, task)
	if err != nil {
		// Roll back the user message so a retry does not double-up
		// when the runner errored before producing a real exchange.
		s.rollbackLastUser(userID)
		return api.ChatReply{}, fmt.Errorf("agent run: %w", err)
	}

	reply := result.Content
	session.messages = append(session.messages, llm.Message{Role: "assistant", Content: reply})
	if len(session.messages) > chatPipeMaxMessages {
		drop := len(session.messages) - chatPipeMaxMessages
		session.messages = append([]llm.Message(nil), session.messages[drop:]...)
	}
	session.updatedAt = time.Now()
	return api.ChatReply{
		Reply:     reply,
		ElapsedMs: result.Elapsed.Milliseconds(),
		LLMCalls:  result.LLMCalls,
		ToolCalls: result.ToolCalls,
		Tokens:    result.Tokens.TotalTokens,
	}, nil
}

func (s *chatPipeService) acquireSession(userID string) *chatPipeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	sess, ok := s.sessions[userID]
	if !ok {
		sess = &chatPipeSession{updatedAt: time.Now()}
		s.sessions[userID] = sess
	}
	return sess
}

func (s *chatPipeService) rollbackLastUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[userID]
	if !ok || len(sess.messages) == 0 {
		return
	}
	last := sess.messages[len(sess.messages)-1]
	if last.Role == "user" {
		sess.messages = sess.messages[:len(sess.messages)-1]
	}
}

// gcLocked evicts idle sessions so a long-lived bot does not accumulate
// per-user in-memory history forever. Caller must hold s.mu.
func (s *chatPipeService) gcLocked() {
	cutoff := time.Now().Add(-chatPipeIdleTTL)
	for id, sess := range s.sessions {
		if sess.updatedAt.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}

// buildChatService returns the api.ChatService to mount on /api/chat.
//
// AURA_USE_HUB=true: route through chat.Hub + web adapter (experimental).
// The Hub's AgentLoop delegates to a chatPipeService so session history and
// tool access are identical to the legacy path.  Default is false — Telegram
// stays on the legacy path until US-704 flips the default.
//
// AURA_USE_HUB=false (default): returns a chatPipeService directly.
func buildChatService(runner *agent.Runner, registry *tools.Registry, logger *slog.Logger) api.ChatService {
	if os.Getenv("AURA_USE_HUB") != "true" {
		return NewChatPipeService(runner, registry)
	}

	// Hub-based web path.
	inner := NewChatPipeService(runner, registry)
	loop := &chatServiceAgentLoop{inner: inner}
	hub, err := chat.New(chat.Config{Loop: loop, Logger: logger})
	if err != nil {
		if logger != nil {
			logger.Warn("AURA_USE_HUB: failed to create hub, falling back to legacy", "error", err)
		}
		return inner
	}
	router := webadapter.NewRouter()
	hub.RegisterOutbound(router)
	webSvc := webadapter.NewChatService(hub, router)
	return &hubChatServiceAdapter{inner: webSvc}
}

// chatServiceAgentLoop bridges api.ChatService (chatPipeService) into the
// chat.AgentLoop interface so chat.Hub can drive it.  Session management
// (per-userID message history) lives inside the wrapped chatPipeService.
type chatServiceAgentLoop struct {
	inner api.ChatService
}

func (l *chatServiceAgentLoop) Run(ctx context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
	reply, err := l.inner.Chat(ctx, msg.PrincipalID, msg.Text)
	if err == nil || reply.Reply != "" {
		_ = emit(chat.OutboundEvent{
			Type:    chat.EventMessageDone,
			Content: reply.Reply,
			Payload: map[string]any{"delivered": err == nil},
		})
	}
	if err == nil {
		_ = emit(chat.OutboundEvent{
			Type: chat.EventUsage,
			Payload: map[string]any{
				"llm_calls":    reply.LLMCalls,
				"tool_calls":   reply.ToolCalls,
				"tokens_total": reply.Tokens,
			},
		})
	}
	return err
}

// hubChatServiceAdapter converts webadapter.ChatReply → api.ChatReply so
// webadapter.ChatService satisfies api.ChatService without an import cycle.
type hubChatServiceAdapter struct {
	inner *webadapter.ChatService
}

func (a *hubChatServiceAdapter) Chat(ctx context.Context, userID, message string) (api.ChatReply, error) {
	r, err := a.inner.Chat(ctx, userID, message)
	return api.ChatReply{
		Reply:     r.Reply,
		ElapsedMs: r.ElapsedMs,
		LLMCalls:  r.LLMCalls,
		ToolCalls: r.ToolCalls,
		Tokens:    r.Tokens,
	}, err
}
