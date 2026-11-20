//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/jackc/pgx/v5/pgxpool"
)

func scheduledPause(t *testing.T, x expireRig, taskID, question string) string {
	t.Helper()
	rc, err := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	token, err := x.r.MintApprovalPause(ownerCtx(), x.conv, question, rc, allResumeDecisions())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func sweepQuestion(id string) string {
	return fmt.Sprintf("Scheduled agent_job task %.8s needs your approval. Approve or reject it below.", id)
}

func requireTaskState(t *testing.T, x expireRig, taskID, status string) {
	t.Helper()
	got := taskStatus(t, x.pool, taskID)
	if got != status {
		t.Errorf("task status = %q, want %q", got, status)
	}
}

func taskStatus(t *testing.T, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var got string
	if err := pool.QueryRow(ownerCtx(), "SELECT status FROM aura.scheduler_tasks WHERE id=$1", taskID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSubmitScheduledApprovalCommitsTaskAndAnswer(t *testing.T) {
	for _, tc := range []struct{ action, status, content string }{
		{askuser.ActionAccept, "active", "now ACTIVE"},
		{askuser.ActionDecline, "cancelled", "has been cancelled"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			x := newExpireRig(t)
			at := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
			id := x.task(t, cron.KindAgentJob, at)
			question := sweepQuestion(id)
			if tc.action == askuser.ActionAccept {
				loc, err := time.LoadLocation("Europe/Rome")
				if err != nil {
					t.Fatal(err)
				}
				question = fmt.Sprintf("Approve scheduled agent_job task %.8s (once), first run %s (Europe/Rome)? It runs: g. It will not run until you approve.", id, at.In(loc).Format("02/01 15:04"))
			}
			token := scheduledPause(t, x, id, question)
			d, err := x.r.SubmitAnswer(ownerCtx(), token, ResponseInput{Action: tc.action})
			if err != nil || d.Outcome != OutcomeContinue || d.Remaining != 0 {
				t.Fatalf("submit = %+v, %v", d, err)
			}
			requireTaskState(t, x, id, tc.status)
			history, err := x.r.Conv.LoadHistory(ownerCtx(), x.conv)
			if err != nil || !historyHasToolContentMsgs(history, tc.content) || resumedAt(t, x.pool, token) == nil {
				t.Fatalf("missing durable %q answer, err %v", tc.content, err)
			}
		})
	}
}

func TestSubmitScheduledApprovalRejectedLeavesPauseAndHistory(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		kind                       cron.TaskKind
		action                     string
		status                     string
		update                     string
		expired, forged, missingID bool
	}{
		{name: "foreign conversation", update: "origin_conversation_id=gen_random_uuid()"},
		{name: "foreign owner", update: "identity_id=gen_random_uuid()::text"},
		{name: "already cancelled", status: "cancelled", update: "status='cancelled'"},
		{name: "forged acknowledgement of an active task", status: "active", update: "status='active'", forged: true},
		{name: "decline an active task", action: askuser.ActionDecline, status: "active", update: "status='active'"},
		{name: "expired", expired: true},
		{name: "message", kind: cron.KindMessage},
		{name: "missing id", missingID: true},
		{name: "forged accept", forged: true},
		{name: "forged decline", action: askuser.ActionDecline, forged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newExpireRig(t)
			at := time.Now().Add(time.Hour)
			if tc.expired {
				at = time.Now().Add(-time.Minute)
			}
			if tc.kind == "" {
				tc.kind = cron.KindAgentJob
			}
			if tc.action == "" {
				tc.action = askuser.ActionAccept
			}
			if tc.status == "" {
				tc.status = "pending_approval"
			}
			id := x.task(t, tc.kind, at)
			if tc.update != "" {
				if _, err := x.pool.Exec(ownerCtx(), "UPDATE aura.scheduler_tasks SET "+tc.update+" WHERE id=$1", id); err != nil {
					t.Fatal(err)
				}
			}
			taskRef := id
			if tc.missingID {
				taskRef = ""
			}
			question := fmt.Sprintf("Scheduled %s task %.8s needs your approval. Approve or reject it below.", tc.kind, id)
			if tc.forged {
				question = "Shall I show you the weather?"
			}
			token := scheduledPause(t, x, taskRef, question)
			if _, err := x.r.SubmitAnswer(ownerCtx(), token, ResponseInput{Action: tc.action, Content: "yes"}); !errors.Is(err, ErrResumeDecisionNotAllowed) {
				t.Errorf("ineligible task resolution = %v, want decision refused", err)
			}
			if resumedAt(t, x.pool, token) != nil || countPersistedToolTurns(t, x.pool, x.conv) != 0 {
				t.Error("failed resolution consumed the pause or recorded an answer")
			}
			requireTaskState(t, x, id, tc.status)
		})
	}
}

func TestSubmitScheduledApprovalBatchRollsBackThenRetries(t *testing.T) {
	x := newExpireRig(t)
	first := x.task(t, cron.KindAgentJob, time.Now().Add(time.Hour))
	second := x.task(t, cron.KindAgentJob, time.Now().Add(time.Hour))
	tokenA := scheduledPause(t, x, first, sweepQuestion(first))
	tokenB := scheduledPause(t, x, second, sweepQuestion(second))
	if _, err := x.pool.Exec(ownerCtx(), "UPDATE aura.scheduler_tasks SET next_run_at = now() WHERE id=$1", second); err != nil {
		t.Fatal(err)
	}
	answers := map[string]ResponseInput{tokenA: {Action: askuser.ActionDecline}, tokenB: {Action: askuser.ActionAccept}}
	if _, err := x.r.SubmitAnswers(ownerCtx(), answers); err == nil {
		t.Error("batch claimed a due one-shot")
	}
	if resumedAt(t, x.pool, tokenA) != nil || resumedAt(t, x.pool, tokenB) != nil || countPersistedToolTurns(t, x.pool, x.conv) != 0 {
		t.Error("failed batch left a durable answer")
	}
	requireTaskState(t, x, first, "pending_approval")
	requireTaskState(t, x, second, "pending_approval")
	if _, err := x.pool.Exec(ownerCtx(), "UPDATE aura.scheduler_tasks SET next_run_at = now()+interval '1 hour' WHERE id=$1", second); err != nil {
		t.Fatal(err)
	}
	if n, err := x.r.SubmitAnswers(ownerCtx(), answers); err != nil || n != 0 {
		t.Fatalf("retry = %d, %v", n, err)
	}
	requireTaskState(t, x, first, "cancelled")
	requireTaskState(t, x, second, "active")
	if countPersistedToolTurns(t, x.pool, x.conv) != 2 {
		t.Fatal("retry did not commit exactly two answers")
	}
}

func TestSubmitScheduledApprovalAppendFailureRollsBackTask(t *testing.T) {
	x := newExpireRig(t)
	id := x.task(t, cron.KindAgentJob, time.Now().Add(time.Hour))
	token := scheduledPause(t, x, id, sweepQuestion(id))
	x.r.resumeCommitter = NewPoolResumeCommitter(x.pool, conversations.New(x.pool, conversations.Config{TurnCapBytes: 32, RunDir: t.TempDir()}), x.pauses)
	if _, err := x.r.SubmitAnswer(ownerCtx(), token, ResponseInput{Action: askuser.ActionAccept}); !errors.Is(err, conversations.ErrContentSpillUnsupported) {
		t.Fatalf("submit = %v", err)
	}
	requireTaskState(t, x, id, "pending_approval")
	if resumedAt(t, x.pool, token) != nil || countPersistedToolTurns(t, x.pool, x.conv) != 0 {
		t.Fatal("append failure persisted part of the resume")
	}
}

func TestSubmitScheduledApprovalAcceptsRecurringTaskWithPastFirstFire(t *testing.T) {
	x := newExpireRig(t)
	id := x.task(t, cron.KindAgentJob, time.Now().Add(-time.Minute))
	if _, err := x.pool.Exec(ownerCtx(), `UPDATE aura.scheduler_tasks SET schedule_kind='every', every_minutes=15, run_at=NULL WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	token := scheduledPause(t, x, id, sweepQuestion(id))
	if _, err := x.r.SubmitAnswer(ownerCtx(), token, ResponseInput{Action: askuser.ActionAccept}); err != nil {
		t.Fatal(err)
	}
	requireTaskState(t, x, id, "active")
}

func TestSubmitScheduledApprovalAfterBoardDecision(t *testing.T) {
	for _, tc := range []struct{ action, status string }{
		{askuser.ActionAccept, "active"}, {askuser.ActionDecline, "cancelled"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			x := newExpireRig(t)
			id := x.task(t, cron.KindAgentJob, time.Now().Add(time.Hour))
			token := scheduledPause(t, x, id, sweepQuestion(id))
			store := cron.New(x.pool)
			var err error
			if tc.action == askuser.ActionAccept {
				err = store.ApproveTask(ownerCtx(), id)
			} else {
				err = store.CancelTask(ownerCtx(), id)
			}
			if err != nil {
				t.Fatal(err)
			}
			var decidedAt, after time.Time
			timestamp := "SELECT updated_at FROM aura.scheduler_tasks WHERE id=$1"
			if err := x.pool.QueryRow(ownerCtx(), timestamp, id).Scan(&decidedAt); err != nil {
				t.Fatal(err)
			}
			if _, err := x.r.SubmitAnswer(ownerCtx(), token, ResponseInput{Action: tc.action}); err != nil {
				t.Fatalf("acknowledge the board decision: %v", err)
			}
			if err := x.pool.QueryRow(ownerCtx(), timestamp, id).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if !decidedAt.Equal(after) {
				t.Fatal("acknowledgement rewrote the scheduler task")
			}
			requireTaskState(t, x, id, tc.status)
			if resumedAt(t, x.pool, token) == nil || countPersistedToolTurns(t, x.pool, x.conv) != 1 {
				t.Fatal("board decision did not close its chat pause")
			}
		})
	}
}

func TestSubmitScheduledApprovalAfterBoardDecisionAndFirstFire(t *testing.T) {
	x := newExpireRig(t)
	first := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	id := x.task(t, cron.KindAgentJob, first)
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	question := fmt.Sprintf("Approve scheduled agent_job task %.8s (every 15m), first run %s (Europe/Rome)? It runs: g. It will not run until you approve.", id, first.In(loc).Format("02/01 15:04"))
	token := scheduledPause(t, x, id, question)
	pending, err := x.pauses.GetByToken(ownerCtx(), token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.pool.Exec(ownerCtx(), `UPDATE aura.scheduler_tasks SET schedule_kind='every', every_minutes=15,
		run_at=NULL, origin_tool_call_id=$2 WHERE id=$1`, id, pending.ToolCallID); err != nil {
		t.Fatal(err)
	}
	store := cron.New(x.pool)
	if err := store.ApproveTask(ownerCtx(), id); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNextRunAt(ownerCtx(), id, first.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := x.r.SubmitAnswer(ownerCtx(), token, ResponseInput{Action: askuser.ActionAccept}); err != nil {
		t.Fatal(err)
	}
	if resumedAt(t, x.pool, token) == nil || countPersistedToolTurns(t, x.pool, x.conv) != 1 {
		t.Fatal("the original approved recurring task remained open in chat after its first fire")
	}
}

func TestSubmitScheduledApprovalWaitsPastDeadline(t *testing.T) {
	t.Run("chat", func(t *testing.T) {
		requireLateApprovalRefused(t, func(ctx context.Context, x expireRig, id, token string) error {
			_, err := x.r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept})
			return err
		})
	})
	t.Run("scheduler board and CLI", func(t *testing.T) {
		requireLateApprovalRefused(t, func(ctx context.Context, x expireRig, id, token string) error {
			return cron.New(x.pool).ApproveTask(ctx, id)
		})
	})
}

func requireLateApprovalRefused(t *testing.T, approve func(context.Context, expireRig, string, string) error) {
	t.Helper()
	x := newExpireRig(t)
	id := x.task(t, cron.KindAgentJob, time.Now().Add(time.Hour))
	token := scheduledPause(t, x, id, sweepQuestion(id))
	ctx, cancel := context.WithTimeout(ownerCtx(), 20*time.Second)
	defer cancel()
	var deadline time.Time
	if err := x.pool.QueryRow(ctx, `UPDATE aura.scheduler_tasks SET next_run_at = clock_timestamp()+interval '2 seconds'
		WHERE id=$1 RETURNING next_run_at`, id).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	lock, err := x.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	var locker int
	if err := lock.QueryRow(ctx, "SELECT pg_backend_pid() FROM aura.scheduler_tasks WHERE id=$1 FOR UPDATE", id).Scan(&locker); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- approve(ctx, x, id, token)
	}()
	t.Cleanup(func() { <-done })
	for {
		var waiting, beforeDeadline bool
		if err := x.pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))),
			clock_timestamp() < $2`, locker, deadline).Scan(&waiting, &beforeDeadline); err != nil {
			t.Fatal(err)
		}
		if !beforeDeadline {
			t.Fatal("approval did not reach the task lock before the deadline")
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := lock.Exec(ctx, "SELECT pg_sleep_until($1)", deadline); err != nil {
		t.Fatal(err)
	}
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Error("approval activated a one-shot after waiting past its deadline")
	}
	requireTaskState(t, x, id, "pending_approval")
	if resumedAt(t, x.pool, token) != nil || countPersistedToolTurns(t, x.pool, x.conv) != 0 {
		t.Fatal("late approval consumed the pause or recorded an answer")
	}
}
