package agent

import (
	"github.com/aura/aura/internal/llm"
)

// agentState is the State + PhantomCorrector implementation for background
// agents (swarm workers, scheduler jobs, /api/chat pipe). Unlike
// conversation.Context it has no summarisation, sliding window, or Telegram
// coupling. A background agent turn is bounded by MaxIterations/Timeout, so
// the loop owns the message slice directly.
type agentState struct {
	messages []llm.Message
}

var _ State = (*agentState)(nil)
var _ PhantomCorrector = (*agentState)(nil)

func newAgentState(initial []llm.Message) *agentState {
	return &agentState{messages: llm.CloneMessages(initial)}
}

func (s *agentState) Messages() []llm.Message {
	return llm.CloneMessages(s.messages)
}

// TrackTokens is a no-op: loop Stats accumulates token counts directly,
// so the state does not need its own accumulator.
func (s *agentState) TrackTokens(_ llm.TokenUsage) {}

func (s *agentState) AddAssistantMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content})
}

func (s *agentState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}

func (s *agentState) AddToolResultMessage(id, content string) {
	s.messages = append(s.messages, llm.Message{Role: "tool", Content: content, ToolCallID: id})
}

// AddUserMessage satisfies PhantomCorrector so the phantom-tool guard can
// inject corrections when a corrective path is wired.
func (s *agentState) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}
