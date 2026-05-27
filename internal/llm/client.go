// Package llm hosts the streaming LLM client interface and an OpenAI-compatible
// implementation. The interface is provider-neutral so the agent loop never
// branches on provider. KV-cache discipline lives in the prompt builder
// (separate file) — this client does the wire layer only.
package llm

import (
	"context"
	"encoding/json"
)

// Role names mirror the OpenAI-compatible wire format. They are the canonical
// representation kept in the conversation history.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one entry in the conversation. ToolCalls populates only for
// assistant messages that invoke tools; ToolCallID populates only for `tool`
// role messages that report a result.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is one assistant-emitted call. Arguments stays a JSON string so the
// dispatcher can validate against the destination tool's schema.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // always "function" for OpenAI compat
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolDef is the LLM-visible declaration of one tool, ready to be wire-encoded
// inside a chat-completion request. The agent assembles this from
// tools.Registry.Render() each turn.
type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// Chunk is one streamed delta from the LLM. Exactly one of Text or ToolCall is
// populated; FinishReason is set on the final chunk of the stream.
type Chunk struct {
	Text         string
	ToolCall     *ToolCall
	FinishReason string
}

// Client is the interface the agent loop targets. Stream returns a channel of
// Chunks; consumers MUST drain it (or the implementation will leak goroutines
// and HTTP connections).
type Client interface {
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Request is one chat-completion call. Caching is a property of how the caller
// constructs Messages (stable prefix) — the wire layer is unaware.
type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolDef
	Temperature float64
	MaxTokens   int
}
