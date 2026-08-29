package cron

import (
	"context"
	"errors"
	"testing"
)

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
	if recorder.appended[0].text != "SPIKE101-DELIVERED" {
		t.Errorf("appended %q, want the run's summary", recorder.appended[0].text)
	}
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
