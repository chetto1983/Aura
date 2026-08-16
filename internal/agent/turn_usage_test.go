package agent

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The bill and the fill are two different numbers, and reading one as the other is what
// put the cockpit's context gauge at 88% of an 81,920-token window on a round whose
// largest single request was about 24k (live deployment, 2026-08-16, conversation
// 01a00c4b). A round re-sends its whole prefix once per tool call and is billed for each;
// only the last of those prompts was ever in the window at one time.
func TestTurnUsageSeparatesTheBillFromTheWindowFill(t *testing.T) {
	t.Parallel()
	var u turnUsage
	for _, prompt := range []int{12_000, 18_000, 24_000} {
		u.add(llm.Usage{PromptTokens: prompt, CompletionTokens: 100, CachedTokens: prompt / 2})
	}

	got := u.total()
	if got.PromptTokens != 54_000 {
		t.Errorf("PromptTokens = %d, want the sum 54000 — that is what the provider billed", got.PromptTokens)
	}
	if got.ContextTokens != 24_000 {
		t.Errorf("ContextTokens = %d, want the last call's 24000 — that is what occupied the window", got.ContextTokens)
	}
}

// A single-call turn is the case where the two coincide, which is exactly why the gap
// stayed invisible until a tool-calling round was measured.
func TestTurnUsageSingleCallReportsTheSameNumberTwice(t *testing.T) {
	t.Parallel()
	var u turnUsage
	u.add(llm.Usage{PromptTokens: 13_540, CompletionTokens: 42})

	got := u.total()
	if got.PromptTokens != 13_540 || got.ContextTokens != 13_540 {
		t.Errorf("total = %+v, want both figures at 13540", got)
	}
}

// A call that reports no prompt tokens (a provider that omits usage on an intermediate
// tool round-trip) must not erase the fill with a zero: the window did not empty because
// the provider went quiet.
func TestTurnUsageKeepsTheLastKnownFillOverAZeroReport(t *testing.T) {
	t.Parallel()
	var u turnUsage
	u.add(llm.Usage{PromptTokens: 20_000})
	u.add(llm.Usage{CompletionTokens: 7}) // no prompt figure at all

	if got := u.total(); got.ContextTokens != 20_000 {
		t.Errorf("ContextTokens = %d, want the last reported 20000", got.ContextTokens)
	}
}

// And the event the cockpit reads must carry it, or the fix stops at the agent boundary.
func TestUsageStateDeltaCarriesTheWindowFill(t *testing.T) {
	t.Parallel()
	delta := usageStateDelta(llm.Usage{PromptTokens: 54_000, ContextTokens: 24_000})

	if delta["prompt_tokens"] != 54_000 {
		t.Errorf("prompt_tokens = %v, want the billed sum", delta["prompt_tokens"])
	}
	if delta["context_tokens"] != 24_000 {
		t.Errorf("context_tokens = %v, want the window fill", delta["context_tokens"])
	}
}
