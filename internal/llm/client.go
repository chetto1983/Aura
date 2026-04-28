package llm

import "context"

// Request represents an LLM API request.
type Request struct {
	Messages []Message
	Model    string
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// Response represents an LLM API response.
type Response struct {
	Content string
	Usage   TokenUsage
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