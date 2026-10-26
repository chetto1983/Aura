package agent

import (
	"log/slog"
	"unicode/utf8"
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
