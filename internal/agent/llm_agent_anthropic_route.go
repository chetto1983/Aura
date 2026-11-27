package agent

import (
	"slices"

	"github.com/chetto1983/aura/internal/llm"
)

// loadedHistoryCount is how many messages of the constructed history precede the
// current user message: the system prompt plus what the conversation store loaded. It
// is the position of the last user message, zero when there is none.
func loadedHistoryCount(hist []llm.Message) int {
	for i, msg := range slices.Backward(hist) {
		if msg.Role == llm.RoleUser {
			return i
		}
	}
	return 0
}

// keepRoundHint appends to the run's history the per-round hint the request just sent,
// so the next request repeats it in place instead of dropping it. Only the anthropic
// route sets StablePrefixMessages: Claude rejects a replayed thinking block whose prefix
// changed, and a rotated hint is such a change (spec 2026-11-27 §0 point 1). Every
// other route keeps today's single trailing hint.
func (a *LlmAgent) keepRoundHint(req llm.Request) {
	if req.StablePrefixMessages <= 0 || req.StablePrefixMessages >= len(req.Messages) {
		return
	}
	a.history = append(a.history, req.Messages[req.StablePrefixMessages:]...)
}
