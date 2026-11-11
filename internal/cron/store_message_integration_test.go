//go:build db_integration

package cron

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOnlyItsOwnersMessageTaskIsActivatedByItsReview(t *testing.T) {
	s := New(migratedPool(t))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	owner, other := uuid.NewString(), uuid.NewString()
	create := func(kind TaskKind, identity string) string {
		at := time.Now().Add(time.Hour)
		task, err := s.CreateTask(ctx, CreateTaskParams{
			Kind: kind, Spec: ScheduleSpec{Kind: KindAt, RunAt: at, TZ: "UTC"}, Payload: []byte(`{"text":"x"}`),
			NextRunAt: at, NotifyRoute: string(RouteNone), Status: "pending_approval", IdentityID: identity,
		})
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, task.ID) })
		return task.ID
	}
	if err := s.ActivateReviewedMessage(ctx, owner, create(KindReminder, owner)); err == nil {
		t.Fatal("a reminder was activated as a reviewed message")
	}
	if err := s.ActivateReviewedMessage(ctx, owner, create(KindMessage, other)); err == nil {
		t.Fatal("another identity's message was activated")
	}
	mine := create(KindMessage, owner)
	if err := s.ActivateReviewedMessage(ctx, owner, mine); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if got, err := s.GetTask(ctx, mine); err != nil || got.Status != "active" {
		t.Fatalf("after activate: %v, %v", got.Status, err)
	}
	if err := s.ActivateReviewedMessage(ctx, owner, mine); err != nil {
		t.Fatalf("a second activation = %v, want nil", err)
	}
	if err := s.CancelReviewedMessage(ctx, owner, mine); err == nil {
		t.Fatal("an active message was cancelled as declined")
	}
}
