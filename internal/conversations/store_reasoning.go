package conversations

import (
	"context"
	"fmt"
)

// TurnReasoning is the DISPLAY-ONLY projection of one answer-shaped assistant
// turn's reasoning columns (amendment #91 / fix-plan 1.12). It deliberately does
// NOT ride Turn/llm.Message: the history rebuild (LoadHistory/loadTurns) must stay
// structurally incapable of carrying CoT back into the model context. Reasoning is
// "" for a NULL column (pre-migration turns, redacted or disabled streams) — the
// caller renders no drawer for those, the correct degrade.
type TurnReasoning struct {
	Seq        int
	Reasoning  string
	DurationMS int64
}

// ListTurnReasoning returns one row per answer-shaped assistant turn (role
// assistant, no tool_calls payload) in seq order — the SAME conversation-wide,
// branch-unfiltered scope LoadHistory reads — so a snapshot consumer can pair the
// rows positionally with the assistant no-tool-call messages LoadHistory yields:
// repairToolMessagePairs never drops, adds, or reorders such messages, so the two
// ordered lists are index-aligned by construction (pinned in agui's attach tests).
func (s *Store) ListTurnReasoning(ctx context.Context, conversationID string) ([]TurnReasoning, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list turn reasoning: %w", err)
	}
	rows, err := s.q.ListAssistantTurnReasoning(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list turn reasoning %s: %w", conversationID, err)
	}
	out := make([]TurnReasoning, 0, len(rows))
	for _, r := range rows {
		out = append(out, TurnReasoning{
			Seq:        int(r.Seq),
			Reasoning:  r.Reasoning.String,
			DurationMS: r.ReasoningDurationMs.Int64,
		})
	}
	return out, nil
}
