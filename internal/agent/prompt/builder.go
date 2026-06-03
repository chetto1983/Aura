package prompt

import (
	"fmt"

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

// Budget carries the used/remaining tool-step counts that Build renders into the
// trailing <budget> hint (D-06, Req#6). The agent passes branchConsumed as Used
// and Remaining() as Remaining; there is no Budget.MaxSteps() getter (landmine
// #11). The zero value (both fields 0) is the omit sentinel: Build emits no
// <budget> message, preserving the byte-identical default for callers that do
// not track a budget (e.g. the wave-1 placeholder at llm_agent.go).
type Budget struct {
	Used      int
	Remaining int
}

// present reports whether a <budget> block should be emitted. The zero value
// (both 0) is the backward-compatible omit case.
func (b Budget) present() bool { return b.Used != 0 || b.Remaining != 0 }

// block renders the directional hint exactly as D-06 specifies.
func (b Budget) block() string {
	return fmt.Sprintf("<budget>used=%d remaining=%d</budget>", b.Used, b.Remaining)
}

// Build assembles the chat-completion request from the supplied history, tool
// registry, provider, and config. It reproduces the previous inline construction
// byte-for-byte (Tools = reg.RenderToolDefs() with its cache-load-bearing
// alphabetical order untouched, scalars from cfg) so the emitted messages[0]
// stays byte-identical (D-01). When budget is present, a trailing user-role
// <budget> message is appended to a COPY of history (the caller's slice and
// messages[0] are never mutated — KV-cache poisoning guard, D-04/D-05). The
// provider branch runs last and is a no-op unless provider == "anthropic".
func (b *PromptBuilder) Build(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget) llm.Request {
	msgs := history
	if budget.present() {
		msgs = append(append([]llm.Message(nil), history...), llm.Message{Role: llm.RoleUser, Content: budget.block()})
	}
	req := llm.Request{
		Model:       cfg.Model,
		Messages:    msgs,
		Tools:       reg.RenderToolDefs(),
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	}
	injectCacheControl(&req, provider)
	return req
}
