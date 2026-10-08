//go:build db_integration

// Integration tests for the scheduler pause (pause.go, migration 0138) against a real
// Postgres: the status transitions, what a paused task is excluded from, the one-statement
// auto-pause, and the constraint that ties a paused status to its reason.
package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func pauseCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPauseAndResumeRoundTrip(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindReminder, "active")
	if _, err := s.pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET next_run_at = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	if err := s.PauseTask(ctx, id); err != nil {
		t.Fatalf("PauseTask: %v", err)
	}
	got, err := s.GetTask(ctx, id)
	if err != nil || got.Status != "paused" || got.PausedReason != PausedByOperator {
		t.Fatalf("after pause = %+v, %v; want paused by operator", got, err)
	}
	due, err := s.DueTasks(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range due {
		if d.ID == id {
			t.Fatal("a paused task that is past due must not be claimable")
		}
	}
	if err := s.RunTaskNow(ctx, id); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("run_now on a paused task = %v, want ErrTaskNotFound", err)
	}
	if err := s.PauseTask(ctx, id); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("second pause = %v, want ErrTaskNotFound", err)
	}
	board, err := s.ListManageableTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !boardHas(board, id, "paused") {
		t.Fatal("the cockpit board must list a paused task so it can be resumed")
	}

	before := time.Now()
	next, err := s.ResumeTask(ctx, id, before)
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	got, err = s.GetTask(ctx, id)
	if err != nil || got.Status != "active" || got.PausedReason != "" || got.ConsecutiveFailures != 0 {
		t.Fatalf("after resume = %+v, %v; want active with no reason", got, err)
	}
	if !next.After(before) || !got.NextRunAt.Equal(next) {
		t.Fatalf("resumed next fire = %v (stored %v), want after %v", next, got.NextRunAt, before)
	}
	if _, err := s.ResumeTask(ctx, id, time.Now()); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("resume of an active task = %v, want ErrTaskNotFound", err)
	}
}

func TestPauseRefusesATaskAwaitingApproval(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindAgentJob, "pending_approval")
	if err := s.PauseTask(ctx, id); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("pause of a pending task = %v, want ErrTaskNotFound", err)
	}
}

func TestRecordRunOutcomePausesAtTheThreshold(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindAgentJob, "active")

	for i := 1; i <= 2; i++ {
		out, err := s.RecordRunOutcome(ctx, id, false, 3)
		if err != nil || out.PausedNow || out.ConsecutiveFailures != i {
			t.Fatalf("failure %d = %+v, %v; want count %d and no pause", i, out, err, i)
		}
	}
	out, err := s.RecordRunOutcome(ctx, id, false, 3)
	if err != nil || !out.PausedNow || out.ConsecutiveFailures != 3 {
		t.Fatalf("third failure = %+v, %v; want the pause", out, err)
	}
	got, _ := s.GetTask(ctx, id)
	if got.Status != "paused" || got.PausedReason != PausedByFailures {
		t.Fatalf("task after the third failure = %+v, want paused by failures", got)
	}
	out, err = s.RecordRunOutcome(ctx, id, false, 3)
	if err != nil || out.PausedNow || out.ConsecutiveFailures != 4 {
		t.Fatalf("a failure of an already paused task = %+v, %v; must not report a new pause", out, err)
	}
}

func TestRecordRunOutcomeSuccessResetsTheCount(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindReminder, "active")
	for range 2 {
		if _, err := s.RecordRunOutcome(ctx, id, false, 3); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.RecordRunOutcome(ctx, id, true, 3)
	if err != nil || out.ConsecutiveFailures != 0 || out.PausedNow {
		t.Fatalf("success = %+v, %v; want the count reset", out, err)
	}
	out, err = s.RecordRunOutcome(ctx, id, false, 3)
	if err != nil || out.ConsecutiveFailures != 1 || out.PausedNow {
		t.Fatalf("failure after a success = %+v, %v; want a fresh count of 1", out, err)
	}
}

func TestRecordRunOutcomeZeroNeverPauses(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindAgentJob, "active")
	for i := range 5 {
		out, err := s.RecordRunOutcome(ctx, id, false, 0)
		if err != nil || out.PausedNow || out.ConsecutiveFailures != i+1 {
			t.Fatalf("failure %d with auto-pause off = %+v, %v", i+1, out, err)
		}
	}
	if got, _ := s.GetTask(ctx, id); got.Status != "active" {
		t.Fatalf("status = %q, want active with auto-pause off", got.Status)
	}
}

func TestRecordRunOutcomeKeepsAnOperatorPause(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindAgentJob, "active")
	if _, err := s.RecordRunOutcome(ctx, id, false, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseTask(ctx, id); err != nil {
		t.Fatal(err)
	}
	out, err := s.RecordRunOutcome(ctx, id, false, 2)
	if err != nil || out.PausedNow {
		t.Fatalf("a run failing after an operator pause = %+v, %v; the pause is the operator's", out, err)
	}
	if got, _ := s.GetTask(ctx, id); got.PausedReason != PausedByOperator {
		t.Fatalf("paused reason = %q, want operator", got.PausedReason)
	}
}

func TestResumeOfAOneShotWhoseTimeHasPassed(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindReminder, "active")
	if err := s.PauseTask(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET schedule_kind = 'at', cron_expr = NULL, run_at = now() - interval '1 hour'
		WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResumeTask(ctx, id, time.Now()); !errors.Is(err, ErrResumePastOneShot) {
		t.Fatalf("resume = %v, want ErrResumePastOneShot", err)
	}
	if got, _ := s.GetTask(ctx, id); got.Status != "paused" {
		t.Fatalf("a refused resume left status %q, want paused", got.Status)
	}
}

func TestEditingAPausedTaskKeepsItPaused(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindReminder, "active")
	if err := s.PauseTask(ctx, id); err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSchedule("every", "", 60, time.Time{}, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTask(ctx, id, UpdateTaskParams{Spec: spec, NextRunAt: time.Now().Add(time.Hour), Payload: []byte(`{"text":"new"}`), NotifyRoute: "stdout"}); err != nil {
		t.Fatalf("UpdateTask on a paused task: %v", err)
	}
	if got, _ := s.GetTask(ctx, id); got.Status != "paused" || got.EveryMinutes != 60 {
		t.Fatalf("after edit = %+v, want the new schedule and still paused", got)
	}
}

func TestCancellingAPausedTask(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindReminder, "active")
	if err := s.PauseTask(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelTask(ctx, id); err != nil {
		t.Fatalf("CancelTask on a paused task: %v", err)
	}
	if got, _ := s.GetTask(ctx, id); got.Status != "cancelled" || got.PausedReason != "" {
		t.Fatalf("after cancel = %+v, want cancelled with no pause reason", got)
	}
}

func TestPausedStatusRequiresAReason(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindReminder, "active")
	if _, err := s.pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET status = 'paused' WHERE id = $1`, id); err == nil {
		t.Fatal("a paused row without a reason must violate scheduler_tasks_paused_reason_iff_paused_chk")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET paused_reason = 'operator' WHERE id = $1`, id); err == nil {
		t.Fatal("an active row with a pause reason must violate the same constraint")
	}
}

func boardHas(tasks []Task, id, status string) bool {
	for _, t := range tasks {
		if t.ID == id && t.Status == status {
			return true
		}
	}
	return false
}

// TestDispatchPausesATaskAfterRepeatedFailures drives the real dispatcher over the real
// store: two failed runs of an agent job with the threshold at 2 leave it paused, the second
// notification says so, and the run ledger keeps the raw errors.
func TestDispatchPausesATaskAfterRepeatedFailures(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	id := makeTask(t, s, ctx, KindAgentJob, "active")
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, err: errors.New("credential expired")}
	notif := &captureNotifier{}
	d := NewDispatch(map[TaskKind]Handler{KindAgentJob: h}, DispatchDeps{
		Store: s, Notifier: notif, PauseAfterFailures: new(2),
	})

	var lastRun string
	for range 2 {
		task, err := s.GetTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.InsertRun(ctx, id, 0)
		if err != nil {
			t.Fatalf("InsertRun: %v", err)
		}
		lastRun = run.ID
		_ = d.Dispatch(ctx, task, &Claim{RunID: run.ID})
	}

	got, err := s.GetTask(ctx, id)
	if err != nil || got.Status != "paused" || got.PausedReason != PausedByFailures || got.ConsecutiveFailures != 2 {
		t.Fatalf("task = %+v, %v; want paused by 2 failures", got, err)
	}
	if len(notif.texts) != 2 || strings.Contains(notif.texts[0], "paused") || !strings.Contains(notif.texts[1], "paused after 2 failed runs in a row") {
		t.Fatalf("notifications = %q; only the second may report the pause", notif.texts)
	}
	run, err := s.GetRun(ctx, lastRun)
	if err != nil || run.Status != "failed" || run.LastError != "credential expired" {
		t.Fatalf("run = %+v, %v; want the raw error in the ledger", run, err)
	}
}
