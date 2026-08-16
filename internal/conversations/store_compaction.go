package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Compaction is one branch's durable summary of its own earlier turns (migration 0096).
//
// It is a VIEW artifact, never a substitute for history: aura.conversation_turns still
// holds every turn this summary describes, and CoversThroughSeq is the seq up to which
// the summary speaks for them.
type Compaction struct {
	Summary          string
	Model            string
	CoversThroughSeq int
	SourceTurns      int
}

// LoadCompaction returns the branch's stored summary. The bool is false when the branch
// has never been compacted -- the normal state of every conversation that fits its
// window -- which is a fact, not an error.
func (s *Store) LoadCompaction(ctx context.Context, conversationID, branchID string) (Compaction, bool, error) {
	conversation, branch, err := compactionKey(conversationID, branchID)
	if err != nil {
		return Compaction{}, false, fmt.Errorf("load compaction: %w", err)
	}
	var row sqlc.AuraConversationCompactions
	err = s.scoped(ctx, func(q *sqlc.Queries) error {
		loaded, qErr := q.GetConversationCompaction(ctx, sqlc.GetConversationCompactionParams{
			ConversationID: conversation, BranchID: branch,
		})
		if qErr != nil {
			return qErr
		}
		row = loaded
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Compaction{}, false, nil
	}
	if err != nil {
		return Compaction{}, false, fmt.Errorf("load compaction %s: %w", conversationID, err)
	}
	return Compaction{
		Summary:          row.Summary,
		Model:            row.Model,
		CoversThroughSeq: int(row.CoversThroughSeq),
		SourceTurns:      int(row.SourceTurns),
	}, true, nil
}

// SaveCompaction stores the branch's summary, advancing the watermark.
//
// Zero rows back is SUCCESS, not a failure: the statement refuses to move the watermark
// backwards, so an empty result means a concurrent turn on the same conversation already
// summarized further ahead. Overwriting it with this older, shorter summary would drop
// the turns between the two watermarks out of the model's view while leaving the ladder
// convinced they had been condensed.
func (s *Store) SaveCompaction(ctx context.Context, conversationID, branchID string, c Compaction) error {
	conversation, branch, err := compactionKey(conversationID, branchID)
	if err != nil {
		return fmt.Errorf("save compaction: %w", err)
	}
	if c.CoversThroughSeq <= 0 || c.Summary == "" {
		return fmt.Errorf("save compaction %s: a summary must cover at least one turn", conversationID)
	}
	err = s.scoped(ctx, func(q *sqlc.Queries) error {
		_, qErr := q.UpsertConversationCompaction(ctx, sqlc.UpsertConversationCompactionParams{
			ConversationID:   conversation,
			BranchID:         branch,
			CoversThroughSeq: int32(c.CoversThroughSeq),
			Summary:          c.Summary,
			Model:            c.Model,
			SourceTurns:      int32(c.SourceTurns),
		})
		if errors.Is(qErr, pgx.ErrNoRows) {
			return nil
		}
		return qErr
	})
	if err != nil {
		return fmt.Errorf("save compaction %s: %w", conversationID, err)
	}
	return nil
}

// compactionKey parses the pair every statement is keyed on. An empty branch is the
// canonical branch, the same all-zero uuid the turns carry.
func compactionKey(conversationID, branchID string) (conversation, branch pgtype.UUID, err error) {
	conversation, err = db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return conversation, branch, err
	}
	if branchID == "" {
		return conversation, pgtype.UUID{Bytes: CanonicalBranchID, Valid: true}, nil
	}
	branch, err = db.ParseUUID("branch_id", branchID)
	return conversation, branch, err
}
