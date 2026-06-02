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

// Usage is the provider-neutral token+cost summary surfaced on the final stream
// chunk so the agent loop can populate the llm.request span (D-13/Req#12) without
// importing a provider package. CachedTokens is the cache-READ count (the
// implicit prompt-cache discount), never cache writes. Cost is the provider's
// reported figure (nil when the provider sent none — the caller falls back to a
// price table and never reports $0 for an unknown model, D-18).
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	Cost             *float64
}

// Chunk is one streamed delta from the LLM. Exactly one of Text, ToolCall, or
// Usage is populated; FinishReason is set on the final content/tool chunk of the
// stream. The trailing Usage chunk (when present) carries the final token+cost
// summary so the agent can read it through the provider-neutral channel.
type Chunk struct {
	Text         string
	ToolCall     *ToolCall
	FinishReason string
	Usage        *Usage
}

// Client is the interface the agent loop targets. Stream returns a channel of
// Chunks; consumers MUST drain it (or the implementation will leak goroutines
// and HTTP connections).
type Client interface {
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Request is one chat-completion call. Caching is primarily a property of how
// the caller constructs Messages (a byte-stable prefix earns the implicit
// prompt-cache discount); that assembly decision lives in the prompt builder,
// not here. ToolsCacheControl is the one explicit knob: it is wire-shape only
// (an Anthropic-direct cache_control marker), set by the builder's provider
// branch and dormant — empty under OpenRouter, the day-1 default. The wire
// layer serializes it but never decides whether to inject it (D-03/D-03a).
type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolDef
	Temperature float64
	MaxTokens   int

	// ToolsCacheControl, when non-empty, is the Anthropic-direct cache_control
	// marker for the tools+system prefix breakpoint (e.g. "ephemeral"). It is set
	// only by the provider branch in internal/agent/prompt; the OpenAI-compat wire
	// client ignores it (Slice 13 LLMRouter does the Anthropic-native translation).
	ToolsCacheControl string
}
