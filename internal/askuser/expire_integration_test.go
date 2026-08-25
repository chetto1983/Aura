//go:build db_integration

package askuser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5"
)

func TestListExpiredPendingApprovalsFiltersAgeKindAndResolution(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	convID := newConversation(t, pool)
	ctx := ownerCtx()

	oldApproval := insertPause(t, store, convID, "approval", "old", 0)
	freshApproval := insertPause(t, store, convID, "approval", "fresh", 0)
	oldClarification := insertPause(t, store, convID, "clarification", "old clarification", 0)
	resolvedApproval := insertPause(t, store, convID, "approval", "resolved", 0)
	cutoff := time.Now().UTC().Add(-time.Hour)

	if err := db.WithIdentityTxRaw(ctx, pool, localID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			"UPDATE aura.paused_states SET created_at=$1 WHERE token = ANY($2::uuid[])",
			cutoff.Add(-time.Minute), []string{oldApproval, oldClarification, resolvedApproval}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`UPDATE aura.paused_states SET resumed_at=now(), resumed_answer='{"action":"decline","content":"done"}'::jsonb WHERE token=$1`,
			resolvedApproval)
		return err
	}); err != nil {
		t.Fatalf("seed expiry states: %v", err)
	}

	foreignCtx := identityctx.WithIdentityID(context.Background(), "22222222-2222-2222-2222-222222222222")
	foreignDue, err := store.ListExpiredPendingApprovals(foreignCtx, cutoff, 10)
	if err != nil {
		t.Fatalf("ListExpiredPendingApprovals(foreign): %v", err)
	}
	if len(foreignDue) != 0 {
		t.Fatalf("foreign identity saw %d expired approvals, want 0", len(foreignDue))
	}

	due, err := store.ListExpiredPendingApprovals(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("ListExpiredPendingApprovals: %v", err)
	}
	if len(due) != 1 || due[0].Token != oldApproval {
		t.Fatalf("due = %#v, want only %s (fresh=%s clarification=%s resolved=%s)",
			due, oldApproval, freshApproval, oldClarification, resolvedApproval)
	}
	dueWithDefaultLimit, err := store.ListExpiredPendingApprovals(ctx, cutoff, 0)
	if err != nil || len(dueWithDefaultLimit) != 1 || dueWithDefaultLimit[0].Token != oldApproval {
		t.Fatalf("due with default limit = %#v, %v; want only %s", dueWithDefaultLimit, err, oldApproval)
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ListExpiredPendingApprovals(canceledCtx, cutoff, 10); err == nil {
		t.Fatal("canceled expiry query returned no error")
	}

	ownedPending, err := store.GetByTokenForIdentity(ctx, oldApproval, localID)
	if err != nil || ownedPending.Token != oldApproval {
		t.Fatalf("GetByTokenForIdentity(pending) = %#v, %v; want %s", ownedPending, err, oldApproval)
	}

	if err := store.MarkResumed(ctx, oldApproval, ResumeAnswer{Action: ActionExpired, Content: "approval expired before a decision was made"}); err != nil {
		t.Fatalf("mark expired: %v", err)
	}
	if _, err := store.GetByToken(ctx, oldApproval); !errors.Is(err, ErrPauseExpired) {
		t.Fatalf("GetByToken(expired) error = %v, want ErrPauseExpired", err)
	}
	if _, err := store.GetByTokenForIdentity(ctx, oldApproval, localID); !errors.Is(err, ErrPauseExpired) {
		t.Fatalf("GetByTokenForIdentity(expired) error = %v, want ErrPauseExpired", err)
	}
}

func TestValidateResumeAnswerAcceptsOnlyInternalExpiryAction(t *testing.T) {
	if err := ValidateResumeAnswer(ResumeAnswer{Action: ActionExpired, Content: "approval expired before a decision was made"}); err != nil {
		t.Fatalf("internal expiry answer: %v", err)
	}
	if err := ValidateResumeAnswer(ResumeAnswer{Action: ActionExpired}); err == nil {
		t.Fatal("empty internal expiry content was accepted")
	}
}
