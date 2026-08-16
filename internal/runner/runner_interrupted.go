package runner

import (
	"context"
	"log/slog"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// interruptedRoundMarker is what a person reads where the answer should have been.
//
// Bracketed and machine-recognisable, in the shape readToolOutputPointer already uses for
// text this system generated, rather than prose pretending to be the assistant. It carries
// NO error detail on purpose: a conversation turn is exported, shared and shown, and an
// upstream error string is the wrong thing to put somewhere that durable. The cause goes to
// the log, where an operator can read it and a reader of the conversation cannot.
const interruptedRoundMarker = "[run interrupted before it produced an answer; " +
	"the question above is unanswered — send it again to retry]"

// recordInterruptedRound writes the turn that a killed round never got to write.
//
// A round persists the person's message when it starts and the assistant's when it ends, so
// anything that stops the process in between leaves a question with nothing after it.
// REPRODUCED on the live stack 2026-08-16 by restarting the daemon mid-run: a user turn, no
// assistant turn, no cache_metrics row — byte for byte the fingerprint of the 2026-08-13
// event where the operator waited three minutes and asked the same thing again. Nothing
// recorded the loss, so silence was indistinguishable from a slow answer, and asking again
// was the only move available.
//
// The write uses context.WithoutCancel for the same reason flushPause does, and here the
// reason is sharper: this runs BECAUSE the context died, so inheriting its cancellation
// would drop the only record that the failure ever happened.
func (r *Runner) recordInterruptedRound(ctx context.Context, tr *turnTracker, roundErr error) error {
	if tr == nil || tr.answered || tr.paused {
		return nil
	}
	// A pause and an answer are the two ways a round ends well. Anything else needs a
	// cause to report; without one this stays out of the way rather than inventing a
	// failure for a conversation that never had one.
	if roundErr == nil && ctx.Err() == nil {
		return nil
	}
	cause := roundErr
	if cause == nil {
		cause = ctx.Err()
	}
	slog.Warn("runner: round ended with no answer; recording it in the conversation",
		"conv", tr.convID, "cause", cause)
	return r.Conv.AppendTurn(context.WithoutCancel(ctx), conversations.AppendTurnParams{
		ConversationID: tr.convID,
		Role:           llm.RoleAssistant,
		Content:        interruptedRoundMarker,
	})
}
