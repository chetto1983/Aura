package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// tellingHandler says one thing to the person, another to the conversation and a third to the
// ledger, as pim_watch does: a sender and a subject must never reach agent_job_runs.summary.
type tellingHandler struct {
	fakeHandler
	notice, record string
}

func (h *tellingHandler) Run(ctx context.Context, job Job) (string, error) {
	if job.Tell != nil {
		job.Tell(h.notice, h.record)
	}
	return h.fakeHandler.Run(ctx, job)
}

func TestToldTextGoesToItsOwnPlace(t *testing.T) {
	t.Parallel()
	h := &tellingHandler{
		meta:    HandlerMeta{Kind: KindPIMWatch},
		summary: "1 mail, 1 alert",
		notice:  "Mario Rossi: fermo linea",
		record:  "framed Mario Rossi: fermo linea",
	}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	recorder := &fakeConversationRecorder{}
	d, c := newDispatchFor(t, h, KindPIMWatch, DispatchDeps{Store: comp, Notifier: notif, ConversationRecorder: recorder})

	task := Task{ID: "t", Kind: KindPIMWatch, NotifyRoute: string(RouteWhatsApp), OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 1 || notif.texts[0] != h.notice {
		t.Fatalf("notification = %q, want the notice", notif.texts)
	}
	if len(recorder.appended) != 1 || recorder.appended[0].text != h.record {
		t.Fatalf("record = %+v, want the record", recorder.appended)
	}
	if len(comp.calls) != 1 || comp.calls[0].Summary != "1 mail, 1 alert" {
		t.Fatalf("ledger = %+v, want the summary", comp.calls)
	}
}

func TestAHandlerThatNeverTellsBehavesAsBefore(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "buy milk"}
	notif := &captureNotifier{}
	recorder := &fakeConversationRecorder{}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{Store: &fakeCompleter{}, Notifier: notif, ConversationRecorder: recorder})

	task := Task{ID: "t", Kind: KindReminder, NotifyRoute: string(RouteWhatsApp), OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 1 || notif.texts[0] != "buy milk" || recorder.appended[0].text != "buy milk" {
		t.Fatalf("notification %q, record %+v, want the summary in both", notif.texts, recorder.appended)
	}
}

// The stdout route would write a mail's sender and subject into the process journal, a third
// place spec §5.2 does not allow. On the cockpit route the conversation record is the delivery.
func TestAToldNoticeStaysOutOfTheStdoutJournal(t *testing.T) {
	t.Parallel()
	h := &tellingHandler{meta: HandlerMeta{Kind: KindPIMWatch}, summary: "1 alert", notice: "secret subject", record: "framed"}
	notif := &captureNotifier{}
	recorder := &fakeConversationRecorder{}
	d, c := newDispatchFor(t, h, KindPIMWatch, DispatchDeps{Store: &fakeCompleter{}, Notifier: notif, ConversationRecorder: recorder})

	task := Task{ID: "t", Kind: KindPIMWatch, NotifyRoute: string(RouteStdout), OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 0 {
		t.Fatalf("stdout notifier got %q", notif.texts)
	}
	if len(recorder.appended) != 1 || recorder.appended[0].text != "framed" {
		t.Fatalf("record = %+v", recorder.appended)
	}
}

// A hard failure says so once (spec §5.6): the runs before the pausing one stay silent, and the
// run that pauses is always reported, though its error asks for silence.
func TestAPausingFailureIsReportedThoughItAskedForSilence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		pausedNow bool
		notified  int
	}{
		{"a failure before the pausing one stays silent", false, 0},
		{"the run that pauses is reported", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHandler{meta: HandlerMeta{Kind: KindPIMWatch}, err: suppressedNotificationError{}}
			notif := &captureNotifier{}
			recorder := &fakeConversationRecorder{}
			rec := &fakeOutcomeRecorder{outcome: RunOutcome{ConsecutiveFailures: 3, PausedNow: tc.pausedNow}}
			d, c := newDispatchFor(t, h, KindPIMWatch, DispatchDeps{
				Store: &fakeCompleter{}, Notifier: notif, OutcomeRecorder: rec, PauseAfterFailures: new(3), ConversationRecorder: recorder,
			})

			task := Task{ID: "t", Kind: KindPIMWatch, NotifyRoute: string(RouteWhatsApp), OriginConversationID: "conv-1"}
			err := d.Dispatch(context.Background(), task, c)
			if !errors.Is(err, suppressedNotificationError{}) {
				t.Fatalf("Dispatch = %v, want the raw error", err)
			}
			if len(notif.texts) != tc.notified {
				t.Fatalf("notifications = %q, want %d", notif.texts, tc.notified)
			}
			if len(recorder.appended) != tc.notified {
				t.Fatalf("records = %+v, want %d", recorder.appended, tc.notified)
			}
			if tc.pausedNow && !strings.Contains(notif.texts[0], "paused after 3 failed runs") {
				t.Fatalf("the report does not say it paused: %q", notif.texts[0])
			}
		})
	}
}

func TestPausedRunErrorKeepsItsCauseAndItsText(t *testing.T) {
	t.Parallel()
	cause := errors.New("provider_auth")
	err := pausedRunError{cause: cause, failures: 3}
	if !errors.Is(err, cause) {
		t.Fatal("the cause is not reachable")
	}
	if got, want := err.Error(), "provider_auth; paused after 3 failed runs in a row, resume it to run again"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// A check that cannot be paused keeps its silence: the suppressor's other caller.
func TestAnUnpausableKindKeepsItsSilence(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindObservabilityCheck}, err: suppressedNotificationError{}}
	notif := &captureNotifier{}
	rec := &fakeOutcomeRecorder{outcome: RunOutcome{ConsecutiveFailures: 3, PausedNow: true}}
	d, c := newDispatchFor(t, h, KindObservabilityCheck, DispatchDeps{
		Store: &fakeCompleter{}, Notifier: notif, OutcomeRecorder: rec, PauseAfterFailures: new(3),
	})

	if err := d.Dispatch(context.Background(), Task{ID: "o", Kind: KindObservabilityCheck, NotifyRoute: string(RouteWhatsApp)}, c); err == nil {
		t.Fatal("Dispatch returned nil")
	}
	if len(notif.texts) != 0 {
		t.Fatalf("notifications = %q, want none", notif.texts)
	}
}
