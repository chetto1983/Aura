package cron

import (
	"context"
	"errors"
	"testing"
)

// reminderPrefix is the author line every record of task "task-1", a reminder, starts with.
const reminderPrefix = "[Scheduled task reminder task-1]\n"

// fakeConversationRecorder captures what a finished run wrote back to the
// conversation it was scheduled from.
type fakeConversationRecorder struct {
	appended []recordedTurn
	err      error
}

type recordedTurn struct {
	conversationID string
	text           string
}

func (f *fakeConversationRecorder) AppendAssistantTurn(_ context.Context, conversationID, text string) error {
	if f.err != nil {
		return f.err
	}
	f.appended = append(f.appended, recordedTurn{conversationID: conversationID, text: text})
	return nil
}

// TestCompletedRunLandsInItsOriginConversation pins the defect measured live on
// 2026-08-26: an operator scheduled a reminder FROM THE COCKPIT, the run completed
// at 22:16:39 with summary "SPIKE101-DELIVERED", and the result was pushed to
// Telegram — the only channel in the tree that implements Deliverer. The cockpit
// conversation ended at "Promemoria programmato ✅" and never received the outcome,
// so the operator who asked at their desk had to look at their phone for the answer.
//
// scheduler_tasks.origin_conversation_id was recorded all along and is threaded into
// the Job; only the approval-pause path ever read it. The rule is LibreChat's (D-00):
// the conversation IS the channel — a finished detached run saves its message and the
// operator sees it when they look. The push stays for an absent operator; this is the
// half that was missing.
func TestCompletedRunLandsInItsOriginConversation(t *testing.T) {
	t.Parallel()

	recorder := &fakeConversationRecorder{}
	handler := &fakeHandler{summary: "SPIKE101-DELIVERED"}
	d := NewDispatch(map[TaskKind]Handler{KindReminder: handler}, DispatchDeps{
		Store:                &fakeCompleter{},
		ConversationRecorder: recorder,
	})

	task := Task{ID: "task-1", Kind: KindReminder, NotifyRoute: "stdout", OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(recorder.appended) != 1 {
		t.Fatalf("appended %d turn(s) to the origin conversation, want 1 — the operator who scheduled from the cockpit never sees the outcome there", len(recorder.appended))
	}
	if recorder.appended[0].conversationID != "conv-1" {
		t.Errorf("appended to %q, want the task's origin conversation conv-1", recorder.appended[0].conversationID)
	}
	if got, want := recorder.appended[0].text, reminderPrefix+"SPIKE101-DELIVERED"; got != want {
		t.Errorf("appended %q, want the run's summary under its author %q", got, want)
	}
}

// fakeReportSteerer captures what a finished run steered to its origin conversation.
type fakeReportSteerer struct {
	steered []steeredReport
	err     error
}

type steeredReport struct{ identity, conv, text string }

func (f *fakeReportSteerer) SteerReport(identityID, conversationID, text string) error {
	if f.err != nil {
		return f.err
	}
	f.steered = append(f.steered, steeredReport{identity: identityID, conv: conversationID, text: text})
	return nil
}

func dispatchRecorded(t *testing.T, kind TaskKind, pub ReportSteerer) *fakeConversationRecorder {
	t.Helper()
	recorder := &fakeConversationRecorder{}
	d := NewDispatch(map[TaskKind]Handler{kind: &fakeHandler{summary: "Nessuna anomalia rilevata"}}, DispatchDeps{
		Store:                &fakeCompleter{},
		ConversationRecorder: recorder,
		Steer:                pub,
	})
	task := Task{ID: "01a221d9-2502-7170-afd4-b4ae70dbd81c", Kind: kind, NotifyRoute: "stdout", IdentityID: "id-1", OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("appended %d turn(s), want 1: the record is the durable copy whatever the push does", len(recorder.appended))
	}
	return recorder
}

// TestAgentJobRecordIsSteeredToItsConversation pins the run measured on the lab VM on
// 2026-11-28: a run_now agent_job recorded its report while the turn that fired it slept
// and polled, and that turn never saw it. The steer copy reaches it after its next tool call.
func TestAgentJobRecordIsSteeredToItsConversation(t *testing.T) {
	t.Parallel()
	pub := &fakeReportSteerer{}
	recorder := dispatchRecorded(t, KindAgentJob, pub)

	want := "[Scheduled task agent_job 01a221d9-2502-7170-afd4-b4ae70dbd81c]\nNessuna anomalia rilevata"
	if recorder.appended[0].text != want {
		t.Fatalf("record = %q, want %q", recorder.appended[0].text, want)
	}
	if len(pub.steered) != 1 {
		t.Fatalf("steered %d report(s), want 1", len(pub.steered))
	}
	if got := pub.steered[0]; got != (steeredReport{identity: "id-1", conv: "conv-1", text: want}) {
		t.Fatalf("steered %+v, want the record text to the owner id-1 on conv-1", got)
	}
}

// TestWatchRecordIsNotSteered: a steer row left at a turn's end starts a follow-on turn
// (Runner.deliverLeftoverSteer), so a watch that runs every few minutes must not push.
func TestWatchRecordIsNotSteered(t *testing.T) {
	t.Parallel()
	pub := &fakeReportSteerer{}
	dispatchRecorded(t, KindPIMWatch, pub)
	if len(pub.steered) != 0 {
		t.Fatalf("a pim_watch steered %d report(s), want 0", len(pub.steered))
	}
}

func TestSteerRefusalKeepsTheRecord(t *testing.T) {
	t.Parallel()
	dispatchRecorded(t, KindAgentJob, &fakeReportSteerer{err: errors.New("steer: message exceeds max size")})
}

func TestNilReportSteererRecordsOnly(t *testing.T) {
	t.Parallel()
	dispatchRecorded(t, KindAgentJob, nil)
}

// TestFailedRunAlsoLandsInItsOriginConversation mirrors the notify path's D-21 rule:
// a failure is exactly the outcome an operator must not have to go hunting for.
func TestFailedRunAlsoLandsInItsOriginConversation(t *testing.T) {
	t.Parallel()

	recorder := &fakeConversationRecorder{}
	handler := &fakeHandler{err: errors.New("boom")}
	d := NewDispatch(map[TaskKind]Handler{KindReminder: handler}, DispatchDeps{
		Store:                &fakeCompleter{},
		ConversationRecorder: recorder,
	})

	// Dispatch surfaces the handler's error to its caller; the recording is what this
	// test is about, so the error is expected rather than fatal.
	task := Task{ID: "task-1", Kind: KindReminder, NotifyRoute: "stdout", OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err == nil {
		t.Fatal("a failing handler must still surface its error")
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("appended %d turn(s) for a FAILED run, want 1", len(recorder.appended))
	}
	if got := recorder.appended[0].text; got == "" {
		t.Error("a failed run appended empty text; the operator learns nothing")
	}
}

// TestHousekeepingSweepsDoNotLandInAConversation proves the explicit `none` route
// suppresses conversation projection even if malformed input carries an origin id.
func TestHousekeepingSweepsDoNotLandInAConversation(t *testing.T) {
	t.Parallel()

	recorder := &fakeConversationRecorder{}
	handler := &fakeHandler{summary: "identity purge ok: purged 0 expired identit(y/ies)"}
	d := NewDispatch(map[TaskKind]Handler{KindIdentityPurge: handler}, DispatchDeps{
		Store:                &fakeCompleter{},
		ConversationRecorder: recorder,
	})

	task := Task{ID: "task-1", Kind: KindIdentityPurge, NotifyRoute: "none", OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got := len(recorder.appended); got != 0 {
		t.Fatalf("a routine housekeeping sweep appended %d turn(s) to a conversation", got)
	}
}

// TestUndeliveredPushIsSaidInTheOriginConversation pins the defect measured on the lab
// VM on 2026-10-05: a WhatsApp reminder whose send was refused still appeared in the
// cockpit conversation as plain text, which read as delivered while nothing had arrived.
// The failure may come from the route's notifier or from the origin channel itself.
func TestUndeliveredPushIsSaidInTheOriginConversation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		route   string
		deps    DispatchDeps
		wantTxt string
	}{
		{
			name:    "notifier refuses",
			route:   "whatsapp",
			deps:    DispatchDeps{Notifier: &captureNotifier{err: errors.New("Recipient must be provided")}},
			wantTxt: reminderPrefix + "drink water\n\nNot delivered via whatsapp: Recipient must be provided",
		},
		{
			name:  "origin channel fails",
			route: "stdout",
			deps: DispatchDeps{Notifier: &captureNotifier{}, ChannelDeliverer: &scriptedDeliverer{byIdentity: map[string]struct {
				delivered bool
				err       error
			}{"id-1": {err: errors.New("telegram down")}}}},
			wantTxt: reminderPrefix + "drink water\n\nNot delivered via stdout: telegram down",
		},
		{
			name:    "delivered",
			route:   "whatsapp",
			deps:    DispatchDeps{Notifier: &captureNotifier{}},
			wantTxt: reminderPrefix + "drink water",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := &fakeConversationRecorder{}
			deps := tc.deps
			deps.Store = &fakeNotificationStore{}
			deps.ConversationRecorder = recorder
			d := NewDispatch(map[TaskKind]Handler{KindReminder: &fakeHandler{summary: "drink water"}}, deps)

			task := Task{ID: "task-1", Kind: KindReminder, NotifyRoute: tc.route, IdentityID: "id-1", OriginConversationID: "conv-1"}
			if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
				t.Fatalf("an undelivered push must not fail the run: %v", err)
			}
			if len(recorder.appended) != 1 || recorder.appended[0].text != tc.wantTxt {
				t.Fatalf("origin conversation got %+v, want one turn %q", recorder.appended, tc.wantTxt)
			}
		})
	}
}

// TestOriginRecordingFailureDoesNotFailTheRun holds the recorder to the same
// contract the ledger has: the work already happened, so a failure to write the
// operator-facing copy is a WARN, never a run failure.
func TestOriginRecordingFailureDoesNotFailTheRun(t *testing.T) {
	t.Parallel()

	recorder := &fakeConversationRecorder{err: errors.New("pool exhausted")}
	handler := &fakeHandler{summary: "done"}
	d := NewDispatch(map[TaskKind]Handler{KindReminder: handler}, DispatchDeps{
		Store:                &fakeCompleter{},
		ConversationRecorder: recorder,
	})

	task := Task{ID: "task-1", Kind: KindReminder, NotifyRoute: "stdout", OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("a failed conversation write must not fail the run: %v", err)
	}
}
