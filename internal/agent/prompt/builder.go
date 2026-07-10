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
// the Desktop because nothing ever told it the path), current local time for
// "today" requests, and the numbered web-source list (D-05 — the volatile
// `[n] Title — url` block the model copies its inline citation numbers from). They
// ride the SAME trailing message, appended AFTER history, so the cached prefix is
// never poisoned: the source list MUST travel here and never in messages[0] (the
// static citation-convention sentence lives in the system prompt instead), or it
// trips the AG-031 KV-drift guard. The agent passes branchConsumed as Used and
// Remaining() as Remaining; there is no Budget.MaxSteps() getter (landmine #11).
// The zero value is the omit sentinel: Build emits no trailing message, preserving
// the byte-identical default for callers that track none.
type Budget struct {
	Used        int
	Remaining   int
	Workspace   string
	CurrentTime string
	Today       string
	Sources     string
}

// present reports whether the trailing hint message should be emitted. The zero
// value (counts 0, no workspace/time/sources) is the backward-compatible omit case.
func (b Budget) present() bool {
	return b.Used != 0 || b.Remaining != 0 || b.Workspace != "" || b.CurrentTime != "" || b.Today != "" || b.Sources != ""
}

// block renders the directional hint: the D-06 budget line (omitted when both
// counts are zero), workspace/current-time lines when configured (#52/D-41), and
// the D-05 numbered source list when a turn consulted web sources. The source
// block mirrors <current_time>: an XML-tagged volatile line that rides the tail-
// inject copy, never messages[0].
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
	if b.Sources != "" {
		lines = append(lines, fmt.Sprintf("<sources>\n%s\n</sources>", b.Sources))
	}
	return strings.Join(lines, "\n")
}

// Build assembles the chat-completion request from the supplied history, tool
// registry, provider, and config. It reproduces the previous inline construction
// byte-for-byte (Tools = reg.RenderToolDefs(activated) with its cache-load-bearing
// alphabetical order untouched, scalars from cfg) so the emitted messages[0]
// stays byte-identical (D-01). When a volatile hint is present, a trailing user-role
// hint message is appended to a COPY of history (the caller's slice and
// messages[0] are never mutated — KV-cache poisoning guard, D-04/D-05). The
// provider branch runs last. Adaptive reasoning is applied only by
// BuildWithReasoningTier, after a caller has produced a tier outside the pure
// builder. cache_control remains a no-op unless provider == "anthropic".
//
// activated is the per-run set of deferred tool names tool_search has promoted
// into the callable manifest (Claude Code parity); nil hides every deferred tool.
func (b *PromptBuilder) Build(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, activated map[string]struct{}) llm.Request {
	req := b.buildBase(history, reg, cfg, budget, activated)
	injectCacheControl(&req, provider)
	return req
}

// BuildWithReasoningTier assembles a request and applies the caller-provided
// adaptive reasoning tier before provider-specific cache-control handling. activated
// is the per-run set of tool_search-promoted deferred tool names (nil hides all).
func (b *PromptBuilder) BuildWithReasoningTier(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, tier ReasoningTier, activated map[string]struct{}) llm.Request {
	req := b.buildBase(history, reg, cfg, budget, activated)
	ApplyAdaptiveReasoning(&req, provider, cfg, tier)
	injectCacheControl(&req, provider)
	return req
}

// BuildWithReasoningOverride assembles a request and forces a caller-selected FIXED
// reasoning effort before provider-specific cache-control handling — the symmetric
// sibling of BuildWithReasoningTier for the per-turn web-Composer override (D-02/D-04).
// A fixed effort BYPASSES the adaptive classifier (ApplyFixedReasoning gates on the
// generalized OpenRouter-or-llama.cpp target, D-08); an empty effort is the "auto"
// sentinel that leaves the request byte-identical to a plain Build (D-04 zero regression).
// activated is the per-run set of tool_search-promoted deferred tool names (nil hides all).
func (b *PromptBuilder) BuildWithReasoningOverride(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, effort llm.ReasoningEffort, activated map[string]struct{}) llm.Request {
	req := b.buildBase(history, reg, cfg, budget, activated)
	ApplyFixedReasoning(&req, provider, cfg, effort)
	injectCacheControl(&req, provider)
	return req
}

func (b *PromptBuilder) buildBase(history []llm.Message, reg *tools.Registry, cfg llm.Config, budget Budget, activated map[string]struct{}) llm.Request {
	msgs := history
	if budget.present() {
		msgs = append(append([]llm.Message(nil), history...), llm.Message{Role: llm.RoleUser, Content: budget.block()})
	}
	req := llm.Request{
		Model:       cfg.Model,
		Messages:    msgs,
		Tools:       reg.RenderToolDefs(activated),
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	}
	return req
}
