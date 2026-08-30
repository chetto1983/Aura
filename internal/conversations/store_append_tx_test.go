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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestAppendTurnTx_DeliveryKeyConflictSkipsAggregateUpdate(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{execTags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0")}}
	s := fakeStore(t, fake)
	convID := uuid.Must(uuid.NewV7()).String()
	if err := s.AppendTurnTx(context.Background(), sqlc.New(fake), AppendTurnParams{
		ConversationID: convID, Seq: 5, Role: "assistant", Content: "card", DeliveryKey: "job-1:terminal",
	}); err != nil {
		t.Fatalf("AppendTurnTx duplicate delivery: %v", err)
	}
	if fake.execCalls != 1 {
		t.Fatalf("exec calls = %d, want only the idempotent insert and no aggregate update", fake.execCalls)
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

// TestAppendTurnTx_RejectsSpill: content over turnCapBytes would spill to a sidecar the
// no-cleanup tx path could orphan on rollback, so AppendTurnTx rejects it with
// ErrContentSpillUnsupported BEFORE writing any file (WR-01). A tiny cap keeps the fixture
// cheap; the assertion also proves no sidecar was written under runDir.
func TestAppendTurnTx_RejectsSpill(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	s := newFakeStore(&Store{q: sqlc.New(&fakeDBTX{}), runDir: runDir, turnCapBytes: 8})
	convID := uuid.Must(uuid.NewV7()).String()
	err := s.AppendTurnTx(context.Background(), sqlc.New(&fakeDBTX{}), AppendTurnParams{
		ConversationID: convID, Seq: 5, Role: "tool", Content: strings.Repeat("x", 9),
	})
	if !errors.Is(err, ErrContentSpillUnsupported) {
		t.Fatalf("AppendTurnTx(over-cap content): want ErrContentSpillUnsupported, got %v", err)
	}
	// The reject fires before maybeSpill, so no sidecar file exists anywhere under runDir.
	convDir := filepath.Join(runDir, "conversations", convID)
	if entries, statErr := os.ReadDir(convDir); statErr == nil && len(entries) > 0 {
		t.Fatalf("AppendTurnTx rejected the spill but wrote %d sidecar file(s) under %s", len(entries), convDir)
	}
}

// TestAppendTurnTx_InlineAtCap: content exactly at turnCapBytes stays inline (no spill, no
// reject) — the boundary the WR-01 guard shares with maybeSpill (len <= cap is inline).
func TestAppendTurnTx_InlineAtCap(t *testing.T) {
	t.Parallel()
	s := newFakeStore(&Store{q: sqlc.New(&fakeDBTX{}), runDir: t.TempDir(), turnCapBytes: 8})
	convID := uuid.Must(uuid.NewV7()).String()
	if err := s.AppendTurnTx(context.Background(), sqlc.New(&fakeDBTX{}), AppendTurnParams{
		ConversationID: convID, Seq: 5, Role: "tool", Content: strings.Repeat("x", 8),
	}); err != nil {
		t.Fatalf("AppendTurnTx(at-cap content): want inline success, got %v", err)
	}
}

func TestAppendTurnTx_AppliesRedactionBeforeSpillGuard(t *testing.T) {
	configured := strings.Repeat("configured-secret-", 4)
	secret.ConfigureExactRedactor([]string{"AURA_DB_TOKEN=" + configured})
	t.Cleanup(func() { secret.ConfigureExactRedactor(os.Environ()) })

	capBytes := len(secret.ConfiguredValuePlaceholder)
	s := newFakeStore(&Store{q: sqlc.New(&fakeDBTX{}), runDir: t.TempDir(), turnCapBytes: capBytes})
	convID := uuid.Must(uuid.NewV7()).String()
	if err := s.AppendTurnTx(context.Background(), sqlc.New(&fakeDBTX{}), AppendTurnParams{
		ConversationID: convID, Seq: 5, Role: "tool", Content: configured,
	}); err != nil {
		t.Fatalf("AppendTurnTx must measure the redacted content before the spill guard: %v", err)
	}
}
