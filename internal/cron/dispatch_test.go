package cron

import (
	"context"
	"errors"
	"testing"
	"time"
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

type fakeNotificationStore struct {
	calls []CompleteRunParams

	inserted []InsertPendingNotificationParams

	sweepRows         []PendingNotification
	sweepAttemptBound int
	sweepLimit        int

	delivered []string
	failed    []struct {
		id  string
		err string
	}
}

func (s *fakeNotificationStore) CompleteRun(_ context.Context, p CompleteRunParams) error {
	s.calls = append(s.calls, p)
	return nil
}

func (s *fakeNotificationStore) InsertPendingNotification(_ context.Context, p InsertPendingNotificationParams) (PendingNotification, error) {
	s.inserted = append(s.inserted, p)
	return PendingNotification{ID: "pending-id", RunID: p.RunID, NotifyRoute: p.NotifyRoute, Body: p.Body, NotifyAfter: p.NotifyAfter, Attempts: p.Attempts, LastError: p.LastError, Status: p.Status}, nil
}

func (s *fakeNotificationStore) SweepDueNotifications(_ context.Context, attemptBound, limit int) ([]PendingNotification, error) {
	s.sweepAttemptBound = attemptBound
	s.sweepLimit = limit
	return s.sweepRows, nil
}

func (s *fakeNotificationStore) MarkNotificationDelivered(_ context.Context, id string) error {
	s.delivered = append(s.delivered, id)
	return nil
}

func (s *fakeNotificationStore) MarkNotificationFailed(_ context.Context, id, lastErr string) error {
	s.failed = append(s.failed, struct {
		id  string
		err string
	}{id: id, err: lastErr})
	return nil
}

// captureNotifier records every delivery.
type captureNotifier struct {
	routes []NotifyRoute
	texts  []string
	err    error
	errs   []error
}

func (n *captureNotifier) Notify(_ context.Context, route NotifyRoute, _ string, text string) error {
	n.routes = append(n.routes, route)
	n.texts = append(n.texts, text)
	if len(n.errs) > 0 {
		err := n.errs[0]
		n.errs = n.errs[1:]
		return err
	}
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

func TestDispatchQuietHoursPersistsPendingNotification(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, summary: "routine summary"}
	store := &fakeNotificationStore{}
	notif := &captureNotifier{}
	end := time.Date(2026, 6, 10, 7, 30, 0, 0, time.UTC)
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{
		Store:      store,
		Notifier:   notif,
		QuietHours: func(string) bool { return true },
		QuietHoursEnd: func(string) (time.Time, bool) {
			return end, true
		},
	})

	task := Task{ID: "t5b", Kind: KindAgentJob, Payload: []byte(`{"goal":"summarize the news"}`), NotifyRoute: "whatsapp"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 0 {
		t.Fatalf("quiet-hours defer must not notify immediately, got %v", notif.texts)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("quiet-hours defer must persist one pending notification, got %+v", store.inserted)
	}
	got := store.inserted[0]
	if got.Status != "pending" || got.RunID != "run-1" || got.NotifyRoute != "whatsapp" || got.Body != "routine summary" {
		t.Fatalf("pending notification mismatch: %+v", got)
	}
	if !got.NotifyAfter.Equal(end) {
		t.Fatalf("notify_after = %s, want quiet-hours end %s", got.NotifyAfter, end)
	}
}

func TestDispatchNotifyFailurePersistsFailedNotification(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, summary: "routine summary"}
	store := &fakeNotificationStore{}
	notif := &captureNotifier{err: errors.New("mcp send failed")}
	d, c := newDispatchFor(t, h, KindAgentJob, DispatchDeps{Store: store, Notifier: notif})

	task := Task{ID: "t5c", Kind: KindAgentJob, Payload: []byte(`{"goal":"summarize the news"}`), NotifyRoute: "email"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("failed notification must be persisted, got %+v", store.inserted)
	}
	got := store.inserted[0]
	if got.Status != "failed" || got.Attempts != 0 || got.NotifyRoute != "email" || got.Body != "routine summary" {
		t.Fatalf("failed notification mismatch: %+v", got)
	}
	if got.LastError != "mcp send failed" {
		t.Fatalf("last_error = %q, want notify error", got.LastError)
	}
}

func TestDispatchSweepNotificationsMarksDeliveredAndFailed(t *testing.T) {
	t.Parallel()
	store := &fakeNotificationStore{sweepRows: []PendingNotification{
		{ID: "n1", NotifyRoute: "whatsapp", Body: "delivered"},
		{ID: "n2", NotifyRoute: "email", Body: "retry later"},
	}}
	notif := &captureNotifier{errs: []error{nil, errors.New("still down")}}
	d := NewDispatch(nil, DispatchDeps{Store: store, Notifier: notif})

	if err := d.sweepNotifications(context.Background()); err != nil {
		t.Fatalf("sweepNotifications: %v", err)
	}
	if store.sweepAttemptBound != pendingNotificationAttemptBound || store.sweepLimit != pendingNotificationSweepLimit {
		t.Fatalf("sweep bounds = (%d,%d), want (%d,%d)", store.sweepAttemptBound, store.sweepLimit, pendingNotificationAttemptBound, pendingNotificationSweepLimit)
	}
	if len(notif.texts) != 2 || notif.texts[0] != "delivered" || notif.texts[1] != "retry later" {
		t.Fatalf("sweep must attempt each notification, got %v", notif.texts)
	}
	if len(store.delivered) != 1 || store.delivered[0] != "n1" {
		t.Fatalf("successful sweep delivery must mark delivered, got %v", store.delivered)
	}
	if len(store.failed) != 1 || store.failed[0].id != "n2" || store.failed[0].err != "still down" {
		t.Fatalf("failed sweep delivery must mark failed with error, got %+v", store.failed)
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
