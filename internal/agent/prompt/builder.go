package prompt

import (
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// PromptBuilder is the single chokepoint that assembles the wire llm.Request for
// every LLM call (D-01). It does not own the system prompt or history mutation —
// the caller supplies an already-byte-stable history whose messages[0] is the
// prepended system message — it owns the request SHAPE and the provider-aware
// cache_control branch. Centralizing it gives the cache-invariant gate (06-05)
// one place to read and the future messages[1]/messages[2] tiers (Slices 10/11e)
// one place to attach.
type PromptBuilder struct{}

// NewPromptBuilder returns a stateless builder.
func NewPromptBuilder() *PromptBuilder { return &PromptBuilder{} }

// Build assembles the chat-completion request from the supplied history, tool
// registry, provider, and config. It reproduces the previous inline construction
// byte-for-byte (Messages = history as-is, Tools = reg.RenderToolDefs() with its
// cache-load-bearing alphabetical order untouched, scalars from cfg) so the
// emitted messages[0] stays byte-identical (D-01). The provider branch runs last
// and is a no-op unless provider == "anthropic".
func (b *PromptBuilder) Build(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config) llm.Request {
	req := llm.Request{
		Model:       cfg.Model,
		Messages:    history,
		Tools:       reg.RenderToolDefs(),
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	}
	injectCacheControl(&req, provider)
	return req
}
