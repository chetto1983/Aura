package agent

import (
	"github.com/aura/aura/internal/llm"
)

// agentState is the State + PhantomCorrector implementation for background
// agents (swarm workers, scheduler jobs, /api/chat pipe). Unlike
// conversation.Context it has no summarisation, sliding window, or Telegram
// coupling — a background agent turn is bounded by MaxIterations / Timeout
// on the Runner, so we let the loop own the message slice directly.
type agentState struct {
	messages []llm.Message
}

var _ State = (*agentState)(nil)
var _ PhantomCorrector = (*agentState)(nil)

func newAgentState(initial []llm.Message) *agentState {
	msgs := make([]llm.Message, len(initial))
	copy(msgs, initial)
	return &agentState{messages: msgs}
}

func (s *agentState) Messages() []llm.Message {
	out := make([]llm.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// TrackTokens is a no-op: agentloop.Stats accumulates token counts directly,
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

// AddUserMessage satisfies agentloop.PhantomCorrector so the phantom-tool
// guard can inject corrections when the Runner has one wired.
func (s *agentState) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}
