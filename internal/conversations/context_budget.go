package conversations

// The budget half of the context ladder: what a conversation is allowed to spend, and the
// two thresholds derived from it. Split out of context.go when that file crossed the
// 600-LOC cap; the ladder itself (which SPENDS the budget) stays there.
//
// The reserves come from internal/llm rather than being restated here: two copies of the
// same arithmetic is how the ladder and the request guard come to disagree about what fits.

import "github.com/chetto1983/aura/internal/llm"

// ContextConfig carries the L1/L2 inputs. ContextWindow + MaxOutputTokens come
// from llm.Config (04-01); ToolEvictAfterTurns from config.Config
// (AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS).
type ContextConfig struct {
	ContextWindow int
	// FixedOverheadTokens is what the request carries BESIDES the history: the tool
	// manifest the agent renders per turn, and anything else added after the ladder has
	// run. The ladder cannot see it -- it is handed turns, not a request -- and it is not
	// small: measured 2026-08-16 on this deployment, a request whose history was ~19k
	// tokens reached the provider at 38,941. Without it "compact at 30% of the window"
	// silently means "at 30% of the part I happen to count".
	FixedOverheadTokens int
	// CompactionTriggerPercent is the share of ContextWindow the replayed history may
	// occupy before L2.4 condenses it, ahead of the hard cap. 0 -- the zero value every
	// hand-built config in the tests carries -- disables early compaction, so adding this
	// changed no existing behaviour; the Runner sets it from llm.Config.
	CompactionTriggerPercent int
	MaxOutputTokens          int
	ToolEvictAfterTurns      int
	// HistoryHardCapTurns bounds rows fetched, sidecars rehydrated, and turns
	// tokenized before the L1/L2 ladder. The protected system head counts toward
	// this aggregate cap.
	HistoryHardCapTurns int
	// AlwaysBlock is the rendered messages[1] always-on skill block (D-07). When
	// non-empty the ladder injects it as a PROTECTED user-role turn immediately after
	// the system L0 turn — counted toward the budget but never evicted by L1/L2.5
	// (Pitfall 3). The Runner renders it per turn from current loader state; an empty
	// string means no always:true skill is active (the turn is omitted).
	AlwaysBlock string
	// TransientContext is a non-persisted reference item, budgeted as one unit and
	// inserted immediately before the current user turn. It is omitted, never
	// truncated, when the deterministic ladder cannot make room for it.
	TransientContext *TransientContext
	// ProviderErrorReserveTokens is the extra L2 hard-cap headroom the configured chat
	// backend needs for its token-estimation error (Wave 1.10). It is non-zero only on
	// the local llama.cpp path, which has no provider-side overflow net — an Aura
	// under-count there is a hard local window overflow, so the cap reserves this many
	// tokens below the SPEC Req#10 formula. The Runner sets it from
	// llm.ProviderErrorReserveTokens(cfg); zero (the default) leaves the formula
	// unchanged for OpenRouter and hand-built test configs.
	ProviderErrorReserveTokens int
	// Summarizer, when non-nil, enables L2.4 LLM compaction: over-budget history is
	// condensed into one synthetic summary turn (keeping the protected head and the
	// active round verbatim) BEFORE the deterministic L2.5 hard-drop. A nil Summarizer
	// (the default, and every unit test that does not set it) disables compaction, so
	// the ladder behaves exactly as before.
	Summarizer Summarizer

	// compactionCache persists the summary so it is computed once instead of once per
	// turn. Set by managedFromTurns (the Store is the implementation); nil in the unit
	// tests that drive the ladder directly, which then behave exactly as before.
	compactionCache compactionCache

	// branchID names WHICH history the durable summary speaks for. A branch is a
	// different conversation past sharing a prefix, so it needs its own summary and its
	// own watermark -- the compaction row is keyed (conversation, branch) exactly for
	// that. It was passed as "" from both call sites until 2026-08-16, which meant two
	// branches shared one row: the summary written while replaying branch A was handed
	// to branch B, whose turns at those seq numbers are different turns.
	//
	// Empty is the canonical branch (the all-zero uuid the 0017 backfill gives every
	// pre-branch turn), so the linear path needs no value and the tests keep working.
	branchID string
}

// hardCap computes the L2 hard cap from the config (SPEC Req#10:
// hard_cap = ContextWindow - max(MaxOutputTokens, 20000) - 13000 -
// ProviderErrorReserveTokens). ProviderErrorReserveTokens (Wave 1.10) is 0 for
// OpenRouter and hand-built configs, so the historical formula is unchanged there;
// on the netless local llama.cpp path it reserves extra headroom for the
// tiktoken-vs-local-tokenizer gap. The formula is kept whenever it is already
// positive (normal/large windows). For a small-window model where the fixed
// reservation exceeds the window the formula is non-positive; instead of clamping to
// 0 (which disabled L2/L2.5 protection entirely — finding M-03) it floors to a
// positive smallWindowHardCapFloor so L2.5 truncation stays active. A degenerate
// ContextWindow <= 0 still yields 0.
func (c ContextConfig) hardCap() int {
	out := llm.OutputReserve(c.ContextWindow, c.MaxOutputTokens)
	cap := c.ContextWindow - out - llm.PromptHeadroom(c.ContextWindow) - c.ProviderErrorReserveTokens
	if cap <= 0 {
		cap = smallWindowHardCapFloor(c.ContextWindow)
	}
	return cap
}

// earlyCompactionTokens is the point at which the ladder condenses history even though
// nothing is over budget yet: a share of the MODEL WINDOW, default half, editable from the
// cockpit Settings page (AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT).
//
// A share of the window, not of the hard cap, because the number has to mean something to
// the person tuning it: "how much of my window may be spent replaying the conversation
// verbatim". Deriving it from hardCap would make it drift with MaxOutputTokens and the
// provider error reserve, which have nothing to do with that judgement.
//
// It returns 0 -- disabled -- with no summarizer, a degenerate window, or a percentage
// outside (0,100). 100 is disabled on purpose rather than clamped: at 100 the trigger and
// the hard cap describe the same moment, and the hard cap already owns it.
func (c ContextConfig) earlyCompactionTokens() int {
	if c.Summarizer == nil || c.ContextWindow <= 0 {
		return 0
	}
	if c.CompactionTriggerPercent <= 0 || c.CompactionTriggerPercent >= 100 {
		return 0
	}
	budget := c.ContextWindow * c.CompactionTriggerPercent / 100
	// Spend the budget on the history alone: the fixed cost is already on the wire no
	// matter what the ladder does, so counting it here is what makes the percentage mean
	// "how full the REQUEST is" rather than "how long the transcript is". A fixed cost
	// larger than the budget yields 0 -- disabled -- because compacting cannot help when
	// the manifest alone has spent the allowance.
	budget -= c.FixedOverheadTokens
	if budget <= 0 {
		return 0
	}
	return budget
}

// HardCap exposes the exact history cap to the final model-request guard.
func (c ContextConfig) HardCap() int {
	return c.hardCap()
}

// smallWindowHardCapFloor returns the positive L2 cap used when the SPEC Req#10
// formula is non-positive (M-03). Adopting the nanobot precedent (a budget <= 0
// clamps to max(128, window//2) rather than disabling history management), Aura
// floors to ContextWindow/2 so L2.5 stays active on small windows. A window <= 0 is
// a misconfiguration with no usable budget, so it returns 0 (the only remaining
// hardCap==0 path; boot fail-fast is the seam to reject it).
func smallWindowHardCapFloor(window int) int {
	if window <= 0 {
		return 0
	}
	return window / 2
}
