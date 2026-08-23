package conversations

// The fixture builders the boundary tests share: a config whose cap is a known number, and
// a history sized to a known token count. Split out of context_boundary_test.go when that
// file crossed the 600-LOC cap.

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// windowFor returns a config whose hardCap is EXACTLY want.
//
// It used to compute want + 20000 + 13000, restating the reserve arithmetic. That broke
// on 2026-08-16 when the reserves became the smaller of those constants and a share of the
// window (so a small window no longer subtracts more than it has): every boundary test
// built on this helper silently got a different cap than its name promised. Solving for
// the window through hardCap itself means the helper cannot encode a stale formula again --
// it asks the code under test what the cap is.
//
// hardCap is monotonically increasing in the window, so a binary search finds it.
func windowFor(want int) ContextConfig {
	cfg := func(window int) ContextConfig {
		return ContextConfig{ContextWindow: window, MaxOutputTokens: 1, ToolEvictAfterTurns: 1_000_000}
	}
	lo, hi := want+1, 4*(want+l2MinOutputReservation+l2HeadroomTokens)
	for lo < hi {
		mid := (lo + hi) / 2
		if cfg(mid).hardCap() < want {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return cfg(lo)
}

// pairHistory builds [system, user, assistant] where the user+assistant bodies
// are sized so the whole history totals exactly `target` tokens. It returns the
// turns; the caller dials hardCap around target to hit a boundary.
func sizedTurns(t *testing.T, enc *tiktoken.Tiktoken, bodyTokens int) []Turn {
	t.Helper()
	// "word " is 1 token in cl100k_base; repeat to reach the wanted body size.
	body := strings.Repeat("word ", bodyTokens)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: body},
		{Seq: 3, Role: llm.RoleAssistant, Content: body},
		{Seq: 4, Role: llm.RoleUser, Content: "q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "a"},
	}
	return turns
}

// l2HeadroomTokens is fixture padding for the boundary range below, NOT the live
// reservation: context_budget.go subtracts llm.PromptHeadroom(ContextWindow), which
// scales with the window instead of being the fixed 13000 this once described.
const l2HeadroomTokens = 13000
