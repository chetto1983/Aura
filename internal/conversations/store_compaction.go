package conversations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/google/uuid"
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

// branchIDForLeaf names the branch a replayed path belongs to: the leaf's own branch id.
//
// It costs one primary-key read per branch load, and it buys the thing the compaction row
// is keyed on. Without it both call sites passed "", so every branch of a conversation
// wrote and read ONE row: branch B would be handed a summary describing branch A's turns
// at the same seq numbers, and B's watermark would then bury A's.
//
// A failure degrades to the canonical branch rather than failing the turn — that is the
// behaviour every conversation had before branches existed, and a summary filed under the
// wrong branch is a worse outcome than an extra summarizer call only when we KNOW the
// branch; not knowing it, the canonical row is the honest place.
func (s *Store) branchIDForLeaf(ctx context.Context, conversationID string, leafSeq int) string {
	if leafSeq <= 0 || leafSeq > math.MaxInt32 {
		return ""
	}
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return ""
	}
	var branch pgtype.UUID
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		row, qErr := q.GetTurnPointers(ctx, sqlc.GetTurnPointersParams{
			ConversationID: id, Seq: int32(leafSeq),
		})
		if qErr != nil {
			return qErr
		}
		branch = row.BranchID
		return nil
	}); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("branch id unreadable; compaction falls back to the canonical branch",
				"conversation_id", redact.Line(conversationID), "leaf_seq", leafSeq, "err", err)
		}
		return ""
	}
	if !branch.Valid || branch.Bytes == CanonicalBranchID {
		return ""
	}
	return uuid.UUID(branch.Bytes).String()
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
