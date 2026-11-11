//go:build db_integration

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// scheduledMessageFixture is one identity with message tasks and their scheduled drafts.
type scheduledMessageFixture struct {
	pool   *pgxpool.Pool
	owner  string
	drafts *messagedrafts.Store
}

func newScheduledMessageFixture(t *testing.T, pool *pgxpool.Pool) scheduledMessageFixture {
	t.Helper()
	owner := uuid.NewString()
	if _, err := pool.Exec(context.Background(), `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, owner, "msg-"+owner); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE identity_id = $1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, owner)
	})
	return scheduledMessageFixture{pool: pool, owner: owner, drafts: messagedrafts.NewStore(pool)}
}

// message creates a message task in status with its draft, approved when approve is set.
func (f scheduledMessageFixture) message(t *testing.T, status string, approve bool) (taskID, draftID string) {
	t.Helper()
	ctx := context.Background()
	sendAt := time.Now().Add(time.Hour)
	task, err := cron.New(f.pool).CreateTask(ctx, cron.CreateTaskParams{
		Kind: cron.KindMessage, Spec: cron.ScheduleSpec{Kind: cron.KindAt, RunAt: sendAt, TZ: "UTC"},
		Payload: []byte(`{}`), NextRunAt: sendAt, NotifyRoute: string(cron.RouteNone),
		Status: status, IdentityID: f.owner,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	draft, err := f.drafts.Create(ctx, messagedrafts.DraftInput{
		IdentityID: f.owner, ConversationID: "conv-" + f.owner[:8], ToolCallID: "call-" + task.ID,
		Target:             messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"10000000000","message":"hello"}`),
		ExpiresAt:          sendAt.Add(tools.ScheduledMessageGrace), ScheduledTaskID: task.ID, SendAt: sendAt,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if approve {
		if _, err := f.drafts.Approve(ctx, f.owner, draft.ID, nil); err != nil {
			t.Fatalf("approve draft: %v", err)
		}
	}
	return task.ID, draft.ID
}

func (f scheduledMessageFixture) state(t *testing.T, taskID, draftID string) (string, messagedrafts.Status) {
	t.Helper()
	draft, err := f.drafts.Get(context.Background(), f.owner, draftID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	return schedulerTaskStatus(t, f.pool, taskID), draft.Status
}

// closedPool is a pool every call fails on: the injected failure of a store.
func closedPool(t *testing.T, live *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.NewWithConfig(context.Background(), live.Config())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	pool.Close()
	return pool
}

func TestCancellingAMessageTaskDeclinesItsDraft(t *testing.T) {
	f := newScheduledMessageFixture(t, bootstrapMigratedPool(t))
	taskID, draftID := f.message(t, "active", true)
	store := newCronTaskStore(f.pool, nil)
	if err := store.CancelScheduledTask(identityctx.WithIdentityID(context.Background(), f.owner), taskID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if task, draft := f.state(t, taskID, draftID); task != "cancelled" || draft != messagedrafts.StatusDeclined {
		t.Fatalf("task %s, draft %s; want cancelled and declined", task, draft)
	}
}

// Decision 15: a failed decline cancels nothing, and the retry declines, then cancels.
func TestCancelFailedDeclineLeavesTheTaskUncancelled(t *testing.T) {
	f := newScheduledMessageFixture(t, bootstrapMigratedPool(t))
	taskID, draftID := f.message(t, "active", true)
	ctx := identityctx.WithIdentityID(context.Background(), f.owner)
	broken := newCronTaskStore(f.pool, nil)
	broken.drafts = messagedrafts.NewStore(closedPool(t, f.pool))
	if err := broken.CancelScheduledTask(ctx, taskID); err == nil {
		t.Fatal("a failed decline was reported as a cancel")
	}
	if task, draft := f.state(t, taskID, draftID); task != "active" || draft != messagedrafts.StatusApproved {
		t.Fatalf("after the failure: task %s, draft %s; want active and approved", task, draft)
	}
	if err := newCronTaskStore(f.pool, nil).CancelScheduledTask(ctx, taskID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if task, draft := f.state(t, taskID, draftID); task != "cancelled" || draft != messagedrafts.StatusDeclined {
		t.Fatalf("after the retry: task %s, draft %s", task, draft)
	}
}

func TestDeactivationCancelsMessageTasksAndDeclinesTheirDrafts(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	f := newScheduledMessageFixture(t, pool)
	other := newScheduledMessageFixture(t, pool)
	reminder := insertOwnedTask(t, pool, f.owner, "reminder")
	pendingTask, pendingDraft := f.message(t, "pending_approval", false)
	approvedTask, approvedDraft := f.message(t, "active", true)
	otherTask, otherDraft := other.message(t, "active", true)

	adapter := scheduledTaskAdapter{store: cron.New(pool), drafts: messagedrafts.NewStore(pool)}
	if err := adapter.TerminateJobs(context.Background(), f.owner); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if status := schedulerTaskStatus(t, pool, reminder); status != "paused" {
		t.Fatalf("reminder is %s, want paused", status)
	}
	for _, m := range [][2]string{{pendingTask, pendingDraft}, {approvedTask, approvedDraft}} {
		if task, draft := f.state(t, m[0], m[1]); task != "cancelled" || draft != messagedrafts.StatusDeclined {
			t.Fatalf("message: task %s, draft %s; want cancelled and declined", task, draft)
		}
	}
	if task, draft := other.state(t, otherTask, otherDraft); task != "active" || draft != messagedrafts.StatusApproved {
		t.Fatalf("another identity's message: task %s, draft %s", task, draft)
	}
}

// Decision 37: the decline goes first and by owner, so either failure leaves a retry that
// ends with the draft declined and the task cancelled.
func TestDeactivationDeclineIsRetrySafe(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	live := scheduledTaskAdapter{store: cron.New(pool), drafts: messagedrafts.NewStore(pool)}
	for name, broken := range map[string]scheduledTaskAdapter{
		"the decline fails":         {store: cron.New(pool), drafts: messagedrafts.NewStore(closedPool(t, pool))},
		"the cancel fails after it": {store: cron.New(closedPool(t, pool)), drafts: messagedrafts.NewStore(pool)},
	} {
		t.Run(name, func(t *testing.T) {
			f := newScheduledMessageFixture(t, pool)
			taskID, draftID := f.message(t, "active", true)
			if err := broken.TerminateJobs(context.Background(), f.owner); err == nil {
				t.Fatal("the failure was not reported")
			}
			if task, draft := f.state(t, taskID, draftID); task != "active" || (draft != messagedrafts.StatusApproved && draft != messagedrafts.StatusDeclined) {
				t.Fatalf("after the failure: task %s, draft %s", task, draft)
			}
			if err := live.TerminateJobs(context.Background(), f.owner); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if task, draft := f.state(t, taskID, draftID); task != "cancelled" || draft != messagedrafts.StatusDeclined {
				t.Fatalf("after the retry: task %s, draft %s", task, draft)
			}
		})
	}
}
