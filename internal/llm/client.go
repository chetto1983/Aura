package llm

import "context"

// Float64Ptr returns a pointer to the given float64 value.
func Float64Ptr(v float64) *float64 { return &v }

// Request represents an LLM API request.
type Request struct {
	Messages    []Message
	Model       string
	Temperature *float64 // nil = API default, 0 = deterministic, >0 = creative. Use 0 for wiki operations.
	Tools       []ToolDefinition
}

// Message represents a single message in a conversation.
type Message struct {
	Role       string // "system", "user", "assistant", "tool"
	Content    string
	ToolCalls  []ToolCall // set on assistant messages when the model requests tool calls
	ToolCallID string     // set on tool result messages to correlate with a ToolCall.ID
}

// ToolDefinition describes a tool that the model can call.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall represents a request from the model to invoke a tool.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Response represents an LLM API response.
type Response struct {
	Content      string
	Usage        TokenUsage
	HasToolCalls bool
	ToolCalls    []ToolCall
}

// TokenUsage tracks token consumption for a single LLM call.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Token represents a streaming response chunk.
type Token struct {
	Content string
	Done    bool
	Err     error
}

// Client is the interface for LLM providers.
type Client interface {
	// Send sends a request and returns the full response.
	Send(ctx context.Context, req Request) (Response, error)
	// Stream sends a request and returns a channel of tokens.
	Stream(ctx context.Context, req Request) (<-chan Token, error)
}
