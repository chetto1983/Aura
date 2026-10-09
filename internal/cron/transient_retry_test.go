package cron

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeRetryScheduler records each re-arm request and answers with a scripted outcome.
type fakeRetryScheduler struct {
	taskIDs []string
	delays  [][]time.Duration
	retry   TransientRetry
	ok      bool
	err     error
}

func (f *fakeRetryScheduler) ScheduleTransientRetry(_ context.Context, taskID string, delays []time.Duration) (TransientRetry, bool, error) {
	f.taskIDs = append(f.taskIDs, taskID)
	f.delays = append(f.delays, delays)
	return f.retry, f.ok, f.err
}

// retryableErr is the mark a handler puts on a failure that happened before the job did anything.
type retryableErr struct{ msg string }

func (e retryableErr) Error() string                { return e.msg }
func (e retryableErr) RetryableBeforeEffects() bool { return true }

type retryFixture struct {
	d      *Dispatch
	claim  *Claim
	comp   *fakeCompleter
	notif  *captureNotifier
	rec    *fakeOutcomeRecorder
	conv   *fakeConversationRecorder
	sched  *fakeRetryScheduler
	handle *fakeHandler
}

func newRetryFixture(t *testing.T, runErr error, sched *fakeRetryScheduler) retryFixture {
	t.Helper()
	f := retryFixture{
		comp:   &fakeCompleter{},
		notif:  &captureNotifier{},
		rec:    &fakeOutcomeRecorder{},
		conv:   &fakeConversationRecorder{},
		sched:  sched,
		handle: &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, err: runErr},
	}
	f.d, f.claim = newDispatchFor(t, f.handle, KindAgentJob, DispatchDeps{
		Store: f.comp, Notifier: f.notif, OutcomeRecorder: f.rec, PauseAfterFailures: new(3),
		ConversationRecorder: f.conv, RetryScheduler: sched,
	})
	return f
}

var retryTask = Task{
	ID: "t1", Kind: KindAgentJob, NotifyRoute: "email",
	OriginConversationID: "11111111-1111-1111-1111-111111111111",
}

// TestDispatchRetriesATransientFailureSilently pins the fix for the outage measured on the lab
// VM on 2026-10-09: a job whose model call failed before any tool ran is fired again later, and
// until then nobody is told it failed, nothing counts toward the auto-pause, and the origin
// conversation gets no entry. The run ledger still records the failed run.
func TestDispatchRetriesATransientFailureSilently(t *testing.T) {
	t.Parallel()
	runErr := retryableErr{msg: "agent_job run: provider returned HTTP 502"}
	sched := &fakeRetryScheduler{ok: true, retry: TransientRetry{At: time.Now().Add(2 * time.Minute), Number: 1}}
	f := newRetryFixture(t, runErr, sched)

	if err := f.d.Dispatch(context.Background(), retryTask, f.claim); !errors.Is(err, runErr) {
		t.Fatalf("Dispatch returned %v, want the handler's error", err)
	}
	if len(sched.taskIDs) != 1 || sched.taskIDs[0] != "t1" {
		t.Fatalf("re-arm requests = %v, want one for t1", sched.taskIDs)
	}
	if got := sched.delays[0]; len(got) != 4 || got[0] != 2*time.Minute || got[3] != 30*time.Minute {
		t.Fatalf("delays = %v, want 2, 5, 15, 30 min", got)
	}
	if len(f.comp.calls) != 1 || f.comp.calls[0].Status != "failed" || f.comp.calls[0].LastError != runErr.msg {
		t.Fatalf("the run ledger must record the failed run, got %+v", f.comp.calls)
	}
	if len(f.notif.texts) != 0 {
		t.Fatalf("a retried failure must not be notified, got %v", f.notif.texts)
	}
	if len(f.rec.calls) != 0 {
		t.Fatalf("a retried failure must not count toward the auto-pause, got %+v", f.rec.calls)
	}
	if len(f.conv.appended) != 0 {
		t.Fatalf("a retried failure must not be written to the origin conversation, got %+v", f.conv.appended)
	}
}

// TestDispatchReportsAFailureItCannotRetry covers every way the retry declines: the handler did
// not mark the failure, the task has used every delay or is no longer active, or the re-arm
// write failed. Each one is reported like any other failure, so the job is never silently lost.
func TestDispatchReportsAFailureItCannotRetry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		runErr     error
		sched      *fakeRetryScheduler
		wantRearms int
	}{
		{"unmarked failure", errors.New("invalid api key"), &fakeRetryScheduler{ok: true}, 0},
		{"retries exhausted", retryableErr{msg: "HTTP 502"}, &fakeRetryScheduler{ok: false}, 1},
		{"re-arm write failed", retryableErr{msg: "HTTP 502"}, &fakeRetryScheduler{err: errors.New("db down")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRetryFixture(t, tc.runErr, tc.sched)
			if err := f.d.Dispatch(context.Background(), retryTask, f.claim); !errors.Is(err, tc.runErr) {
				t.Fatalf("Dispatch returned %v, want the handler's error", err)
			}
			if len(tc.sched.taskIDs) != tc.wantRearms {
				t.Fatalf("re-arm requests = %d, want %d", len(tc.sched.taskIDs), tc.wantRearms)
			}
			if len(f.notif.texts) != 1 || f.notif.texts[0] != "agent_job failed: "+tc.runErr.Error() {
				t.Fatalf("the failure must be notified, got %v", f.notif.texts)
			}
			if len(f.rec.calls) != 1 || f.rec.calls[0].succeeded {
				t.Fatalf("the failure must be recorded as an outcome, got %+v", f.rec.calls)
			}
			if len(f.conv.appended) != 1 {
				t.Fatalf("the failure must reach the origin conversation, got %+v", f.conv.appended)
			}
		})
	}
}

// TestDispatchDoesNotRetryASuccess guards the nil error: a completed run never asks for a retry.
func TestDispatchDoesNotRetryASuccess(t *testing.T) {
	t.Parallel()
	sched := &fakeRetryScheduler{ok: true}
	f := newRetryFixture(t, nil, sched)
	f.handle.summary = "done"
	if err := f.d.Dispatch(context.Background(), retryTask, f.claim); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(sched.taskIDs) != 0 {
		t.Fatalf("a success must not be re-armed, got %v", sched.taskIDs)
	}
	if len(f.rec.calls) != 1 || !f.rec.calls[0].succeeded {
		t.Fatalf("the success must be recorded, got %+v", f.rec.calls)
	}
}

// TestNewDispatchTakesTheRetrySchedulerFromTheStore pins the production wiring: the composition
// root passes the *Store only, and a dispatcher without a retry scheduler would never retry.
func TestNewDispatchTakesTheRetrySchedulerFromTheStore(t *testing.T) {
	t.Parallel()
	s := &Store{}
	d := NewDispatch(nil, DispatchDeps{Store: s})
	if d.deps.RetryScheduler != s {
		t.Fatalf("RetryScheduler = %v, want the store", d.deps.RetryScheduler)
	}
	if NewDispatch(nil, DispatchDeps{Store: &fakeCompleter{}}).deps.RetryScheduler != nil {
		t.Fatal("a store that cannot re-arm must leave RetryScheduler nil")
	}
}

func TestRetryableBeforeEffectsReadsTheMarkThroughWrapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", errors.New("boom"), false},
		{"marked", retryableErr{msg: "502"}, true},
		{"wrapped mark", errors.Join(errors.New("context"), retryableErr{msg: "502"}), true},
	} {
		if got := retryableBeforeEffects(tc.err); got != tc.want {
			t.Errorf("%s: retryableBeforeEffects = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestStoreFakeScheduleTransientRetry covers the store's mapping without a database: the delay
// schedule binds as whole seconds, a returned row is a scheduled retry, no row means the task
// was not re-armed, and anything else is an error.
func TestStoreFakeScheduleTransientRetry(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 9, 12, 25, 31, 0, time.UTC)
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: []any{pgtype.Timestamptz{Time: at, Valid: true}, int32(2)}}}
	retry, ok, err := storeWithFake(f).ScheduleTransientRetry(context.Background(), fakeTaskID, transientRetryDelays)
	if err != nil || !ok || retry != (TransientRetry{At: at, Number: 2}) {
		t.Fatalf("ScheduleTransientRetry = %+v ok=%v err=%v", retry, ok, err)
	}
	if got := f.gotArgs[0].([]int32); len(got) != 4 || got[0] != 120 || got[3] != 1800 {
		t.Fatalf("delays bound as %v, want [120 300 900 1800]", got)
	}

	if _, ok, err := storeWithFake(&cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: pgx.ErrNoRows}}).
		ScheduleTransientRetry(context.Background(), fakeTaskID, transientRetryDelays); err != nil || ok {
		t.Fatalf("no row must mean not re-armed, got ok=%v err=%v", ok, err)
	}
	if _, _, err := storeWithFake(&cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: errDB}}).
		ScheduleTransientRetry(context.Background(), fakeTaskID, transientRetryDelays); !errors.Is(err, errDB) {
		t.Fatalf("a DB error must wrap, got %v", err)
	}
	if _, _, err := storeWithFake(&cronFakeDBTX{}).ScheduleTransientRetry(context.Background(), "bogus", nil); err == nil {
		t.Fatal("an invalid task id must be rejected")
	}
}
