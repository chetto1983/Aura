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

	// ReasoningEffort enables provider-side chain-of-thought for models that
	// support it (DeepSeek V4 Flash, OpenAI o-series / gpt-5*, Anthropic
	// thinking, ...). Allowed values match the lowest-common-denominator set
	// across providers: "minimal", "low", "medium", "high", "xhigh".
	// Empty string means "do not include any reasoning field" so providers
	// without reasoning support (vanilla OpenAI gpt-4o, fakes in tests)
	// keep working unchanged.
	//
	// The wire format depends on the provider: OpenAI accepts the top-level
	// `reasoning_effort` string; OpenRouter normalizes the legacy top-level
	// flag and also accepts the nested `reasoning: {effort: "..."}` object.
	// The openai.go transport emits BOTH so the request is portable.
	ReasoningEffort string
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
//
// Reasoning carries the plaintext "thinking" the model produced before its
// final answer, when the provider surfaces it (OpenRouter on Claude /
// DeepSeek / o-series; empty everywhere else). ReasoningDetails is the
// opaque structured array providers want the caller to round-trip on the
// next turn — kept as raw bytes so we never lose fidelity.
type Response struct {
	Content          string
	Usage            TokenUsage
	HasToolCalls     bool
	ToolCalls        []ToolCall
	Reasoning        string
	ReasoningDetails []byte
}

// TokenUsage tracks token consumption for a single LLM call.
//
// ReasoningTokens, when non-zero, accounts for tokens the model spent on
// chain-of-thought that did NOT reach Content. It is included in
// CompletionTokens (and therefore in TotalTokens) so existing budget
// math is unchanged; treat it as a finer-grained breakdown rather than a
// separate counter to add on top.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ReasoningTokens  int
}

// Token represents a streaming response chunk.
//
// Content is a text delta — empty strings are valid (some providers emit
// role-only chunks).
//
// ToolCalls is populated only on the final token (Done=true) when the
// model decided to call tools. The streaming parser accumulates the
// per-delta argument fragments internally and surfaces fully-formed
// ToolCall objects here, so consumers don't need to track per-index
// argument state. If ToolCalls is non-empty, callers should treat the
// streamed Content as discardable scaffolding (most providers emit no
// Content alongside tool calls) and route to the tool execution path.
//
// Usage is populated only on the final token when the provider honors
// stream_options.include_usage. Providers that omit usage leave it zero, so
// callers must tolerate that.
type Token struct {
	Content   string
	// Reasoning is the chain-of-thought delta produced by reasoning-capable
	// providers (DeepSeek V4 Flash, OpenAI o-series / gpt-5*, Anthropic
	// thinking). It is NOT part of the final answer; UIs should render it
	// in a distinct style (italic, faded, "🧠 thinking…" prefix) so the
	// user sees progress instead of a stall, and can ignore it once the
	// Content stream begins. Empty when the provider does not surface it.
	Reasoning string
	ToolCalls []ToolCall
	Usage     TokenUsage
	Done      bool
	Err       error
}

// Client is the interface for LLM providers.
type Client interface {
	// Send sends a request and returns the full response.
	Send(ctx context.Context, req Request) (Response, error)
	// Stream sends a request and returns a channel of tokens.
	Stream(ctx context.Context, req Request) (<-chan Token, error)
}
