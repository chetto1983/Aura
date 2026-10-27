package conversations

// The conversation as a reader sees it (spec 2026-10-27, D5): every turn of every branch,
// each with the turn it continues, so the history snapshot can draw the tree the cockpit
// navigates without asking the server again.

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/llm"
)

// DisplayTurn is one message of the conversation with its place in the tree. Seq is the turn
// behind it and ParentSeq the turn it continues, 0 at a root. A result the repair synthesizes
// for a call that never got one has no turn: its Seq is 0 and it hangs from the assistant
// turn whose call it answers.
type DisplayTurn struct {
	Seq, ParentSeq int
	Message        llm.Message
}

// LoadDisplayTurns returns every turn of the conversation in seq order, repaired as the model
// context is, with the place of each one kept. A turn whose parent the repair dropped hangs
// from that parent's nearest ancestor still drawn.
func (s *Store) LoadDisplayTurns(ctx context.Context, conversationID string) ([]DisplayTurn, error) {
	turns, err := s.loadTurns(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return rebuildDisplayTurns(conversationID, turns)
}

func rebuildDisplayTurns(conversationID string, turns []Turn) ([]DisplayTurn, error) {
	out := make([]DisplayTurn, 0, len(turns))
	for _, t := range turns {
		msg, err := turnToMessage(t)
		if err != nil {
			return nil, fmt.Errorf("rebuild %s seq %d: %w", conversationID, t.Seq, err)
		}
		out = append(out, DisplayTurn{Seq: t.Seq, ParentSeq: t.ParentSeq, Message: msg})
	}
	return repairDisplayTurns(out), nil
}

// rebuildMessages is the model-facing rebuild: the same repair, without the tree.
func rebuildMessages(conversationID string, turns []Turn) ([]llm.Message, error) {
	display, err := rebuildDisplayTurns(conversationID, turns)
	if err != nil {
		return nil, err
	}
	return DisplayMessages(display), nil
}

var displayPairs = toolPairs[DisplayTurn]{
	message: func(t DisplayTurn) llm.Message { return t.Message },
	recovered: func(assistant DisplayTurn, call llm.ToolCall) DisplayTurn {
		return DisplayTurn{ParentSeq: assistant.Seq, Message: recoveredToolResult(assistant.Message, call)}
	},
}

// repairDisplayTurns runs the model's tool-pair repair and hangs every turn it keeps from the
// nearest ancestor it kept. A parent always precedes its child in seq order, so one pass
// knows each dropped turn's ancestor before any child asks for it.
func repairDisplayTurns(in []DisplayTurn) []DisplayTurn {
	kept := repairToolPairs(in, displayPairs)
	drawn := make(map[int]bool, len(kept))
	for _, t := range kept {
		drawn[t.Seq] = true
	}
	ancestor := make(map[int]int)
	nearest := func(seq int) int {
		if a, dropped := ancestor[seq]; dropped {
			return a
		}
		return seq
	}
	for _, t := range in {
		if !drawn[t.Seq] {
			ancestor[t.Seq] = nearest(t.ParentSeq)
		}
	}
	for i := range kept {
		kept[i].ParentSeq = nearest(kept[i].ParentSeq)
	}
	return kept
}

// DisplayMessages strips the tree from turns: what the display normalizers read.
func DisplayMessages(turns []DisplayTurn) []llm.Message {
	if len(turns) == 0 {
		return nil
	}
	out := make([]llm.Message, len(turns))
	for i, t := range turns {
		out[i] = t.Message
	}
	return out
}
