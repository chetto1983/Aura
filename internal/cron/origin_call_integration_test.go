//go:build db_integration

package cron

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A retried turn parks the same model call again (decision 30): the second create must find
// the first row rather than schedule the message twice.
func TestTheSameCallCreatesOneTask(t *testing.T) {
	s := New(migratedPool(t))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conversation := uuid.NewString()
	params := CreateTaskParams{
		Kind:                 KindMessage,
		Spec:                 ScheduleSpec{Kind: KindAt, RunAt: time.Now().Add(time.Hour), TZ: "UTC"},
		Payload:              []byte(`{}`),
		NextRunAt:            time.Now().Add(time.Hour),
		NotifyRoute:          string(RouteNone),
		Status:               "pending_approval",
		IdentityID:           "identity-" + conversation[:8],
		OriginConversationID: conversation,
		OriginToolCallID:     "call-1",
	}
	first, err := s.CreateTask(ctx, params)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, first.ID)
	})
	second, err := s.CreateTask(ctx, params)
	if err != nil || second.ID != first.ID {
		t.Fatalf("second create = %q, %v; want the first row %q", second.ID, err, first.ID)
	}
}
