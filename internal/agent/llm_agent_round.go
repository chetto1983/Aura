// Per-round Run loop helpers, split out of llm_agent.go so it stays under the
// 600-LOC no-god-class cap (CLAUDE.md) once plan 45-04's two dedup call sites
// landed. Both functions below are mechanical extractions with no behaviour
// change and no signature change visible outside the package: roundBudget
// reads only receiver state plus ic.Budget/ic.RequestID-independent inputs
// (its local `now` is used ONLY for CurrentTime/Today and escapes nowhere
// else — measured against llm_agent.go's Run loop before the split),
// recordRequestBuilt is a pure side effect over inputs already in scope at
// its one call site.
package agent

import (
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// roundBudget assembles the per-round prompt.Budget the request builder
// consumes.
//
// The builder is the single assembly chokepoint (D-01): it reproduces the
// byte-stable messages[0] and routes the provider-aware cache_control seam.
// a.history stays read-only — the client never mutates it (Req#13). The live
// volatile hints are tail-injected to a COPY (messages[0] untouched, D-04):
// remaining is the shared balance, used = the steps this branch has spent
// (Remaining never exceeds the start, so used = start-remaining is the
// per-branch consumption — no MaxSteps() getter, landmine #11). Current time
// rides here too, not in the system prompt, so date-sensitive turns are
// deterministic without poisoning the cached prefix.
func (a *LlmAgent) roundBudget(ic InvocationContext) prompt.Budget {
	budget := a.groundingBudget(ic.Budget.Now())
	budget.Used = ic.Budget.BranchConsumed()
	budget.Remaining = ic.Budget.Remaining()
	// The roster of what is still unloaded, minus what this run already promoted —
	// so it shrinks as the turn goes and never tells the model to load something it
	// is already holding. It travels here rather than in tool_search's Description
	// for the same reason as Sources: the cached prefix is the wrong distance from
	// the decision.
	budget.DeferredTools = a.registry.DeferredRoster(a.activated)
	return budget
}

// groundingBudget carries only the FACTS the turn is handed — no step counts, no
// deferred roster. Split out because the completion critic needs exactly this half
// and must not be handed instructions meant for the model (llm_agent_completion.go).
func (a *LlmAgent) groundingBudget(now time.Time) prompt.Budget {
	loc := clockLocation(a.location, now)
	return prompt.Budget{
		Workspace:   a.workspace,
		CurrentTime: tools.FormatClock(now, loc),
		Today:       now.In(loc).Format("2006-01-02"),
		// D-05: the volatile numbered source list rides the tail-inject copy
		// (RenderSourceList is "" until a web tool consulted a source this turn,
		// so a non-web turn keeps the byte-identical default). messages[0] stays
		// untouched — the static citation convention lives in the system prompt.
		Sources: a.sources.RenderSourceList(),
	}
}

// recordRequestBuilt emits the agent_request_built reasoning-trace event for
// one round. Pure side effect, no return value; every input is already in
// scope at the call site.
func (a *LlmAgent) recordRequestBuilt(requestID string, round modelRound, req llm.Request) {
	reasoningtrace.Record("agent_request_built", map[string]any{
		"request_id":          requestID,
		"model_round_ordinal": round.ordinal,
		"thread_id":           a.sessionID,
		"provider":            a.cfg.Provider,
		"model":               req.Model,
		"max_tokens":          req.MaxTokens,
		"tool_choice":         req.ToolChoice,
		"tools_count":         len(req.Tools),
		"reasoning":           req.Reasoning,
		"history":             a.history,
	})
}

// clockLocation is the zone to render a hint in: the configured one, or -- when nothing is
// configured -- the instant's OWN zone.
//
// Nil means "leave it alone", never "UTC". Forcing UTC here rewrote a caller's +02:00 clock
// into Z and lost the fact the caller had already supplied, which is the same information
// loss this whole change exists to undo, one layer up.
func clockLocation(loc *time.Location, now time.Time) *time.Location {
	if loc == nil {
		return now.Location()
	}
	return loc
}
