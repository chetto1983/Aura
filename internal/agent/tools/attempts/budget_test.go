package attempts_test

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// seedBudgetRun inserts a minimal runs row so FK constraints are satisfied.
func seedBudgetRun(t *testing.T, runID string) *attempts.SQLiteRepo {
	t.Helper()
	db := openMigratedDB(t)
	seedRun(t, db, runID)
	return attempts.NewSQLiteRepo(db)
}

// recordOutcome inserts a synthetic tool attempt with the given outcome class.
func recordOutcome(t *testing.T, repo *attempts.SQLiteRepo, runID, toolName, class string, offset time.Duration) {
	t.Helper()
	obs := makeObs(runID, toolName, class, offset)
	if err := repo.Record(context.Background(), obs); err != nil {
		t.Fatalf("recordOutcome: %v", err)
	}
}

// --- CheckBudget unit tests ---

// TestCheckBudget_RecoverableLimit2_FirstAllowed confirms that with 0 prior
// recoverable failures and budget=2 the call is allowed.
func TestCheckBudget_RecoverableLimit2_FirstAllowed(t *testing.T) {
	const runID = "run-budget-recoverable-first"
	repo := seedBudgetRun(t, runID)

	allowed, reason := attempts.CheckBudget(context.Background(), repo, runID, "web_fetch", tools.OutcomeRecoverable, 2)
	if !allowed {
		t.Fatalf("expected allowed with 0 prior failures, got reason=%q", reason)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

// TestCheckBudget_RecoverableLimit2_SecondAllowed confirms that with 1 prior
// recoverable failure and budget=2 the call is still allowed.
func TestCheckBudget_RecoverableLimit2_SecondAllowed(t *testing.T) {
	const runID = "run-budget-recoverable-second"
	repo := seedBudgetRun(t, runID)

	recordOutcome(t, repo, runID, "web_fetch", "io", 0)

	allowed, _ := attempts.CheckBudget(context.Background(), repo, runID, "web_fetch", tools.OutcomeRecoverable, 2)
	if !allowed {
		t.Fatal("expected allowed with 1 prior failure and budget=2")
	}
}

// TestCheckBudget_RecoverableLimit2_ThirdRefused verifies the 3rd dispatch is
// refused when budget=2 and there are already 2 prior recoverable failures.
func TestCheckBudget_RecoverableLimit2_ThirdRefused(t *testing.T) {
	const runID = "run-budget-recoverable-third"
	repo := seedBudgetRun(t, runID)

	// Insert 2 recoverable observations (io class → OutcomeRecoverable).
	recordOutcome(t, repo, runID, "web_fetch", "io", 0)
	recordOutcome(t, repo, runID, "web_fetch", "io", time.Millisecond)

	allowed, reason := attempts.CheckBudget(context.Background(), repo, runID, "web_fetch", tools.OutcomeRecoverable, 2)
	if allowed {
		t.Fatal("expected 3rd dispatch refused with budget=2 and 2 prior recoverable failures")
	}
	if reason != "retry_budget_exhausted_for_class:recoverable" {
		t.Errorf("reason = %q, want 'retry_budget_exhausted_for_class:recoverable'", reason)
	}
}

// TestCheckBudget_Blocked_Budget0_FirstAllowed confirms that with no prior
// blocked failures and budget=0, the call is allowed (budget=0 ≡ limit=1).
func TestCheckBudget_Blocked_Budget0_FirstAllowed(t *testing.T) {
	const runID = "run-budget-blocked-first"
	repo := seedBudgetRun(t, runID)

	allowed, _ := attempts.CheckBudget(context.Background(), repo, runID, "tool_x", tools.OutcomeBlocked, 0)
	if !allowed {
		t.Fatal("expected first dispatch allowed with budget=0 and no prior failures")
	}
}

// TestCheckBudget_Blocked_Budget0_RefusedAfterOneFailure verifies that after
// 1 blocked failure the next dispatch is refused immediately (budget=0 → limit=1).
func TestCheckBudget_Blocked_Budget0_RefusedAfterOneFailure(t *testing.T) {
	const runID = "run-budget-blocked-refused"
	repo := seedBudgetRun(t, runID)

	// Pre-insert 1 blocked observation (permission class → OutcomeBlocked).
	recordOutcome(t, repo, runID, "tool_x", "permission", 0)

	allowed, reason := attempts.CheckBudget(context.Background(), repo, runID, "tool_x", tools.OutcomeBlocked, 0)
	if allowed {
		t.Fatal("expected refused immediately with budget=0 and 1 prior blocked failure")
	}
	if reason != "retry_budget_exhausted_for_class:blocked" {
		t.Errorf("reason = %q, want 'retry_budget_exhausted_for_class:blocked'", reason)
	}
}

// TestCheckBudget_ReasonFormat verifies the exact reason string format.
func TestCheckBudget_ReasonFormat(t *testing.T) {
	const runID = "run-budget-reason-fmt"
	repo := seedBudgetRun(t, runID)

	recordOutcome(t, repo, runID, "web_fetch", "io", 0)
	recordOutcome(t, repo, runID, "web_fetch", "io", time.Millisecond)

	_, reason := attempts.CheckBudget(context.Background(), repo, runID, "web_fetch", tools.OutcomeRecoverable, 2)
	want := "retry_budget_exhausted_for_class:recoverable"
	if reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
}

// TestCheckBudget_SuccessWithPriorRecoverable_AllowedBeforeBudgetHit verifies
// that a call is allowed when fewer prior failures exist than the budget, even
// if some prior calls were recoverable. The success row is expected to persist
// (no refusal row is produced by CheckBudget when it returns allowed=true).
func TestCheckBudget_SuccessWithPriorRecoverable_AllowedBeforeBudgetHit(t *testing.T) {
	const runID = "run-budget-success-before-limit"
	repo := seedBudgetRun(t, runID)

	// Insert 1 recoverable row — budget=2 allows one more dispatch.
	recordOutcome(t, repo, runID, "web_fetch", "io", 0)

	allowed, reason := attempts.CheckBudget(context.Background(), repo, runID, "web_fetch", tools.OutcomeRecoverable, 2)
	if !allowed {
		t.Fatalf("expected allowed with 1 prior failure and budget=2, got reason=%q", reason)
	}

	// Record the success (simulates the dispatch that returned ok).
	recordOutcome(t, repo, runID, "web_fetch", "", 2*time.Millisecond) // "" → OutcomeOK

	// Verify: success row exists in DB, no refusal rows.
	count, err := repo.CountOutcome(context.Background(), runID, "web_fetch", tools.OutcomeOK)
	if err != nil {
		t.Fatalf("CountOutcome(ok): %v", err)
	}
	if count != 1 {
		t.Errorf("ok count = %d, want 1 (success persisted)", count)
	}

	// No refusal rows: a refusal would appear as OutcomeBlocked with a reason.
	blockedCount, err := repo.CountOutcome(context.Background(), runID, "web_fetch", tools.OutcomeBlocked)
	if err != nil {
		t.Fatalf("CountOutcome(blocked): %v", err)
	}
	if blockedCount != 0 {
		t.Errorf("blocked count = %d, want 0 (no refusal logged)", blockedCount)
	}
}

// TestCheckBudget_FailOpenOnRepoError verifies that a repo error causes
// CheckBudget to return (true, "") — fail open so a storage hiccup never
// silently drops a legitimately needed tool call.
func TestCheckBudget_FailOpenOnRepoError(t *testing.T) {
	repo := attempts.NewSQLiteRepo(nil) // nil db → ErrRepoUnavailable

	allowed, reason := attempts.CheckBudget(context.Background(), repo, "run", "web_fetch", tools.OutcomeRecoverable, 2)
	if !allowed {
		t.Fatalf("expected fail-open (allowed=true) on repo error, got reason=%q", reason)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty on fail-open", reason)
	}
}
