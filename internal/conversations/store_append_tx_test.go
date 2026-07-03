// Unit tier (no build tag): AppendTurnTx is the no-spill tx-inner insert body the 34-06
// ResumeCommitter calls with a tx-bound *sqlc.Queries. These tests drive it through the
// in-memory fakeDBTX (declared in store_fakedbtx_test.go) — no live Postgres, no
// pool.Begin — asserting the Seq>0 guard, that a small (<cap) turn inserts via the
// supplied queries, and that a DB error propagates. The real cross-store tx (pause claim
// + turn append in one db.WithTx) is exercised in 34-06's db_integration tier; the public
// AppendTurn's own spill/rollback stays covered by the db_integration append tests in
// store_test.go / store_sequence_test.go.

package conversations

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

// TestAppendTurnTx_RequiresSeq: the tx-inner body never allocates a seq, so a
// non-positive Seq is ErrSeqRequired before any DB touch.
func TestAppendTurnTx_RequiresSeq(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	q := sqlc.New(&fakeDBTX{})
	convID := uuid.Must(uuid.NewV7()).String()
	for _, seq := range []int{0, -1} {
		err := s.AppendTurnTx(context.Background(), q, AppendTurnParams{
			ConversationID: convID, Seq: seq, Role: "tool", Content: "ok",
		})
		if !errors.Is(err, ErrSeqRequired) {
			t.Errorf("AppendTurnTx(seq=%d): want ErrSeqRequired, got %v", seq, err)
		}
	}
}

// TestAppendTurnTx_InsertsViaSuppliedQueries: a small (<cap) resume/pause turn folds the
// turn + aggregate writes through the caller-supplied queries (no spill, no pool.Begin).
func TestAppendTurnTx_InsertsViaSuppliedQueries(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	q := sqlc.New(&fakeDBTX{}) // execErr nil: InsertConversationTurn + UpdateConversationAggregates succeed
	convID := uuid.Must(uuid.NewV7()).String()
	if err := s.AppendTurnTx(context.Background(), q, AppendTurnParams{
		ConversationID: convID, Seq: 5, Role: "tool", Content: "the user's short reply", ToolCallID: "call_1",
	}); err != nil {
		t.Fatalf("AppendTurnTx: %v", err)
	}
}

// TestAppendTurnTx_PropagatesDBError: a DB failure inside the supplied queries surfaces
// wrapped (the insertTurnAndAggregates "%w" chain), never swallowed.
func TestAppendTurnTx_PropagatesDBError(t *testing.T) {
	t.Parallel()
	boom := errors.New("insert failed")
	s := fakeStore(t, &fakeDBTX{})
	q := sqlc.New(&fakeDBTX{execErr: boom})
	convID := uuid.Must(uuid.NewV7()).String()
	err := s.AppendTurnTx(context.Background(), q, AppendTurnParams{
		ConversationID: convID, Seq: 5, Role: "tool", Content: "x",
	})
	if !errors.Is(err, boom) {
		t.Errorf("AppendTurnTx DB error must propagate: %v", err)
	}
}

// TestAppendTurnTx_BadConversationID: a malformed conversation id fails in
// appendTurnWrites before any DB touch.
func TestAppendTurnTx_BadConversationID(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	q := sqlc.New(&fakeDBTX{})
	err := s.AppendTurnTx(context.Background(), q, AppendTurnParams{
		ConversationID: "not-a-uuid", Seq: 1, Role: "tool", Content: "x",
	})
	if err == nil {
		t.Fatal("AppendTurnTx(bad conversation id): want error, got nil")
	}
}
