package telegram

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/llm"
)

// chatPipeService adapts agent.Runner to api.ChatService so cmd/chat (the
// local CLI pipe) can drive a turn against the same live LLM + tool
// registry the Telegram bot uses. The session state is in-memory and
// process-local; it intentionally does NOT share the SessionStore /
// UserGate that the Telegram path owns, otherwise a chat-pipe turn would
// serialize behind a live Telegram message for the same user.
type chatPipeService struct {
	runner *agent.Runner

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

// NewChatPipeService wires the chat pipe against an existing runner. Nil
// runner returns nil so cmd/aura's wiring can pass through unconditionally.
func NewChatPipeService(runner *agent.Runner) api.ChatService {
	if runner == nil {
		return nil
	}
	return &chatPipeService{
		runner:   runner,
		sessions: make(map[string]*chatPipeSession),
	}
}

func (s *chatPipeService) Chat(ctx context.Context, userID, message string) (api.ChatReply, error) {
	if s == nil || s.runner == nil {
		return api.ChatReply{}, errors.New("chat pipe: runner unavailable")
	}
	session := s.acquireSession(userID)
	session.messages = append(session.messages, llm.Message{Role: "user", Content: message})

	task := agent.Task{
		SystemPrompt: "You are Aura — a helpful assistant chatting through the local CLI pipe. Reply in the user's language. Be conversational and concise.",
		Messages:     append([]llm.Message(nil), session.messages...),
		UserID:       userID,
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
