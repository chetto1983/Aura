package conversations

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrTurnNotFound is returned when a fork targets a seq that does not exist in the
// conversation — mapped to a clean 404 at the REST boundary (T-25-26).
var ErrTurnNotFound = errors.New("conversations: turn not found")

// CanonicalBranchID is the all-zero sentinel branch every pre-0017 turn is backfilled
// onto by migration 0017 (D-09 / CHAT-05). A non-branched conversation's whole history
// lives on this branch, so a path walk from its leaf reconstructs the same linear turn
// list ListTurnsBySeq returns (the byte-identity contract, store.go:250). New sibling
// branches (plan 25-07 edit/regenerate) mint a fresh uuid instead.
var CanonicalBranchID = uuid.UUID{}

// LatestTurnSeq returns the seq of the turn written last, 0 for a conversation with no
// turns. It is the head a run continues when nothing names another, and the branch a reload
// opens on: every append chains to the turn before it unless told otherwise, so the turn
// written last ends the branch that was being written.
func (s *Store) LatestTurnSeq(ctx context.Context, conversationID string) (int, error) {
	return scopedSeq(ctx, s, "latest turn seq", "conversation_id", conversationID,
		func(q *sqlc.Queries, id pgtype.UUID) (int32, error) {
			return q.LatestTurnSeq(ctx, id)
		})
}

// loadBranchTurns walks the SELECTED branch path (leaf -> root via parent_seq) and
// returns the turns in root->leaf (seq ASC) order, rehydrating sidecar-spilled content
// from disk exactly like loadTurns. The reconstruction is a PURE function of the
// persisted rows: two consecutive calls return byte-identical slices (the LoadHistory
// byte-identity contract extended to the branch path — Pitfall 3 / Req#8). A leafSeq of
// 0 (or a seq not present) yields an empty path. The new code is ONLY the parent/branch
// pointer walk; turnFromRow / sidecar rehydration are reused unchanged.
func (s *Store) loadBranchTurns(ctx context.Context, conversationID string, leafSeq int) ([]Turn, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("load branch turns: %w", err)
	}
	// Narrow leafSeq inside the proven-safe branch so the int32 conversion is guarded
	// at the call site (CodeQL go/incorrect-integer-conversion); a non-positive or
	// overflowing leafSeq leaves seq at 0, which yields an empty path as documented.
	var seq int32
	if leafSeq > 0 && leafSeq <= math.MaxInt32 {
		seq = int32(leafSeq)
	}
	var rows []sqlc.ListTurnsByBranchPathRow
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		listed, lErr := q.ListTurnsByBranchPath(ctx, sqlc.ListTurnsByBranchPathParams{
			ConversationID: id,
			Seq:            seq,
		})
		if lErr != nil {
			return fmt.Errorf("load branch turns %s leaf %d: %w", conversationID, leafSeq, lErr)
		}
		rows = listed
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]Turn, 0, len(rows))
	for _, r := range rows {
		t := turnFromRow(branchPathRowAsSeqRow(r))
		if t.ContentSidecarPath != "" {
			// Reconstruct-don't-trust: the DB path is a did-spill flag; the fenced
			// os.Root read is reconstructed from (runDir, convID, seq) — shared with
			// loadTurns so both loaders converge on one model (LOOP-05 / D-08).
			data, rerr := s.readTurnSidecar(conversationID, t.Seq)
			if rerr != nil {
				return nil, fmt.Errorf("load branch turns %s seq %d: read sidecar: %w",
					conversationID, t.Seq, rerr)
			}
			t.Content = string(data)
		}
		out = append(out, t)
	}
	return out, nil
}

// branchPathRowAsSeqRow adapts the field-identical ListTurnsByBranchPath row onto the
// ListTurnsBySeq row so the single turnFromRow projection serves both loaders (the two
// sqlc query-row structs carry the same 11 SELECTed columns; only the source query
// differs). Keeping ONE projection preserves the byte-identity guarantee across the
// linear and path-walk loaders.
func branchPathRowAsSeqRow(r sqlc.ListTurnsByBranchPathRow) sqlc.ListTurnsBySeqRow {
	return sqlc.ListTurnsBySeqRow(r)
}

// LoadBranchHistory reconstructs the history of one path: the turns from leafSeq up to its
// root, repaired exactly like LoadHistory. A leafSeq <= 0 is the branch written last, the one
// a run continues when nothing names another, which is what a share shows (spec 2026-10-27,
// D12). For a non-branched conversation the result is byte-identical to LoadHistory. The
// returned slice is raw; the L1/L2/L2.5 ladder (context.go) is applied by the caller.
func (s *Store) LoadBranchHistory(ctx context.Context, conversationID string, leafSeq int) ([]llm.Message, error) {
	if leafSeq <= 0 {
		latest, err := s.LatestTurnSeq(ctx, conversationID)
		if err != nil {
			return nil, err
		}
		leafSeq = latest
	}
	turns, err := s.loadBranchTurns(ctx, conversationID, leafSeq)
	if err != nil {
		return nil, err
	}
	return rebuildMessages(conversationID, turns)
}

// ForkBranch appends a sibling of the turn at divergeSeq: a fresh turn that chains off the
// SAME parent, so it replaces divergeSeq on a new branch rather than continuing after it.
// This is an edit of a user turn or a regenerate. The old branch is left as it is. The
// append gives the fork a branch id of its own, because its parent already has a child,
// and every turn appended after it inherits that id. It returns the new turn's seq, which
// the caller re-runs over (LoadManagedHistoryForBranch). divergeSeq must exist
// (ErrTurnNotFound).
func (s *Store) ForkBranch(ctx context.Context, conversationID string, divergeSeq int, role, content string) (int, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("fork branch: %w", err)
	}
	if divergeSeq <= 0 || divergeSeq > math.MaxInt32 {
		return 0, fmt.Errorf("fork branch %s seq %d: %w", conversationID, divergeSeq, ErrTurnNotFound)
	}
	parent := 0
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		diverge, qErr := q.GetTurnPointers(ctx, sqlc.GetTurnPointersParams{ConversationID: id, Seq: int32(divergeSeq)})
		if qErr != nil {
			return qErr
		}
		if diverge.ParentSeq.Valid {
			parent = int(diverge.ParentSeq.Int32)
		}
		return nil
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("fork branch %s seq %d: %w", conversationID, divergeSeq, ErrTurnNotFound)
		}
		return 0, fmt.Errorf("fork branch %s seq %d: read diverge turn: %w", conversationID, divergeSeq, err)
	}
	return s.AppendTurnSeq(ctx, AppendTurnParams{
		ConversationID: conversationID, Role: role, Content: content, ParentSeq: &parent,
	})
}
