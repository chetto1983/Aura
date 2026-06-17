package conversations

import (
	"context"
	"fmt"
	"os"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// CanonicalBranchID is the all-zero sentinel branch every pre-0017 turn is backfilled
// onto by migration 0017 (D-09 / CHAT-05). A non-branched conversation's whole history
// lives on this branch, so a path walk from its leaf reconstructs the same linear turn
// list ListTurnsBySeq returns (the byte-identity contract, store.go:250). New sibling
// branches (plan 25-07 edit/regenerate) mint a fresh uuid instead.
var CanonicalBranchID = uuid.UUID{}

// CanonicalBranchLeaf returns the deepest seq of a conversation's canonical branch — the
// default leaf LoadBranchHistory walks from when no specific branch is selected. It is 0
// for a conversation with no turns. The walk from this leaf over a properly-chained
// canonical branch (the 0017 backfill produces parent_seq = seq-1) yields the linear
// history (Pitfall 3: only body turns differ per branch; the head stays byte-identical).
func (s *Store) CanonicalBranchLeaf(ctx context.Context, conversationID string) (int, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("canonical branch leaf: %w", err)
	}
	leaf, err := s.q.CanonicalBranchLeafSeq(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("canonical branch leaf %s: %w", conversationID, err)
	}
	return int(leaf), nil
}

// loadBranchTurns walks the SELECTED branch path (leaf -> root via parent_seq) and
// returns the turns in root->leaf (seq ASC) order, rehydrating sidecar-spilled content
// from disk exactly like loadTurns. The reconstruction is a PURE function of the
// persisted rows: two consecutive calls return byte-identical slices (the LoadHistory
// byte-identity contract extended to the branch path — Pitfall 3 / Req#8). A leafSeq of
// 0 (or a seq not present) yields an empty path. The new code is ONLY the parent/branch
// pointer walk; turnFromRow / sidecar rehydration are reused unchanged.
func (s *Store) loadBranchTurns(ctx context.Context, conversationID string, leafSeq int) ([]Turn, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("load branch turns: %w", err)
	}
	rows, err := s.q.ListTurnsByBranchPath(ctx, sqlc.ListTurnsByBranchPathParams{
		ConversationID: id,
		Seq:            int32(leafSeq),
	})
	if err != nil {
		return nil, fmt.Errorf("load branch turns %s leaf %d: %w", conversationID, leafSeq, err)
	}
	out := make([]Turn, 0, len(rows))
	for _, r := range rows {
		t := turnFromRow(branchPathRowAsSeqRow(r))
		if t.ContentSidecarPath != "" {
			data, rerr := os.ReadFile(t.ContentSidecarPath)
			if rerr != nil {
				return nil, fmt.Errorf("load branch turns %s seq %d: read sidecar %q: %w",
					conversationID, t.Seq, t.ContentSidecarPath, rerr)
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

// LoadBranchHistory reconstructs the loop history for the SELECTED branch path, the
// path-aware analog of LoadHistory (store.go:255). It walks leaf->root from the given
// leaf seq, projects each turn to an llm.Message, and repairs tool-message pairs along
// the path exactly like LoadHistory. For the canonical branch leaf of a non-branched
// conversation the result is byte-identical to LoadHistory (the migration backfilled a
// complete parent_seq = seq-1 chain — RESEARCH A3). The returned slice is raw; the
// L1/L2/L2.5 ladder (context.go) is applied by the caller around it.
func (s *Store) LoadBranchHistory(ctx context.Context, conversationID string, leafSeq int) ([]llm.Message, error) {
	turns, err := s.loadBranchTurns(ctx, conversationID, leafSeq)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, 0, len(turns))
	for _, t := range turns {
		msg, err := turnToMessage(t)
		if err != nil {
			return nil, fmt.Errorf("load branch history %s seq %d: %w", conversationID, t.Seq, err)
		}
		out = append(out, msg)
	}
	return repairToolMessagePairs(out), nil
}

// SetBranchPointers sets a turn's branch/parent pointers — the branch-write seam plan
// 25-07 uses when an edit/regenerate forks a new sibling branch off an existing parent
// turn. parentSeq <= 0 writes a NULL parent (a branch root). It validates the ids at the
// boundary (Pitfall 5) and routes through the SetTurnBranchPointers sqlc query.
func (s *Store) SetBranchPointers(ctx context.Context, conversationID string, seq int, branchID uuid.UUID, parentSeq int) error {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("set branch pointers: %w", err)
	}
	parent := pgtype.Int4{}
	if parentSeq > 0 {
		parent = pgtype.Int4{Int32: int32(parentSeq), Valid: true}
	}
	if err := s.q.SetTurnBranchPointers(ctx, sqlc.SetTurnBranchPointersParams{
		ConversationID: id,
		Seq:            int32(seq),
		BranchID:       pgtype.UUID{Bytes: branchID, Valid: true},
		ParentSeq:      parent,
	}); err != nil {
		return fmt.Errorf("set branch pointers %s seq %d: %w", conversationID, seq, err)
	}
	return nil
}
