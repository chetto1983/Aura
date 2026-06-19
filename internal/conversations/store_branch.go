package conversations

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Branch is one navigable branch path of a conversation tree (D-09 / CHAT-05): the
// LeafSeq tip the BranchPicker selects + the BranchID it belongs to. ParentSeq is the
// divergence point (NULL → 0 for a root branch). The canonical (all-zero) branch sorts
// first (ListBranchLeaves ORDER BY branch_id ASC), so a non-branched conversation reports
// exactly one branch whose leaf is its last turn.
type Branch struct {
	LeafSeq   int
	BranchID  uuid.UUID
	ParentSeq int
}

// ErrTurnNotFound is returned when a fork targets a seq that does not exist in the
// conversation — mapped to a clean 404 at the REST boundary (T-25-26).
var ErrTurnNotFound = errors.New("conversations: turn not found")

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
	// Narrow leafSeq inside the proven-safe branch so the int32 conversion is guarded
	// at the call site (CodeQL go/incorrect-integer-conversion); a non-positive or
	// overflowing leafSeq leaves seq at 0, which yields an empty path as documented.
	var seq int32
	if leafSeq > 0 && leafSeq <= math.MaxInt32 {
		seq = int32(leafSeq)
	}
	rows, err := s.q.ListTurnsByBranchPath(ctx, sqlc.ListTurnsByBranchPathParams{
		ConversationID: id,
		Seq:            seq,
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

// ListBranches returns the conversation's navigable branch set — one Branch per leaf
// turn (a turn no other turn chains off of). The BranchPicker navigates among sibling
// leaves; a re-run continues over the selected leaf's path (LoadManagedHistoryForBranch).
// A non-branched conversation reports exactly one branch (its canonical leaf), so the
// picker stays hidden until an edit/regenerate forks a sibling. The order is stable
// (canonical branch first, then by seq).
func (s *Store) ListBranches(ctx context.Context, conversationID string) ([]Branch, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	rows, err := s.q.ListBranchLeaves(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list branches %s: %w", conversationID, err)
	}
	out := make([]Branch, 0, len(rows))
	for _, r := range rows {
		b := Branch{LeafSeq: int(r.Seq)}
		if r.BranchID.Valid {
			b.BranchID = r.BranchID.Bytes
		}
		if r.ParentSeq.Valid {
			b.ParentSeq = int(r.ParentSeq.Int32)
		}
		out = append(out, b)
	}
	return out, nil
}

// ForkBranch creates a new sibling branch by appending a fresh turn that chains off the
// SAME parent as the diverging turn at divergeSeq — so the new turn REPLACES divergeSeq
// on a new branch rather than continuing after it (the edit-a-user-turn / regenerate
// semantics, RESEARCH OQ3 full tree). The old branch stays queryable (the diverging turn
// and its descendants are untouched). It allocates the next global seq, inserts the turn,
// and sets its branch_id (a fresh uuid) + parent_seq in ONE transaction so a partial fork
// can never leave a turn with default (canonical) pointers. Returns the new leaf seq and
// its branch id; the caller re-runs over LoadManagedHistoryForBranch(newLeaf). The
// messages[0] head is unchanged by construction — only body turns differ per branch
// (Pitfall 3 / the CAP-04 cache invariant). divergeSeq must exist (→ ErrTurnNotFound).
func (s *Store) ForkBranch(ctx context.Context, conversationID string, divergeSeq int, role, content string) (int, uuid.UUID, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return 0, uuid.UUID{}, fmt.Errorf("fork branch: %w", err)
	}
	branchID := uuid.New()
	var newSeq int
	var spilledPath string
	run := func() error {
		return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
			diverge, err := q.GetTurnPointers(ctx, sqlc.GetTurnPointersParams{ConversationID: id, Seq: int32(divergeSeq)})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("fork branch %s seq %d: %w", conversationID, divergeSeq, ErrTurnNotFound)
				}
				return fmt.Errorf("fork branch %s seq %d: read diverge turn: %w", conversationID, divergeSeq, err)
			}
			seq, err := s.allocateTurnSeq(ctx, q, conversationID)
			if err != nil {
				return err
			}
			newSeq = seq
			p := AppendTurnParams{ConversationID: conversationID, Seq: seq, Role: role, Content: content}
			turn, agg, err := s.appendTurnWrites(p)
			if err != nil {
				return err
			}
			spilledPath = turn.ContentSidecarPath.String
			if err := insertTurnAndAggregates(ctx, q, turn, agg); err != nil {
				return err
			}
			return q.SetTurnBranchPointers(ctx, sqlc.SetTurnBranchPointersParams{
				ConversationID: id,
				Seq:            int32(seq),
				BranchID:       pgtype.UUID{Bytes: branchID, Valid: true},
				ParentSeq:      diverge.ParentSeq, // same parent as the diverging turn → a sibling
			})
		})
	}
	if err := cleanupSidecarOnTxError(run, func() string { return spilledPath }); err != nil {
		return 0, uuid.UUID{}, err
	}
	return newSeq, branchID, nil
}
