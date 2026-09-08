package agent

import (
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/llm"
)

const completionVetoPrefix = "Completion check FAILED: "
const completionMaxAttempts = 2

// Completion checks are local: never buy another model call to judge an answer.
func (a *LlmAgent) gateCompletion(answer string) (veto bool, feedback string) {
	if !a.cfg.CompletionGate || a.completionAttempts >= completionMaxAttempts {
		return false, ""
	}
	veto, feedback = a.gateReplyHygiene(answer)
	if veto {
		a.completionAttempts++
		slog.Info("agent completion gate: answer vetoed", "source", "reply_hygiene",
			"attempt", a.completionAttempts, "answer_runes", utf8.RuneCountInString(answer))
	}
	return veto, feedback
}

// Retained conversations can contain nudges from the removed LLM critic.
// They must not become the user's request when a continuation resumes.
func lastUserRequest(history []llm.Message) string {
	for _, v := range slices.Backward(history) {
		m := v
		if m.Role != llm.RoleUser || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if isAgentNudge(m.Content) {
			continue
		}
		return m.Content
	}
	return ""
}

// isAgentNudge reports whether content is one of the agent's own injected
// user-role messages (so lastUserRequest never mistakes a nudge for the request).
func isAgentNudge(content string) bool {
	return content == recoveryNudgeGeneric ||
		content == recoveryNudgeEmpty ||
		strings.HasPrefix(content, recoveryNudgeToolPrefix) ||
		strings.HasPrefix(content, completionVetoPrefix) ||
		strings.HasPrefix(content, "Completion check FAILED again: ") ||
		strings.HasPrefix(content, verifyOnStopNudgePrefix) ||
		strings.HasPrefix(content, deliverOnStopNudgePrefix)
}
