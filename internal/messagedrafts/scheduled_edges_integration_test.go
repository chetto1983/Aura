//go:build db_integration

package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/db"
)

// Every scheduled-draft read and transition answers ErrUnavailable for an id it cannot use,
// before it reaches a row, so the API cannot tell a foreign draft from a missing one.
func TestScheduledDraftCallsRefuseUnusableIDs(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	ctx := context.Background()
	missing := uuid.NewString()
	for name, call := range map[string]func() error{
		"approve a malformed id":    func() error { _, err := store.Approve(ctx, owner, "not-a-uuid", nil); return err },
		"approve a missing draft":   func() error { _, err := store.Approve(ctx, owner, missing, nil); return err },
		"claim a malformed id":      func() error { _, err := store.ClaimScheduled(ctx, owner, "not-a-uuid"); return err },
		"claim a missing draft":     func() error { _, err := store.ClaimScheduled(ctx, owner, missing); return err },
		"expire a malformed id":     func() error { _, err := store.ExpireScheduled(ctx, owner, "not-a-uuid"); return err },
		"read a malformed owner":    func() error { _, err := store.ForTask(ctx, "not-a-uuid", missing); return err },
		"read a malformed task":     func() error { _, err := store.ForTask(ctx, owner, "not-a-uuid"); return err },
		"read a task with no draft": func() error { _, err := store.ForTask(ctx, owner, missing); return err },
		"decline a malformed task":  func() error { return store.DeclineForTask(ctx, owner, "not-a-uuid") },
		"decline a malformed owner": func() error { return store.DeclineScheduledForOwner(ctx, "not-a-uuid") },
	} {
		if err := call(); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s = %v, want ErrUnavailable", name, err)
		}
	}
}

// Approve takes only a pending scheduled draft, and only edits the review allows.
func TestApproveTakesOnlyAPendingDraftAndAllowedEdits(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	ctx := context.Background()
	draft, err := store.Create(ctx, scheduledInput(owner, "call-approve-edges", scheduledTask(t, pool, owner), time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	edit := json.RawMessage(`{"quoted_content":"x"}`)
	_, want := MergeApprovedArgs(draft.Target, draft.OriginalArgs, edit)
	if _, err := store.Approve(ctx, owner, draft.ID, edit); want == nil || err == nil || err.Error() != want.Error() {
		t.Fatalf("a non-editable field = %v, want the merge refusal %v", err, want)
	}
	if _, err := store.Decline(ctx, owner, draft.ID); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if _, err := store.Approve(ctx, owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("approve a declined draft = %v, want ErrUnavailable", err)
	}
	due, err := store.Create(ctx, scheduledInput(owner, "call-approve-due", scheduledTask(t, pool, owner), time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	dueDraft(t, pool, owner, due.ID)
	if _, err := store.Approve(ctx, owner, due.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("approve a draft past its send time = %v, want ErrUnavailable", err)
	}
}

func TestApprovedForTaskAndExpireForTaskReadTheDraftsState(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	ctx := context.Background()
	create := func(call string) (string, Draft) {
		task := scheduledTask(t, pool, owner)
		d, err := store.Create(ctx, scheduledInput(owner, call, task, time.Now().Add(time.Hour)))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return task, d
	}
	approvedTask, approved := create("call-state-approved")
	if _, err := store.Approve(ctx, owner, approved.ID, nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	pendingTask, pending := create("call-state-pending")
	for task, want := range map[string]bool{approvedTask: true, pendingTask: false, uuid.NewString(): false} {
		if got, err := store.ApprovedForTask(ctx, owner, task); err != nil || got != want {
			t.Errorf("ApprovedForTask(%s) = %v, %v; want %v", task, got, err, want)
		}
	}
	if err := store.ExpireForTask(ctx, owner, pendingTask); err != nil {
		t.Fatalf("expire a pending draft not yet due: %v", err)
	}
	if got, _ := store.Get(ctx, owner, pending.ID); got.Status != StatusPending {
		t.Fatalf("a draft not yet due was expired: %s", got.Status)
	}
	dueDraft(t, pool, owner, pending.ID)
	dueDraft(t, pool, owner, approved.ID)
	// Past its expiry too: ExpireScheduled alone would expire it, ExpireForTask must not.
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE aura.message_drafts SET expires_at = now() - interval '1 second' WHERE id = $1`, approved.ID)
		return err
	}); err != nil {
		t.Fatalf("lapse the approved draft: %v", err)
	}
	for _, task := range []string{pendingTask, approvedTask, uuid.NewString()} {
		if err := store.ExpireForTask(ctx, owner, task); err != nil {
			t.Fatalf("ExpireForTask(%s) = %v", task, err)
		}
	}
	if got, _ := store.Get(ctx, owner, pending.ID); got.Status != StatusExpired {
		t.Fatalf("a due pending draft = %s, want expired", got.Status)
	}
	if got, _ := store.Get(ctx, owner, approved.ID); got.Status != StatusApproved {
		t.Fatalf("an approved draft = %s, want approved", got.Status)
	}
	if _, err := store.ApprovedForTask(ctx, "not-a-uuid", approvedTask); !errors.Is(err, ErrUnavailable) && err != nil {
		t.Fatalf("a malformed owner = %v", err)
	}
}
