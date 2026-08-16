package conversations

import "github.com/chetto1983/aura/internal/llm"

// dropRepeatedUserTurns collapses a user turn whose immediate predecessor is a user turn
// with the same content, keeping the later one.
//
// A run that dies without persisting anything leaves the person's message in the history
// with no answer beside it, and they do the only thing they can: they send it again.
// MEASURED on the live deployment, conversation 019ffabe seq 76 and 77 -- the same 42
// characters three minutes apart, nothing in between, answered only on the second attempt.
// From then on every later turn asked the model that question twice, because nothing in
// this package ever looked at two adjacent user turns.
//
// Only byte-identical repeats go. Two DIFFERENT messages in a row are a person typing twice
// before the agent answers -- "via non capisci un cazzo" then "percè????", from the same
// corpus -- which is a real exchange, and merging it would put words in their mouth.
//
// The later turn is the one kept, not the earlier: it is the message the assistant actually
// answered, and it is what splitHeadHistoryActive anchors the active round on.
//
// This runs on the PERSISTED turns, before injectAlwaysBlock, so the synthetic head can
// never be a candidate. The marker guards below are belt and braces for the paths that
// hand back a history already carrying one.
func dropRepeatedUserTurns(turns []Turn) []Turn {
	if len(turns) < 2 {
		return turns
	}
	out := make([]Turn, 0, len(turns))
	for _, turn := range turns {
		if len(out) > 0 && isRepeatedUserTurn(out[len(out)-1], turn) {
			out[len(out)-1] = turn
			continue
		}
		out = append(out, turn)
	}
	return out
}

// unansweredMarker stands in the wire copy where an answer never came.
//
// Deliberately says what is KNOWN — no answer was produced — and not why. A crashed run and
// a person who typed twice before the agent replied leave the identical shape in storage,
// and the runner's own marker is the one that can name a cause, because it is written by
// the round that failed and knows it.
const unansweredMarker = "[no answer was produced for the message above]"

// markUnansweredUserTurns inserts that marker, on the WIRE COPY only, after any user turn
// that another user turn follows.
//
// recordInterruptedRound covers the round that dies while something is still running to
// write it down: a cancellation, a failing provider, a SIGTERM. It cannot cover a SIGKILL or
// a crash, because no moment survives in which to write. This is the other half, and it
// needs no sweep and no grace period: by the time a history is read back, the round that
// produced it is over by definition, so a user turn with another user turn after it went
// unanswered as a matter of record rather than of timing.
//
// The approach is hermes-agent's (`repair_empty_non_final_messages`, whose reasoning applies
// unchanged here): repair the per-call copy, never the stored history, so the conversation
// the person scrolls stays exactly what happened. Its specific pass -- substituting a
// placeholder into EMPTY non-final messages, which some providers reject with a 400 that
// then poisons every later turn -- is deliberately NOT ported: measured 2026-08-16, Aura
// holds zero turns of that shape, so porting it would be curing a disease this corpus does
// not have.
func markUnansweredUserTurns(turns []Turn) []Turn {
	if len(turns) < 2 {
		return turns
	}
	out := make([]Turn, 0, len(turns)+1)
	for index, turn := range turns {
		out = append(out, turn)
		if index+1 >= len(turns) {
			continue
		}
		if !isPlainUserTurn(turn) || !isPlainUserTurn(turns[index+1]) {
			continue
		}
		out = append(out, Turn{
			Seq: turn.Seq, Role: llm.RoleAssistant, Content: unansweredMarker,
		})
	}
	return out
}

// isPlainUserTurn excludes the synthetic user-role turns this package injects. They are
// bookkeeping, not anybody's message, and an answer was never owed to them.
func isPlainUserTurn(turn Turn) bool {
	return turn.Role == llm.RoleUser && !isAlwaysBlock(turn) && !isCompaction(turn)
}

func isRepeatedUserTurn(previous, current Turn) bool {
	if !isPlainUserTurn(previous) || !isPlainUserTurn(current) {
		return false
	}
	return previous.Content == current.Content
}
