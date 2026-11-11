package cron

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLapsedStore serves the lapsed one-shots and records the expiry's CAS and its run.
type fakeLapsedStore struct {
	tasks    []Task
	cas      bool
	expired  []string
	runs     []string
	casCalls int
}

func (f *fakeLapsedStore) ListLapsedApprovals(context.Context, int) ([]Task, error) {
	return f.tasks, nil
}

func (f *fakeLapsedStore) ExpireLapsedApproval(_ context.Context, id string) (bool, error) {
	f.casCalls++
	if f.cas {
		f.expired = append(f.expired, id)
	}
	return f.cas, nil
}

func (f *fakeLapsedStore) InsertRun(_ context.Context, taskID string, _ int) (Run, error) {
	f.runs = append(f.runs, taskID)
	return Run{ID: "run-1", TaskID: taskID}, nil
}

// fakeCloser answers every close with result, or err.
type fakeCloser struct {
	result  PauseClosed
	err     error
	answers []string
}

func (f *fakeCloser) CloseLapsedApproval(_ context.Context, _ Task, answer string) (PauseClosed, error) {
	f.answers = append(f.answers, answer)
	return f.result, f.err
}

// fakeLapsedMessages is a message task's draft as the pass sees it.
type fakeLapsedMessages struct {
	approved  bool
	activated int
	expired   int
}

func (f *fakeLapsedMessages) ApprovedForTask(context.Context, string, string) (bool, error) {
	return f.approved, nil
}

func (f *fakeLapsedMessages) ActivateReviewedMessage(context.Context, string, string) error {
	f.activated++
	return nil
}

func (f *fakeLapsedMessages) ExpireForTask(context.Context, string, string) error {
	f.expired++
	return nil
}

type lapsedRig struct {
	store     *fakeLapsedStore
	closer    *fakeCloser
	messages  *fakeLapsedMessages
	completer *fakeCompleter
	notifier  *captureNotifier
	recorder  *fakeConversationRecorder
}

func newLapsedRig(task Task, cas bool) (*Dispatch, *lapsedRig) {
	rig := &lapsedRig{
		store: &fakeLapsedStore{tasks: []Task{task}, cas: cas}, closer: &fakeCloser{result: PauseClosedNow},
		messages: &fakeLapsedMessages{}, completer: &fakeCompleter{}, notifier: &captureNotifier{},
		recorder: &fakeConversationRecorder{},
	}
	d := NewDispatch(nil, DispatchDeps{
		Store: rig.completer, Notifier: rig.notifier, ConversationRecorder: rig.recorder,
		LapsedApprovals: rig.store, LapsedPauseCloser: rig.closer, LapsedMessages: rig.messages,
	})
	return d, rig
}

func lapsedJob() Task {
	return Task{
		ID: "01a19af5-0000-7000-8000-000000000000", Kind: KindAgentJob, Status: "pending_approval", TZ: "Europe/Rome",
		NextRunAt: time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC), NotifyRoute: string(RouteStdout),
		IdentityID: "owner", OriginConversationID: "conv-1",
	}
}

func (r *lapsedRig) quiet(t *testing.T) {
	t.Helper()
	if len(r.store.runs) != 0 || len(r.notifier.texts) != 0 || len(r.recorder.appended) != 0 || len(r.completer.calls) != 0 {
		t.Fatalf("runs %v, notices %v, records %v, completions %d", r.store.runs, r.notifier.texts, r.recorder.appended, len(r.completer.calls))
	}
}

func TestALapsedOneShotIsExpiredReportedAndClosed(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), true)
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	wantAnswer := "Scheduled agent_job task 01a19af5 was not approved in time; it has been cancelled and did not run. Do NOT re-schedule it unless the operator asks."
	if len(rig.closer.answers) != 1 || rig.closer.answers[0] != wantAnswer {
		t.Fatalf("closer answers = %v", rig.closer.answers)
	}
	if len(rig.store.expired) != 1 || len(rig.store.runs) != 1 {
		t.Fatalf("expired %v, runs %v", rig.store.expired, rig.store.runs)
	}
	if len(rig.completer.calls) != 1 || rig.completer.calls[0].Status != "failed" {
		t.Fatalf("completion = %+v", rig.completer.calls)
	}
	const notice = "agent_job for 02/11 18:00 (Europe/Rome) was not approved in time and did not run."
	if len(rig.recorder.appended) != 1 || rig.recorder.appended[0].text != notice {
		t.Fatalf("recorded %+v, want %q", rig.recorder.appended, notice)
	}
}

// Decision 26: a pause that cannot be closed now leaves the task pending for the next tick.
func TestAPauseThatCannotBeClosedIsRetriedNextTick(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), true)
	rig.closer.err = errors.New("thread busy")
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.store.casCalls != 0 {
		t.Fatal("the task was expired under a live pause")
	}
	rig.quiet(t)
}

func TestALostRaceSendsNothing(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), false)
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	rig.quiet(t)
}

// Decision 36: an accept already given is the hook's to apply.
func TestAnApprovalInFlightIsNotExpired(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), true)
	rig.closer.result = PauseAnswered
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.store.casCalls != 0 {
		t.Fatal("an approval in flight was expired")
	}
	rig.quiet(t)
}

func TestAnApprovedMessageDueNowIsActivatedNotCancelled(t *testing.T) {
	task := lapsedJob()
	task.Kind = KindMessage
	d, rig := newLapsedRig(task, true)
	rig.messages.approved = true
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.messages.activated != 1 || len(rig.closer.answers) != 0 || rig.store.casCalls != 0 {
		t.Fatalf("activated %d, closes %d, CAS %d", rig.messages.activated, len(rig.closer.answers), rig.store.casCalls)
	}
	rig.quiet(t)
}

func TestAMessageWithNoPauseHasItsDraftExpired(t *testing.T) {
	task := lapsedJob()
	task.Kind = KindMessage
	d, rig := newLapsedRig(task, true)
	rig.closer.result = PauseNone
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.messages.expired != 1 || len(rig.store.expired) != 1 || len(rig.recorder.appended) != 1 {
		t.Fatalf("draft expired %d, task expired %v, notices %v", rig.messages.expired, rig.store.expired, rig.recorder.appended)
	}
}
