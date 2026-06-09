package prompt

import (
	"fmt"
	"strings"

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

// Budget carries the per-turn volatile hints Build renders after history:
// used/remaining tool-step counts (D-06, Req#6), the per-conversation Workspace
// path (#52/D-41 — the model must KNOW where its workspace is to honor the
// system prompt's deliverables convention; live run 7 saved a perfect .xlsx to
// the Desktop because nothing ever told it the path), and current local time for
// "today" requests. They ride the SAME trailing message, appended AFTER history,
// so the cached prefix is never poisoned. The agent passes branchConsumed as
// Used and Remaining() as Remaining; there is no Budget.MaxSteps() getter
// (landmine #11). The zero value is the omit sentinel: Build emits no trailing
// message, preserving the byte-identical default for callers that track none.
type Budget struct {
	Used        int
	Remaining   int
	Workspace   string
	CurrentTime string
	Today       string
}

// present reports whether the trailing hint message should be emitted. The zero
// value (counts 0, no workspace/time) is the backward-compatible omit case.
func (b Budget) present() bool {
	return b.Used != 0 || b.Remaining != 0 || b.Workspace != "" || b.CurrentTime != "" || b.Today != ""
}

// block renders the directional hint: the D-06 budget line (omitted when both
// counts are zero), plus workspace/current-time lines when configured (#52/D-41).
func (b Budget) block() string {
	var lines []string
	if b.Used != 0 || b.Remaining != 0 {
		lines = append(lines, fmt.Sprintf("<budget>used=%d remaining=%d</budget>", b.Used, b.Remaining))
	}
	if b.Workspace != "" {
		lines = append(lines, fmt.Sprintf("<workspace>%s</workspace>", b.Workspace))
	}
	if b.CurrentTime != "" {
		lines = append(lines, fmt.Sprintf("<current_time>%s</current_time>", b.CurrentTime))
	}
	if b.Today != "" {
		lines = append(lines, fmt.Sprintf("<today>%s</today>", b.Today))
	}
	return strings.Join(lines, "\n")
}

// Build assembles the chat-completion request from the supplied history, tool
// registry, provider, and config. It reproduces the previous inline construction
// byte-for-byte (Tools = reg.RenderToolDefs() with its cache-load-bearing
// alphabetical order untouched, scalars from cfg) so the emitted messages[0]
// stays byte-identical (D-01). When a volatile hint is present, a trailing user-role
// hint message is appended to a COPY of history (the caller's slice and
// messages[0] are never mutated — KV-cache poisoning guard, D-04/D-05). The
// provider branch runs last. Adaptive reasoning is applied only by
// BuildWithReasoningTier, after a caller has produced a tier outside the pure
// builder. cache_control remains a no-op unless provider == "anthropic".
func (b *PromptBuilder) Build(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget) llm.Request {
	req := b.buildBase(history, reg, cfg, budget)
	injectCacheControl(&req, provider)
	return req
}

// BuildWithReasoningTier assembles a request and applies the caller-provided
// adaptive reasoning tier before provider-specific cache-control handling.
func (b *PromptBuilder) BuildWithReasoningTier(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, tier ReasoningTier) llm.Request {
	req := b.buildBase(history, reg, cfg, budget)
	ApplyAdaptiveReasoning(&req, provider, cfg, tier)
	injectCacheControl(&req, provider)
	return req
}

func (b *PromptBuilder) buildBase(history []llm.Message, reg *tools.Registry, cfg llm.Config, budget Budget) llm.Request {
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
	return req
}
