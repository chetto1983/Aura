// context_rot.go carries the L2.5 audit trail — the context_rot_events row written when
// the deterministic ladder drops history it could not fit, plus the read projection the
// cockpit's context-budget gauge marks on its fill bar. It is split out of context.go on
// touch (600-LOC cap, CLAUDE.md §Behavioral rules): the ladder is a pure function of
// (turns, cfg, encoder), and this is the ONE place it reaches a database — a different
// concern, and the only reason context.go needed db/sqlc/time at all.
package conversations

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// rotActionHardDropPairs is the context_rot_events.action value written when L2.5
// drops oldest user/assistant pairs (amendment #22).
const rotActionHardDropPairs = "hard_drop_pairs"

// rotEmitter is the narrow write surface ApplyContextLadder needs to record an L2.5
// drop. *Store satisfies it; unit tests pass a fake (no DB).
type rotEmitter interface {
	insertContextRotEvent(ctx context.Context, conversationID string, pairsDropped, before, after int) error
}

// insertContextRotEvent records one L2.5 hard-drop audit row (Store impl).
func (s *Store) insertContextRotEvent(ctx context.Context, conversationID string, pairsDropped, before, after int) error {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("context rot event: %w", err)
	}
	return s.scoped(ctx, func(q *sqlc.Queries) error {
		if iErr := q.InsertContextRotEvent(ctx, sqlc.InsertContextRotEventParams{
			ConversationID: id,
			Action:         rotActionHardDropPairs,
			PairsDropped:   int32(pairsDropped),
			TokensBefore:   int32(before),
			TokensAfter:    int32(after),
		}); iErr != nil {
			return fmt.Errorf("insert context rot event %s: %w", conversationID, iErr)
		}
		return nil
	})
}

// RotEvent is the domain projection of one aura.context_rot_events row — the
// microcompact ladder audit trail (L2.5 hard-drop). PairsDropped/TokensBefore/
// TokensAfter quantify one drop; TS is the RFC3339 timestamp. The web cockpit's
// context-budget gauge marks these on the fill bar (D-11, Phase 25).
type RotEvent struct {
	TS           string // RFC3339
	Action       string
	PairsDropped int
	TokensBefore int
	TokensAfter  int
}

// ListContextRotEvents returns the microcompact ladder events for one conversation
// in ts-ASC order via the existing ListContextRotEvents sqlc query (no new query or
// migration — read-only projection at the pgtype boundary). A malformed id is a
// parse error the caller maps to a clean 404.
func (s *Store) ListContextRotEvents(ctx context.Context, conversationID string) ([]RotEvent, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list context rot events: %w", err)
	}
	var rows []sqlc.AuraContextRotEvents
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		listed, lErr := q.ListContextRotEvents(ctx, id)
		if lErr != nil {
			return fmt.Errorf("list context rot events %s: %w", conversationID, lErr)
		}
		rows = listed
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]RotEvent, 0, len(rows))
	for _, r := range rows {
		ts := ""
		if r.Ts.Valid {
			ts = r.Ts.Time.Format(time.RFC3339)
		}
		out = append(out, RotEvent{
			TS:           ts,
			Action:       r.Action,
			PairsDropped: int(r.PairsDropped),
			TokensBefore: int(r.TokensBefore),
			TokensAfter:  int(r.TokensAfter),
		})
	}
	return out, nil
}
