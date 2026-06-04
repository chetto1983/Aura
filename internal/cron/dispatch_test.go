package cron

import (
	"context"
	"errors"
	"testing"
)

// fakeHandler is a scripted cron.Handler for routing/notify tests.
type fakeHandler struct {
	meta    HandlerMeta
	summary string
	err     error
	ran     bool
	gotJob  Job
}

func (h *fakeHandler) Meta() HandlerMeta { return h.meta }

func (h *fakeHandler) Run(_ context.Context, job Job) (string, error) {
	h.ran = true
	h.gotJob = job
	return h.summary, h.err
}

// fakeCompleter captures the terminal run write.
type fakeCompleter struct {
	calls []CompleteRunParams
	err   error
}

func (c *fakeCompleter) CompleteRun(_ context.Context, p CompleteRunParams) error {
	c.calls = append(c.calls, p)
	return c.err
}

// captureNotifier records every delivery.
type captureNotifier struct {
	routes []NotifyRoute
	texts  []string
	err    error
}

func (n *captureNotifier) Notify(_ context.Context, route NotifyRoute, _ string, text string) error {
	n.routes = append(n.routes, route)
	n.texts = append(n.texts, text)
	return n.err
}

func newDispatchFor(t *testing.T, h Handler, kind TaskKind, deps DispatchDeps) (*Dispatch, *Claim) {
	t.Helper()
	d := NewDispatch(map[TaskKind]Handler{kind: h}, deps)
	return d, &Claim{RunID: "run-1"}
}

func TestDispatchRoutesByKindAndCompletes(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "buy milk"}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{Store: comp, Notifier: notif})

	task := Task{ID: "t1", Kind: KindReminder, Payload: []byte(`{"text":"buy milk"}`), StepBudget: 7, NotifyRoute: "whatsapp"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !h.ran {
		t.Fatal("the reminder handler must have run")
	}
	if h.gotJob.StepBudget != 7 || h.gotJob.RunID != "run-1" {
		t.Fatalf("handler got wrong job: %+v", h.gotJob)
	}
	if len(comp.calls) != 1 || comp.calls[0].Status != "completed" || comp.calls[0].Summary != "buy milk" {
		t.Fatalf("expected one completed run write with the summary, got %+v", comp.calls)
	}
	if comp.calls[0].CompletedHash != "run-1" {
		t.Fatalf("idempotency hash must be the run id, got %q", comp.calls[0].CompletedHash)
	}
	if len(notif.texts) != 1 || notif.texts[0] != "buy milk" {
		t.Fatalf("expected the summary notified, got %v", notif.texts)
	}
}

func TestDispatchFailureCompletesFailedAndNotifies(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, err: errors.New("model exploded")}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{Store: comp, Notifier: notif})

	task := Task{ID: "t2", Kind: KindAgentJob, NotifyRoute: "email"}
	err := d.Dispatch(context.Background(), task, c)
	if err == nil {
		t.Fatal("a handler error must propagate")
	}
	if comp.calls[0].Status != "failed" || comp.calls[0].LastError != "model exploded" {
		t.Fatalf("expected a failed run with LastError, got %+v", comp.calls[0])
	}
	// D-21: a failed job notifies too, with the LastError in the text.
	if len(notif.texts) != 1 || notif.texts[0] == "" {
		t.Fatalf("a failed job must still notify (D-21), got %v", notif.texts)
	}
}

func TestDispatchUnknownKindFailsLoud(t *testing.T) {
	t.Parallel()
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d := NewDispatch(map[TaskKind]Handler{}, DispatchDeps{Store: comp, Notifier: notif})

	err := d.Dispatch(context.Background(), Task{ID: "t3", Kind: TaskKind("bogus")}, &Claim{RunID: "r"})
	if err == nil {
		t.Fatal("an unknown kind must fail loud, not silently drop")
	}
	if len(comp.calls) != 1 || comp.calls[0].Status != "failed" {
		t.Fatalf("unknown kind must record a failed run, got %+v", comp.calls)
	}
}

func TestDispatchDestructiveRidesImmediateAlert(t *testing.T) {
	t.Parallel()
	// A destructive agent_job payload (matches the destructive keyword) is Destructive
	// tier → RequiresImmediateAlert at the default Risky threshold → it must notify
	// even though it would otherwise be quiet-hours-deferrable (D-27).
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, summary: "did the thing"}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{
		Store:      comp,
		Notifier:   notif,
		QuietHours: func(string) bool { return true }, // pretend it's always quiet hours
	})

	task := Task{ID: "t4", Kind: KindAgentJob, Payload: []byte(`{"goal":"drop the staging db"}`), NotifyRoute: "whatsapp"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 1 {
		t.Fatalf("a destructive task must ride the immediate alert through quiet hours (D-27), got %v", notif.texts)
	}
}

func TestDispatchQuietHoursDefersNonDestructive(t *testing.T) {
	t.Parallel()
	// A safe agent_job summary inside quiet hours defers (no notification this tick).
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, summary: "routine summary"}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{
		Store:      comp,
		Notifier:   notif,
		QuietHours: func(string) bool { return true },
	})

	task := Task{ID: "t5", Kind: KindAgentJob, Payload: []byte(`{"goal":"summarize the news"}`), NotifyRoute: "whatsapp"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 0 {
		t.Fatalf("a non-destructive notification must defer inside quiet hours (D-23), got %v", notif.texts)
	}
	// The run is still completed + recorded even when the notification defers.
	if len(comp.calls) != 1 || comp.calls[0].Status != "completed" {
		t.Fatalf("the run must complete even when notification defers, got %+v", comp.calls)
	}
}

func TestDispatchReminderFiresInQuietHours(t *testing.T) {
	t.Parallel()
	// A reminder the user explicitly scheduled still fires inside quiet hours — its
	// delivery IS the task, not an advisory notification (D-23).
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "take your meds"}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{
		Store:      comp,
		Notifier:   notif,
		QuietHours: func(string) bool { return true },
	})

	task := Task{ID: "t6", Kind: KindReminder, Payload: []byte(`{"text":"take your meds"}`), NotifyRoute: "whatsapp"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 1 || notif.texts[0] != "take your meds" {
		t.Fatalf("an in-window reminder must still fire (D-23), got %v", notif.texts)
	}
}

func TestDispatchCompleterErrorIsNonFatal(t *testing.T) {
	t.Parallel()
	// A CompleteRun error (e.g. idempotent 23505 swallow) must not crash the dispatch —
	// it is logged; the handler result still propagates.
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "ok"}
	comp := &fakeCompleter{err: ErrAlreadyRunning}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{Store: comp, Notifier: &captureNotifier{}})
	if err := d.Dispatch(context.Background(), Task{ID: "t7", Kind: KindReminder}, c); err != nil {
		t.Fatalf("a completer error must not surface as a dispatch error, got %v", err)
	}
}

func TestDispatchDefaultsAlertThreshold(t *testing.T) {
	t.Parallel()
	d := NewDispatch(map[TaskKind]Handler{}, DispatchDeps{Store: &fakeCompleter{}})
	if d.deps.AlertThreshold == "" {
		t.Fatal("NewDispatch must default the alert threshold (Risky)")
	}
}
