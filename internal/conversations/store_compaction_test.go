//go:build db_integration

package conversations

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// claimFixture returns a fresh, immediately-pending automatic claim for conv/branch.
func claimFixture(conv, branch, op, owner string, lease time.Time) CompactionClaimParams {
	return CompactionClaimParams{OperationID: op, ConversationID: conv, BranchID: branch, IdempotencyKey: op, OwnerID: owner, CapturedWatermark: 1, Priority: ClaimPriorityAutomatic, LeaseUntil: lease}
}

// claimAndFinalizeCheckpoint drives one full claim -> finalize cycle in-process (not via
// the cmd/compaction-test-worker subprocess used by the multiprocess tests, which does
// NOT contribute to this package's own coverage profile) and returns the operation and
// resulting checkpoint IDs for Preview/Restore/Quarantine fixtures.
func claimAndFinalizeCheckpoint(t *testing.T, s *Store, conv, branch string) (op, checkpointID string) {
	t.Helper()
	op = uuid.NewString()
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, branch, op, "owner", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1, Content: "x"}})
	p := finalizeFixture(op, "owner", m)
	cp, err := s.FinalizeCompaction(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	return op, cp
}

func TestClaimCompactionFirstClaimIsPending(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	claim, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "owner", time.Now().Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if claim.State != ClaimPending || claim.OperationID != op || claim.OwnerID != "owner" {
		t.Fatalf("claim=%+v", claim)
	}
}

func TestClaimCompactionMalformedIdentifiers(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	t.Run("conversation id", func(t *testing.T) {
		p := claimFixture("not-a-uuid", "main", uuid.NewString(), "owner", time.Now().Add(time.Minute))
		if _, err := s.ClaimCompaction(t.Context(), p); err == nil {
			t.Fatal("expected parse error")
		}
	})
	t.Run("operation id", func(t *testing.T) {
		p := claimFixture(conv, "main", "not-a-uuid", "owner", time.Now().Add(time.Minute))
		if _, err := s.ClaimCompaction(t.Context(), p); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestClaimCompactionSupersededOnStaleBaseGeneration(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	claimAndFinalizeCheckpoint(t, s, conv, "main") // advances generation to 1
	stale := claimFixture(conv, "main", uuid.NewString(), "owner2", time.Now().Add(time.Minute))
	stale.BaseGeneration = 0
	if _, err := s.ClaimCompaction(t.Context(), stale); !errors.Is(err, ErrCompactionSuperseded) {
		t.Fatalf("stale generation err=%v", err)
	}
}

func TestClaimCompactionDuplicateIdempotencyKeepsOriginalOwner(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	a, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "first", time.Now().Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "second", time.Now().Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if a.OperationID != b.OperationID || b.OwnerID != "first" {
		t.Fatalf("duplicate claim a=%+v b=%+v", a, b)
	}
}

func TestClaimCompactionRenewsLeaseWhenSameOperationExpired(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "dead", time.Now().Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	renewed, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "takeover", time.Now().Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if renewed.OwnerID != "takeover" {
		t.Fatalf("renewed=%+v", renewed)
	}
}

func TestClaimCompactionManualPreemptsUnstartedAutomatic(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", uuid.NewString(), "auto", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	manual := claimFixture(conv, "main", uuid.NewString(), "manual", time.Now().Add(time.Minute))
	manual.Priority = ClaimPriorityManual
	got, err := s.ClaimCompaction(t.Context(), manual)
	if err != nil || got.OwnerID != "manual" {
		t.Fatalf("manual preemption got=%+v err=%v", got, err)
	}
}

func TestClaimCompactionInProgressWhenAnotherAutomaticIsPending(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", uuid.NewString(), "first", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	_, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", uuid.NewString(), "second", time.Now().Add(time.Minute)))
	if !errors.Is(err, ErrCompactionInProgress) {
		t.Fatalf("in-progress err=%v", err)
	}
}

func TestClaimCompactionManualDoesNotPreemptStartedInference(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "auto", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompactionInferenceStarted(t.Context(), op, "auto"); err != nil {
		t.Fatal(err)
	}
	manual := claimFixture(conv, "main", uuid.NewString(), "manual", time.Now().Add(time.Minute))
	manual.Priority = ClaimPriorityManual
	if _, err := s.ClaimCompaction(t.Context(), manual); !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("err=%v", err)
	}
}

func TestMarkCompactionInferenceStartedSucceeds(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "owner", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompactionInferenceStarted(t.Context(), op, "owner"); err != nil {
		t.Fatal(err)
	}
}

func TestMarkCompactionInferenceStartedMalformedOperationID(t *testing.T) {
	pool, _, _ := compactionDB(t)
	s := New(pool, Config{})
	if err := s.MarkCompactionInferenceStarted(t.Context(), "not-a-uuid", "owner"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMarkCompactionInferenceStartedFailsForWrongOwner(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "owner", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompactionInferenceStarted(t.Context(), op, "someone-else"); !errors.Is(err, ErrCompactionSuperseded) {
		t.Fatalf("wrong owner err=%v", err)
	}
}

func TestMarkCompactionInferenceStartedFailsForExpiredLease(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	op := uuid.NewString()
	if _, err := s.ClaimCompaction(t.Context(), claimFixture(conv, "main", op, "owner", time.Now().Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompactionInferenceStarted(t.Context(), op, "owner"); !errors.Is(err, ErrCompactionSuperseded) {
		t.Fatalf("expired lease err=%v", err)
	}
}

func TestPreviewCompactionReturnsActiveCheckpoint(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	_, cp := claimAndFinalizeCheckpoint(t, s, conv, "main")
	preview, err := s.PreviewCompaction(t.Context(), conv, "main")
	if err != nil {
		t.Fatal(err)
	}
	if preview.CheckpointID != cp || preview.PriorCheckpointID != "" || !strings.Contains(preview.Summary, "ok") {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestPreviewCompactionNoActivePointerReturnsEmpty(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	preview, err := s.PreviewCompaction(t.Context(), conv, "never-claimed")
	if err != nil {
		t.Fatal(err)
	}
	if preview != (CompactionPreview{}) {
		t.Fatalf("expected empty preview, got %+v", preview)
	}
}

func TestPreviewCompactionMalformedConversationID(t *testing.T) {
	pool, _, _ := compactionDB(t)
	s := New(pool, Config{})
	if _, err := s.PreviewCompaction(t.Context(), "not-a-uuid", "main"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestQuarantineCompactionCheckpointRecordsRow(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	_, cp := claimAndFinalizeCheckpoint(t, s, conv, "main")
	if err := s.QuarantineCompactionCheckpoint(t.Context(), cp, "structured_summary", "digest_mismatch", "deadbeef"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM aura.compaction_quarantine WHERE checkpoint_id=$1`, cp).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("quarantine rows=%d", n)
	}
}

func TestQuarantineCompactionCheckpointMalformedID(t *testing.T) {
	pool, _, _ := compactionDB(t)
	s := New(pool, Config{})
	if err := s.QuarantineCompactionCheckpoint(t.Context(), "not-a-uuid", "structured_summary", "digest_mismatch", ""); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRestoreCompactionMalformedIdentifiers(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	cases := []struct {
		name, conv, checkpoint, op string
	}{
		{"conversation id", "not-a-uuid", uuid.NewString(), uuid.NewString()},
		{"checkpoint id", conv, "not-a-uuid", uuid.NewString()},
		{"operation id", conv, uuid.NewString(), "not-a-uuid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.RestoreCompaction(t.Context(), tc.conv, "main", tc.checkpoint, tc.op, "actor"); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestRestoreCompactionNoActivePointer(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	err := s.RestoreCompaction(t.Context(), conv, "never-claimed", uuid.NewString(), uuid.NewString(), "actor")
	if err == nil || !strings.Contains(err.Error(), "lock restore pointer") {
		t.Fatalf("err=%v", err)
	}
}

func TestRestoreCompactionTargetCheckpointNotFound(t *testing.T) {
	pool, _, conv := compactionDB(t)
	s := New(pool, Config{})
	claimAndFinalizeCheckpoint(t, s, conv, "main")
	err := s.RestoreCompaction(t.Context(), conv, "main", uuid.NewString(), uuid.NewString(), "actor")
	if err == nil || !strings.Contains(err.Error(), "load restore target") {
		t.Fatalf("err=%v", err)
	}
}
