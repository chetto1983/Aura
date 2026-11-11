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
	limit    int
	budget   int
	listErr  error
	casErr   error
	runErr   error
}

func (f *fakeLapsedStore) ListLapsedApprovals(_ context.Context, limit int) ([]Task, error) {
	f.limit = limit
	return f.tasks, f.listErr
}

func (f *fakeLapsedStore) ExpireLapsedApproval(_ context.Context, id string) (bool, error) {
	f.casCalls++
	if f.casErr != nil {
		return false, f.casErr
	}
	if f.cas {
		f.expired = append(f.expired, id)
	}
	return f.cas, nil
}

func (f *fakeLapsedStore) InsertRun(_ context.Context, taskID string, stepBudget int) (Run, error) {
	if f.runErr != nil {
		return Run{}, f.runErr
	}
	f.runs = append(f.runs, taskID)
	f.budget = stepBudget
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
	approved    bool
	activated   int
	expired     int
	reads       int
	readErr     error
	activateErr error
	expireErr   error
}

func (f *fakeLapsedMessages) ApprovedForTask(context.Context, string, string) (bool, error) {
	f.reads++
	return f.approved, f.readErr
}

func (f *fakeLapsedMessages) ActivateReviewedMessage(context.Context, string, string) error {
	f.activated++
	return f.activateErr
}

func (f *fakeLapsedMessages) ExpireForTask(context.Context, string, string) error {
	f.expired++
	return f.expireErr
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

func TestTheLapsedPassBoundsItsWorkAndRecordsAPlainRun(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), true)
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.store.limit != 50 || rig.store.budget != 0 {
		t.Fatalf("limit %d, step budget %d; want 50 and 0", rig.store.limit, rig.store.budget)
	}
}

// Only a message's draft is read: an approved draft never activates another kind.
func TestOnlyAMessageReadsItsDraft(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), true)
	rig.messages.approved = true
	rig.closer.result = PauseNone
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.messages.reads != 0 || rig.messages.activated != 0 || rig.messages.expired != 0 || len(rig.store.expired) != 1 {
		t.Fatalf("reads %d, activated %d, draft expired %d, task expired %v", rig.messages.reads, rig.messages.activated, rig.messages.expired, rig.store.expired)
	}
}

func TestAListErrorStopsThePass(t *testing.T) {
	d, rig := newLapsedRig(lapsedJob(), true)
	rig.store.listErr = errors.New("db down")
	if err := d.ExpireLapsedApprovals(context.Background()); err == nil {
		t.Fatal("a list error was swallowed")
	}
	if len(rig.closer.answers) != 0 {
		t.Fatal("a row was handled after the list failed")
	}
}

func TestAPassWithoutAStoreDoesNothing(t *testing.T) {
	d := NewDispatch(nil, DispatchDeps{})
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
}

// Every store failure leaves the row for the next tick: nothing after it runs.
func TestAFailedStepLeavesTheRowForTheNextTick(t *testing.T) {
	message := lapsedJob()
	message.Kind = KindMessage
	cases := map[string]struct {
		task  Task
		setup func(*lapsedRig)
		cas   int
	}{
		"the draft cannot be read":         {message, func(r *lapsedRig) { r.messages.readErr = errors.New("x") }, 0},
		"the approved one cannot activate": {message, func(r *lapsedRig) { r.messages.approved, r.messages.activateErr = true, errors.New("x") }, 0},
		"the draft cannot be expired": {message, func(r *lapsedRig) {
			r.closer.result, r.messages.expireErr = PauseNone, errors.New("x")
		}, 0},
		"the CAS fails":             {lapsedJob(), func(r *lapsedRig) { r.store.casErr = errors.New("x") }, 1},
		"the run cannot be written": {lapsedJob(), func(r *lapsedRig) { r.store.runErr = errors.New("x") }, 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d, rig := newLapsedRig(c.task, true)
			c.setup(rig)
			if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
				t.Fatalf("pass: %v", err)
			}
			if rig.store.casCalls != c.cas || len(rig.recorder.appended) != 0 || len(rig.completer.calls) != 0 {
				t.Fatalf("CAS %d (want %d), notices %v, completions %d", rig.store.casCalls, c.cas, rig.recorder.appended, len(rig.completer.calls))
			}
		})
	}
}

// A closed pause on a message does not expire its draft a second time.
func TestAClosedMessagePauseDoesNotExpireTheDraftAgain(t *testing.T) {
	message := lapsedJob()
	message.Kind = KindMessage
	d, rig := newLapsedRig(message, true)
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if rig.messages.expired != 0 || len(rig.store.expired) != 1 {
		t.Fatalf("draft expired %d, task expired %v", rig.messages.expired, rig.store.expired)
	}
}

func TestTheNoticeFallsBackToUTCAndTheIDIsCutAtEight(t *testing.T) {
	task := lapsedJob()
	task.TZ = "Not/AZone"
	if got := lapsedApprovalText(task); got != "agent_job for 02/11 17:00 (UTC) was not approved in time and did not run." {
		t.Fatalf("notice = %q", got)
	}
	for id, want := range map[string]string{"12345678": "12345678", "123456789": "12345678", "1234567": "1234567"} {
		if got := shortID(id); got != want {
			t.Errorf("shortID(%q) = %q, want %q", id, got, want)
		}
	}
}
