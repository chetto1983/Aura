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

func isRepeatedUserTurn(previous, current Turn) bool {
	if previous.Role != llm.RoleUser || current.Role != llm.RoleUser {
		return false
	}
	if isAlwaysBlock(previous) || isAlwaysBlock(current) {
		return false
	}
	if isCompaction(previous) || isCompaction(current) {
		return false
	}
	return previous.Content == current.Content
}
