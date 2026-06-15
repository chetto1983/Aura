package agent

import (
	"log/slog"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
)

// prefixSnapshot returns the cache-stable prefix hash (messages[0]) used to
// detect a BeforeModel hook rewrite that busts the provider's implicit prompt
// cache (AG-031). An empty string means the hash could not be computed; the
// drift check then no-ops rather than reporting a false positive.
func prefixSnapshot(msgs []llm.Message) string {
	h, err := prompt.PrefixHash(msgs, []int{0})
	if err != nil {
		return ""
	}
	return h
}

// checkPrefixDrift compares the post-hook prefix against the pre-hook snapshot
// and, on a mismatch, records the prefix_drift metric + a warning. It never
// mutates messages[0] — it only observes (AG-031). An empty snapshot (hash error)
// or a post-hash error skips the check.
func (a *LlmAgent) checkPrefixDrift(before string, msgs []llm.Message, requestID string) {
	if before == "" {
		return
	}
	after, err := prompt.PrefixHash(msgs, []int{0})
	if err != nil || after == before {
		return
	}
	recordPrefixDrift()
	slog.Warn("agent cache-prefix drift after BeforeModel hook", "request_id", requestID, "thread_id", a.sessionID)
}
