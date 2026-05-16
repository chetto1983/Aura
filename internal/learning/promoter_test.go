package learning_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/testutil"
)

// fakeRepo implements attempts.Repo with a predetermined AggregateForPromotion result.
type fakeRepo struct {
	candidates []attempts.LessonCandidate
}

func (f *fakeRepo) Record(_ context.Context, _ tools.ToolObservation) error { return nil }
func (f *fakeRepo) Recent(_ context.Context, _, _ string, _ int) ([]attempts.ToolAttempt, error) {
	return nil, nil
}
func (f *fakeRepo) CountOutcome(_ context.Context, _, _ string, _ tools.Outcome) (int, error) {
	return 0, nil
}
func (f *fakeRepo) AggregateForPromotion(_ context.Context, _, _ int) ([]attempts.LessonCandidate, error) {
	return f.candidates, nil
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testutil.OpenTestDB(t, migrations.Run)
}

func makeCandidate(toolName, errorClass string) attempts.LessonCandidate {
	return attempts.LessonCandidate{
		ToolName:         toolName,
		ToolKind:         "native",
		ErrorClass:       errorClass,
		Reason:           "timeout",
		AttemptCount:     5,
		FirstSeen:        time.Now().Add(-24 * time.Hour),
		LastSeen:         time.Now(),
		SampleAttemptIDs: []string{"atm_aaa", "atm_bbb"},
		SignatureHash:    attempts.LessonSignatureHash(toolName, "native", errorClass, "timeout"),
	}
}

func TestPromoteLessons_EmptyRepo(t *testing.T) {
	db := openTestDB(t)
	store := learning.NewSQLProposalStore(db)
	repo := &fakeRepo{}

	promoted, skipped, err := learning.PromoteLessons(context.Background(), repo, store, 3, 7)
	if err != nil {
		t.Fatalf("PromoteLessons: %v", err)
	}
	if promoted != 0 || skipped != 0 {
		t.Errorf("empty repo: got promoted=%d skipped=%d, want 0 0", promoted, skipped)
	}
}

func TestPromoteLessons_SingleCandidate(t *testing.T) {
	db := openTestDB(t)
	store := learning.NewSQLProposalStore(db)
	repo := &fakeRepo{candidates: []attempts.LessonCandidate{
		makeCandidate("web_fetch", "io"),
	}}

	promoted, skipped, err := learning.PromoteLessons(context.Background(), repo, store, 3, 7)
	if err != nil {
		t.Fatalf("PromoteLessons: %v", err)
	}
	if promoted != 1 || skipped != 0 {
		t.Errorf("single candidate: got promoted=%d skipped=%d, want 1 0", promoted, skipped)
	}
}

func TestPromoteLessons_Idempotency(t *testing.T) {
	db := openTestDB(t)
	store := learning.NewSQLProposalStore(db)
	repo := &fakeRepo{candidates: []attempts.LessonCandidate{
		makeCandidate("web_fetch", "io"),
	}}
	ctx := context.Background()

	// First run: should create one proposal.
	promoted, skipped, err := learning.PromoteLessons(ctx, repo, store, 3, 7)
	if err != nil {
		t.Fatalf("PromoteLessons run 1: %v", err)
	}
	if promoted != 1 || skipped != 0 {
		t.Errorf("run 1: got promoted=%d skipped=%d, want 1 0", promoted, skipped)
	}

	// Re-run with identical data: same signature_hash → skip.
	promoted, skipped, err = learning.PromoteLessons(ctx, repo, store, 3, 7)
	if err != nil {
		t.Fatalf("PromoteLessons run 2: %v", err)
	}
	if promoted != 0 || skipped != 1 {
		t.Errorf("run 2 (idempotency): got promoted=%d skipped=%d, want 0 1", promoted, skipped)
	}
}

func TestPromoteLessons_MultiCandidate(t *testing.T) {
	db := openTestDB(t)
	store := learning.NewSQLProposalStore(db)
	repo := &fakeRepo{candidates: []attempts.LessonCandidate{
		makeCandidate("web_fetch", "io"),
		makeCandidate("web_search", "not_found"),
		makeCandidate("store_source", "permission"),
	}}

	promoted, skipped, err := learning.PromoteLessons(context.Background(), repo, store, 3, 7)
	if err != nil {
		t.Fatalf("PromoteLessons: %v", err)
	}
	if promoted != 3 || skipped != 0 {
		t.Errorf("multi-candidate: got promoted=%d skipped=%d, want 3 0", promoted, skipped)
	}
}
