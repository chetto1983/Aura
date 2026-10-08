package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeOutcomeRecorder struct {
	calls   []recordedOutcome
	outcome RunOutcome
	err     error
}

type recordedOutcome struct {
	taskID     string
	succeeded  bool
	pauseAfter int
}

func (r *fakeOutcomeRecorder) RecordRunOutcome(_ context.Context, taskID string, succeeded bool, pauseAfter int) (RunOutcome, error) {
	r.calls = append(r.calls, recordedOutcome{taskID: taskID, succeeded: succeeded, pauseAfter: pauseAfter})
	return r.outcome, r.err
}

func TestPauseAfterFailuresReadsTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"", defaultPauseAfterFailures},
		{"5", 5},
		{"0", 0},
		{"-2", 0},
		{"three", defaultPauseAfterFailures},
	} {
		t.Setenv("AURA_SCHEDULER_PAUSE_AFTER_FAILURES", tc.value)
		if got := pauseAfterFailures(); got != tc.want {
			t.Errorf("AURA_SCHEDULER_PAUSE_AFTER_FAILURES=%q → %d, want %d", tc.value, got, tc.want)
		}
	}
}

func TestDispatchReportsTheFailureThatPausedTheTask(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, err: errors.New("token expired")}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	rec := &fakeOutcomeRecorder{outcome: RunOutcome{ConsecutiveFailures: 3, PausedNow: true}}
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{
		Store: comp, Notifier: notif, OutcomeRecorder: rec, PauseAfterFailures: new(3),
	})

	err := d.Dispatch(context.Background(), Task{ID: "t", Kind: KindAgentJob, NotifyRoute: "email"}, c)
	if err == nil || err.Error() != "token expired" {
		t.Fatalf("Dispatch returned %v, want the handler's own error", err)
	}
	if got := rec.calls; len(got) != 1 || got[0] != (recordedOutcome{taskID: "t", succeeded: false, pauseAfter: 3}) {
		t.Fatalf("recorded outcomes = %+v", got)
	}
	if comp.calls[0].LastError != "token expired" {
		t.Fatalf("the run ledger must keep the raw error, got %q", comp.calls[0].LastError)
	}
	if len(notif.texts) != 1 || !strings.Contains(notif.texts[0], "token expired; paused after 3 failed runs in a row") {
		t.Fatalf("notification must say the task paused, got %v", notif.texts)
	}
}

func TestDispatchCountsASuccess(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "ok"}
	rec := &fakeOutcomeRecorder{}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{
		Store: &fakeCompleter{}, Notifier: &captureNotifier{}, OutcomeRecorder: rec, PauseAfterFailures: new(2),
	})
	if err := d.Dispatch(context.Background(), Task{ID: "r", Kind: KindReminder, NotifyRoute: "none"}, c); err != nil {
		t.Fatal(err)
	}
	if got := rec.calls; len(got) != 1 || !got[0].succeeded || got[0].pauseAfter != 2 {
		t.Fatalf("a success must reset the count: %+v", got)
	}
}

func TestDispatchNeverCountsATaskThatCannotBePaused(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindBackupPostgres}, err: errors.New("disk full")}
	rec := &fakeOutcomeRecorder{outcome: RunOutcome{PausedNow: true}}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindBackupPostgres, DispatchDeps{
		Store: &fakeCompleter{}, Notifier: notif, OutcomeRecorder: rec, PauseAfterFailures: new(1),
	})
	_ = d.Dispatch(context.Background(), Task{ID: "b", Kind: KindBackupPostgres, NotifyRoute: "email"}, c)
	if len(rec.calls) != 0 {
		t.Fatalf("the backup must never be counted toward a pause: %+v", rec.calls)
	}
	if len(notif.texts) != 1 || strings.Contains(notif.texts[0], "paused") {
		t.Fatalf("a backup failure must not claim a pause: %v", notif.texts)
	}
}

func TestDispatchKeepsTheRunErrorWhenTheOutcomeCannotBeRecorded(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, err: errors.New("boom")}
	rec := &fakeOutcomeRecorder{err: errors.New("db down"), outcome: RunOutcome{PausedNow: true}}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{
		Store: &fakeCompleter{}, Notifier: notif, OutcomeRecorder: rec, PauseAfterFailures: new(1),
	})
	_ = d.Dispatch(context.Background(), Task{ID: "a", Kind: KindAgentJob, NotifyRoute: "email"}, c)
	if len(notif.texts) != 1 || strings.Contains(notif.texts[0], "paused") || !strings.Contains(notif.texts[0], "boom") {
		t.Fatalf("a failed outcome write must leave the report as the run error: %v", notif.texts)
	}
}

func TestResumeFireStartsFromNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 3, 4, 10, 7, 0, 0, time.UTC)
	every, err := ResumeFire(Task{ScheduleKind: KindEvery, EveryMinutes: 30, TZ: "UTC"}, now)
	if err != nil || !every.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("every-30 resume = %v, %v; want %v", every, err, now.Add(30*time.Minute))
	}
	cron, err := ResumeFire(Task{ScheduleKind: KindCron, CronExpr: "0 9 * * *", TZ: "UTC"}, now)
	if want := time.Date(2030, 3, 5, 9, 0, 0, 0, time.UTC); err != nil || !cron.Equal(want) {
		t.Fatalf("cron resume = %v, %v; want %v", cron, err, want)
	}
	future := now.Add(time.Hour)
	at, err := ResumeFire(Task{ScheduleKind: KindAt, RunAt: future, TZ: "UTC"}, now)
	if err != nil || !at.Equal(future) {
		t.Fatalf("future one-shot resume = %v, %v; want %v", at, err, future)
	}
	if _, err := ResumeFire(Task{ScheduleKind: KindAt, RunAt: now.Add(-time.Hour), TZ: "UTC"}, now); !errors.Is(err, ErrResumePastOneShot) {
		t.Fatalf("past one-shot resume err = %v, want ErrResumePastOneShot", err)
	}
}
