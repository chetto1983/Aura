package runner

import "github.com/chetto1983/aura/internal/llm"

// stripLeadingSystem drops a persisted leading system turn so the agent's own
// byte-stable system message is the sole messages[0] (KV-cache discipline).
func stripLeadingSystem(history []llm.Message) []llm.Message {
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		return history[1:]
	}
	return history
}
