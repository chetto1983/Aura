package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

// webChatService calls agent.RunTask for the /api/chat endpoint.
// Session state is in-memory and process-local; it deliberately does NOT share
// the SessionStore / UserGate that the Telegram path owns.
type webChatService struct {
	depsGetter func() agent.RunTaskDeps

	mu       sync.Mutex
	sessions map[string]*webChatSession
}

type webChatSession struct {
	messages  []llm.Message
	updatedAt time.Time
}

const (
	webChatMaxMessages = 30
	webChatIdleTTL     = 30 * time.Minute
)

// NewWebChatService wires the web chat service with a lazy deps builder.
// A nil getter returns nil so the caller can pass through unconditionally when
// no LLM is configured.
func NewWebChatService(depsGetter func() agent.RunTaskDeps) ChatService {
	if depsGetter == nil {
		return nil
	}
	return &webChatService{
		depsGetter: depsGetter,
		sessions:   make(map[string]*webChatSession),
	}
}

func (s *webChatService) Chat(ctx context.Context, userID, message string) (ChatReply, error) {
	if s == nil || s.depsGetter == nil {
		return ChatReply{}, errors.New("web chat: runner unavailable")
	}
	session := s.acquireSession(userID)
	session.messages = append(session.messages, llm.Message{Role: "user", Content: message})

	deps := s.depsGetter()
	var allowlist []string
	if deps.Tools != nil {
		allowlist = deps.Tools.Names()
	}

	task := agent.Task{
		SystemPrompt:  conversation.RenderSystemPrompt(time.Now(), time.Local),
		Messages:      append([]llm.Message(nil), session.messages...),
		ToolAllowlist: allowlist,
		UserID:        userID,
	}
	result, err := agent.RunTask(ctx, deps, task)
	if err != nil {
		s.rollbackLastUser(userID)
		return ChatReply{}, fmt.Errorf("agent run: %w", err)
	}

	reply := result.Content
	session.messages = append(session.messages, llm.Message{Role: "assistant", Content: reply})
	if len(session.messages) > webChatMaxMessages {
		drop := len(session.messages) - webChatMaxMessages
		session.messages = append([]llm.Message(nil), session.messages[drop:]...)
	}
	session.updatedAt = time.Now()
	return ChatReply{
		Reply:     reply,
		ElapsedMs: result.Elapsed.Milliseconds(),
		LLMCalls:  result.LLMCalls,
		ToolCalls: result.ToolCalls,
		Tokens:    result.Tokens.TotalTokens,
	}, nil
}

func (s *webChatService) acquireSession(userID string) *webChatSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	sess, ok := s.sessions[userID]
	if !ok {
		sess = &webChatSession{updatedAt: time.Now()}
		s.sessions[userID] = sess
	}
	return sess
}

func (s *webChatService) rollbackLastUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[userID]
	if !ok || len(sess.messages) == 0 {
		return
	}
	if sess.messages[len(sess.messages)-1].Role == "user" {
		sess.messages = sess.messages[:len(sess.messages)-1]
	}
}

func (s *webChatService) gcLocked() {
	cutoff := time.Now().Add(-webChatIdleTTL)
	for id, sess := range s.sessions {
		if sess.updatedAt.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}
