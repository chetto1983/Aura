package attempts_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
)

// verifySignatureHash recomputes the expected hash inline so the test does not
// depend on the exported helper producing the wrong value.
func verifySignatureHash(t *testing.T, lc attempts.LessonCandidate) {
	t.Helper()
	h := sha256.Sum256([]byte(lc.ToolName + lc.ToolKind + lc.ErrorClass + lc.Reason))
	expected := fmt.Sprintf("%x", h[:8])
	if lc.SignatureHash != expected {
		t.Errorf("SignatureHash mismatch: got %q, want %q", lc.SignatureHash, expected)
	}
	// Also cross-check against the exported helper.
	if got := attempts.LessonSignatureHash(lc.ToolName, lc.ToolKind, lc.ErrorClass, lc.Reason); got != expected {
		t.Errorf("LessonSignatureHash helper: got %q, want %q", got, expected)
	}
}

func TestAggregateForPromotion_EmptyStore(t *testing.T) {
	db := openMigratedDB(t)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	got, err := repo.AggregateForPromotion(ctx, 3, 7)
	if err != nil {
		t.Fatalf("AggregateForPromotion: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty store: got %d candidates, want 0", len(got))
	}
}

func TestAggregateForPromotion_BelowThreshold(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_agg_below"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	// Insert 2 recoverable failures — below minOccurrences=3.
	for i := range 2 {
		obs := makeObs(runID, "web_search", "io", time.Duration(i)*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	got, err := repo.AggregateForPromotion(ctx, 3, 7)
	if err != nil {
		t.Fatalf("AggregateForPromotion: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("below threshold: got %d candidates, want 0", len(got))
	}
}

func TestAggregateForPromotion_AboveThreshold(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_agg_above"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	// Insert 4 recoverable failures for the same (tool, kind, class, reason).
	for i := range 4 {
		obs := makeObs(runID, "web_fetch", "io", time.Duration(i)*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	got, err := repo.AggregateForPromotion(ctx, 3, 7)
	if err != nil {
		t.Fatalf("AggregateForPromotion: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("above threshold: got %d candidates, want 1", len(got))
	}

	lc := got[0]
	if lc.AttemptCount < 3 {
		t.Errorf("AttemptCount: got %d, want >= 3", lc.AttemptCount)
	}
	if lc.ToolName != "web_fetch" {
		t.Errorf("ToolName: got %q, want %q", lc.ToolName, "web_fetch")
	}
	if lc.ErrorClass != "io" {
		t.Errorf("ErrorClass: got %q, want %q", lc.ErrorClass, "io")
	}
	verifySignatureHash(t, lc)
	if len(lc.SampleAttemptIDs) == 0 {
		t.Error("SampleAttemptIDs must not be empty")
	}
	if len(lc.SampleAttemptIDs) > 5 {
		t.Errorf("SampleAttemptIDs: got %d IDs, want <= 5", len(lc.SampleAttemptIDs))
	}
}

func TestAggregateForPromotion_MultiGroup(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_agg_multi"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	// Group A: web_fetch + io — 5 failures.
	for i := range 5 {
		obs := makeObs(runID, "web_fetch", "io", time.Duration(i)*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record web_fetch[%d]: %v", i, err)
		}
	}

	// Group B: web_search + not_found — 3 failures.
	for i := range 3 {
		obs := makeObs(runID, "web_search", "not_found", time.Duration(i)*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record web_search[%d]: %v", i, err)
		}
	}

	got, err := repo.AggregateForPromotion(ctx, 3, 7)
	if err != nil {
		t.Fatalf("AggregateForPromotion: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("multi-group: got %d candidates, want 2", len(got))
	}
	// Must be ordered by AttemptCount DESC.
	if got[0].AttemptCount <= got[1].AttemptCount {
		t.Errorf("not ordered by count DESC: [0]=%d [1]=%d", got[0].AttemptCount, got[1].AttemptCount)
	}
	if got[0].ToolName != "web_fetch" {
		t.Errorf("first candidate tool: got %q, want %q", got[0].ToolName, "web_fetch")
	}
	verifySignatureHash(t, got[0])
	verifySignatureHash(t, got[1])
}

func TestAggregateForPromotion_SinceDays(t *testing.T) {
	db := openMigratedDB(t)
	runID := "run_agg_since"
	seedRun(t, db, runID)
	repo := attempts.NewSQLiteRepo(db)
	ctx := context.Background()

	// Insert 4 failures with started_at = 100 days ago (outside 7-day window).
	for i := range 4 {
		obs := makeObs(runID, "web_fetch", "io",
			-100*24*time.Hour+time.Duration(i)*time.Millisecond)
		if err := repo.Record(ctx, obs); err != nil {
			t.Fatalf("Record old[%d]: %v", i, err)
		}
	}

	got, err := repo.AggregateForPromotion(ctx, 3, 7)
	if err != nil {
		t.Fatalf("AggregateForPromotion: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("sincedays filter: expected 0 candidates (old rows excluded), got %d", len(got))
	}
}
