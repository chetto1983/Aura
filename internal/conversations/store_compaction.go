package conversations

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
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

// LoadPathCompaction returns the stored summary of the branch leafSeq ends, the read behind
// the cockpit's marker. A leafSeq <= 0 is the turn written last, which on a conversation with
// no turn is no compaction. A seq that names no turn is ErrTurnNotFound.
//
// The marker shows only a summary the loader would apply to this path. A row whose watermark
// is past the leaf is no compaction: another tab grew the branch and compacted it. A branch's
// own turns form one chain, because an append to a parent that already has a child opens a
// new branch (spec 2026-10-27, D4), so a watermark at or before the leaf is on its path.
func (s *Store) LoadPathCompaction(ctx context.Context, conversationID string, leafSeq int) (Compaction, bool, error) {
	if leafSeq <= 0 {
		latest, err := s.LatestTurnSeq(ctx, conversationID)
		if err != nil || latest == 0 {
			return Compaction{}, false, err
		}
		leafSeq = latest
	}
	branchID, err := s.branchOfTurn(ctx, conversationID, leafSeq)
	if err != nil {
		return Compaction{}, false, err
	}
	c, ok, err := s.LoadCompaction(ctx, conversationID, branchID)
	if err != nil || !ok || c.CoversThroughSeq > leafSeq {
		return Compaction{}, false, err
	}
	return c, true, nil
}

// branchOfTurn names the branch a path belongs to: its leaf's own branch id, "" for the
// canonical branch, and "" with ErrTurnNotFound when seq names no turn.
//
// The compaction row is keyed on it. Until 2026-08-16 both call sites passed "", so every
// branch of a conversation wrote and read ONE row: branch B was handed a summary describing
// branch A's turns at the same seq numbers, and B's watermark then buried A's.
func (s *Store) branchOfTurn(ctx context.Context, conversationID string, seq int) (string, error) {
	if seq <= 0 || seq > math.MaxInt32 {
		return "", ErrTurnNotFound
	}
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return "", err
	}
	var branch pgtype.UUID
	err = s.scoped(ctx, func(q *sqlc.Queries) error {
		row, qErr := q.GetTurnPointers(ctx, sqlc.GetTurnPointersParams{
			ConversationID: id, Seq: int32(seq),
		})
		branch = row.BranchID
		return qErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTurnNotFound
	}
	if err != nil {
		return "", fmt.Errorf("branch of turn %d in %s: %w", seq, conversationID, err)
	}
	if !branch.Valid || branch.Bytes == CanonicalBranchID {
		return "", nil
	}
	return uuid.UUID(branch.Bytes).String(), nil
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
