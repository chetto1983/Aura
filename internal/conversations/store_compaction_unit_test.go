// Unit tier (no build tag) for the compaction row's guards and its branch read, driven
// through the in-memory DBTX fake (store_fakedbtx_test.go). Under db_integration the database
// answers for these guards: its CHECKs refuse an empty summary, and a seq that names no turn
// finds no row. Here the fake answers every statement, so each guard is the only thing
// standing between a bad input and a write or a wrong branch.
package conversations

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func turnPointersRow(seq int32, branch uuid.UUID) *fakeRow {
	return &fakeRow{values: []any{
		seq, pgtype.UUID{Bytes: branch, Valid: true}, pgtype.Int4{Int32: seq - 1, Valid: true}, "user",
	}}
}

func TestBranchOfTurn_FakeAnswers(t *testing.T) {
	t.Parallel()
	conv := uuid.Must(uuid.NewV7()).String()
	fork := uuid.Must(uuid.NewV7())
	boom := errors.New("branch read failed")
	for name, tc := range map[string]struct {
		seq     int
		row     *fakeRow
		want    string
		wantErr error
	}{
		// The guard answers before the query: the fake would return a branch.
		"seq 0":             {seq: 0, row: turnPointersRow(1, fork), wantErr: ErrTurnNotFound},
		"seq -1":            {seq: -1, row: turnPointersRow(1, fork), wantErr: ErrTurnNotFound},
		"past int32":        {seq: math.MaxInt32 + 1, row: turnPointersRow(1, fork), wantErr: ErrTurnNotFound},
		"the last int32":    {seq: math.MaxInt32, row: turnPointersRow(math.MaxInt32, fork), want: fork.String()},
		"a forked turn":     {seq: 5, row: turnPointersRow(5, fork), want: fork.String()},
		"a canonical turn":  {seq: 5, row: turnPointersRow(5, CanonicalBranchID), want: ""},
		"no such turn":      {seq: 5, row: &fakeRow{scanErr: pgx.ErrNoRows}, wantErr: ErrTurnNotFound},
		"an unreadable row": {seq: 5, row: &fakeRow{scanErr: boom}, wantErr: boom},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := fakeStore(t, &fakeDBTX{queryRowVal: tc.row}).branchOfTurn(context.Background(), conv, tc.seq)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("branchOfTurn(%d) error = %v, want %v", tc.seq, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("branchOfTurn(%d) = %q, want %q", tc.seq, got, tc.want)
			}
		})
	}
}

func TestBranchOfTurn_FakeMalformedConversationID(t *testing.T) {
	t.Parallel()
	_, err := fakeStore(t, &fakeDBTX{queryRowVal: turnPointersRow(5, uuid.Must(uuid.NewV7()))}).
		branchOfTurn(context.Background(), "not-a-uuid", 5)
	if err == nil || errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("branchOfTurn on a malformed id = %v, want a parse error", err)
	}
}

// The fake accepts every upsert (no row back is success), so a refused save is the guard's.
func TestSaveCompaction_FakeGuards(t *testing.T) {
	t.Parallel()
	conv := uuid.Must(uuid.NewV7()).String()
	for name, tc := range map[string]struct {
		conversationID string
		c              Compaction
	}{
		"a malformed conversation id": {"not-a-uuid", Compaction{Summary: "s", CoversThroughSeq: 1}},
		"no turn covered":             {conv, Compaction{Summary: "s", CoversThroughSeq: 0}},
		"a negative watermark":        {conv, Compaction{Summary: "s", CoversThroughSeq: -1}},
		"no summary":                  {conv, Compaction{Summary: "", CoversThroughSeq: 3}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := fakeStore(t, &fakeDBTX{}).SaveCompaction(context.Background(), tc.conversationID, "", tc.c); err == nil {
				t.Fatal("SaveCompaction succeeded, want the guard's refusal")
			}
		})
	}
	if err := fakeStore(t, &fakeDBTX{}).SaveCompaction(context.Background(), conv, "", Compaction{Summary: "s", CoversThroughSeq: 1}); err != nil {
		t.Fatalf("SaveCompaction covering one turn: %v", err)
	}
}

func TestCompactionRow_FakeDatabaseErrors(t *testing.T) {
	t.Parallel()
	conv := uuid.Must(uuid.NewV7()).String()
	boom := errors.New("compaction row unreadable")
	failing := func() *fakeDBTX { return &fakeDBTX{queryRowVal: &fakeRow{scanErr: boom}} }

	if _, ok, err := fakeStore(t, failing()).LoadCompaction(context.Background(), conv, ""); !errors.Is(err, boom) || ok {
		t.Fatalf("LoadCompaction on a failing read = (%v, %v), want the error", ok, err)
	}
	if err := fakeStore(t, failing()).SaveCompaction(context.Background(), conv, "", Compaction{Summary: "s", CoversThroughSeq: 2}); !errors.Is(err, boom) {
		t.Fatalf("SaveCompaction on a failing write = %v, want the error", err)
	}
	if _, ok, err := fakeStore(t, &fakeDBTX{}).LoadCompaction(context.Background(), conv, ""); err != nil || ok {
		t.Fatalf("LoadCompaction with no row = (%v, %v), want none and no error", ok, err)
	}
}
