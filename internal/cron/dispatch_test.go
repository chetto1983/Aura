package cron

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// fakeHandler is a scripted cron.Handler for routing/notify tests.
type fakeHandler struct {
	meta    HandlerMeta
	summary string
	err     error
	ran     bool
	gotJob  Job
	gotCtx  context.Context
}

func (h *fakeHandler) Meta() HandlerMeta {
	meta := h.meta
	if meta.MaxDuration <= 0 {
		meta.MaxDuration = time.Second
	}
	return meta
}

func (h *fakeHandler) Run(ctx context.Context, job Job) (string, error) {
	h.ran = true
	h.gotJob = job
	h.gotCtx = ctx
	return h.summary, h.err
}

func TestScheduledOperationIsStablePerClaimedRun(t *testing.T) {
	t.Parallel()

	identityID := "00000000-0000-0000-0000-000000000002"
	task := Task{ID: "task-1", Kind: KindReminder, IdentityID: identityID, Payload: []byte(`{"text":"buy milk"}`)}
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "ok"}
	d := NewDispatch(map[TaskKind]Handler{KindReminder: h}, DispatchDeps{Store: &fakeCompleter{}})

	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	first, ok := idempotency.OperationFromContext(h.gotCtx)
	if !ok {
		t.Fatal("handler context lacks scheduled operation")
	}
	if first.Key.IdentityID != identityID || first.Key.Scope != idempotency.ScopeSchedulerRun || identityctx.IdentityID(h.gotCtx) != identityID {
		t.Fatalf("first operation has wrong trusted metadata: %+v", first)
	}

	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("retry Dispatch: %v", err)
	}
	retry, _ := idempotency.OperationFromContext(h.gotCtx)
	if retry != first {
		t.Fatalf("same claimed run changed operation: %+v != %+v", retry, first)
	}

	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-2"}); err != nil {
		t.Fatalf("later Dispatch: %v", err)
	}
	later, _ := idempotency.OperationFromContext(h.gotCtx)
	if later.Key.Key == first.Key.Key || later.Fingerprint == first.Fingerprint {
		t.Fatalf("distinct claimed run reused operation: first=%+v later=%+v", first, later)
	}
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

// TestDispatchSilentSuccessKindSuppressesRoutineNotification pins the fix for the
// Telegram housekeeping flood: a system-seeded sweep (identity_purge / sandbox_reap /
// skill_ttl_sweep) that succeeds must STILL write its audit summary to the run ledger
// (CompleteRun) but must NOT push a routine "ok" notification to the user's channel.
func TestDispatchSilentSuccessKindSuppressesRoutineNotification(t *testing.T) {
	t.Parallel()
	for _, kind := range []TaskKind{KindIdentityPurge, KindSandboxReap, KindSkillTTLSweep} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			h := &fakeHandler{meta: HandlerMeta{Kind: kind}, summary: "purged 0 expired identit(y/ies)"}
			comp := &fakeCompleter{}
			notif := &captureNotifier{}
			d, c := newDispatchFor(t, h, kind, DispatchDeps{Store: comp, Notifier: notif})

			if err := d.Dispatch(context.Background(), Task{ID: "sys", Kind: kind}, c); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			// The audit ledger still records the run + summary — only the user-facing push is dropped.
			if len(comp.calls) != 1 || comp.calls[0].Status != "completed" || comp.calls[0].Summary == "" {
				t.Fatalf("the run must still be recorded completed with its summary, got %+v", comp.calls)
			}
			if len(notif.texts) != 0 {
				t.Fatalf("a routine housekeeping success must NOT notify the user, got %v", notif.texts)
			}
		})
	}
}

// TestDispatchSilentSuccessKindStillNotifiesOnFailure guards D-21: the suppression is
// scoped to routine SUCCESS. A failed housekeeping sweep must still surface to the user.
func TestDispatchSilentSuccessKindStillNotifiesOnFailure(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindIdentityPurge}, err: errors.New("purger exploded")}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindIdentityPurge, DispatchDeps{Store: comp, Notifier: notif})

	if err := d.Dispatch(context.Background(), Task{ID: "sys-fail", Kind: KindIdentityPurge}, c); err == nil {
		t.Fatal("a handler error must propagate")
	}
	if len(notif.texts) != 1 || notif.texts[0] == "" {
		t.Fatalf("a FAILED housekeeping sweep must still notify (D-21), got %v", notif.texts)
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
	if store.sweepAttemptBound != defaultPendingNotificationAttemptBound || store.sweepLimit != pendingNotificationSweepLimit {
		t.Fatalf("sweep bounds = (%d,%d), want (%d,%d)", store.sweepAttemptBound, store.sweepLimit, defaultPendingNotificationAttemptBound, pendingNotificationSweepLimit)
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

func TestDispatchSweepNotificationsUsesRetryAttemptEnv(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS", "5")
	store := &fakeNotificationStore{}
	d := NewDispatch(nil, DispatchDeps{Store: store, Notifier: &captureNotifier{}})

	if err := d.sweepNotifications(context.Background()); err != nil {
		t.Fatalf("sweepNotifications: %v", err)
	}
	if store.sweepAttemptBound != 5 {
		t.Fatalf("sweep attempt bound = %d, want env override 5", store.sweepAttemptBound)
	}
}

// TestDispatchSweepNotificationsPrefersOriginChannel pins the Step-2 route-back: a
// swept row carrying a real identity_id snapshot delivers via the origin channel
// (keyed on the ROW's identity, not a live task) under the SAME gate as the live-task
// path; a NULL-identity legacy row falls back to Notifier.Notify with its route
// (byte-identical to pre-Phase-20). It asserts the DESTINATION of each branch.
func TestDispatchSweepNotificationsPrefersOriginChannel(t *testing.T) {
	t.Parallel()
	store := &fakeNotificationStore{sweepRows: []PendingNotification{
		// owned row: real identity, no explicit route → origin channel.
		{ID: "owned", IdentityID: "id-I", Body: "via channel"},
		// legacy row: NULL identity (empty) + a route → Notifier fallback.
		{ID: "legacy", NotifyRoute: "whatsapp", Body: "via route"},
		// owns-but-fails: real identity, channel errors → keep for next sweep, no Notifier.
		{ID: "failing", IdentityID: "id-F", Body: "channel down"},
	}}
	notif := &captureNotifier{}

	// Script the channel: deliver "id-I", error on "id-F".
	scripted := &scriptedDeliverer{byIdentity: map[string]struct {
		delivered bool
		err       error
	}{
		"id-I": {delivered: true},
		"id-F": {err: errors.New("telegram down")},
	}}

	d := NewDispatch(nil, DispatchDeps{
		Store:               store,
		Notifier:            notif,
		ChannelDeliverer:    scripted,
		PreferOriginChannel: true,
	})
	if err := d.sweepNotifications(context.Background()); err != nil {
		t.Fatalf("sweepNotifications: %v", err)
	}

	// owned → channel delivered, Notifier NOT called for it, row marked delivered.
	if !scripted.calledFor("id-I") {
		t.Fatalf("owned row must be delivered via the origin channel keyed on its identity")
	}
	if got := scripted.textFor("id-I"); got != "via channel" {
		t.Fatalf("channel delivered text %q, want the row body", got)
	}
	// legacy NULL-identity row → Notifier fallback with its route (regression guard).
	if len(notif.texts) != 1 || notif.texts[0] != "via route" {
		t.Fatalf("legacy NULL-identity row must fall back to Notifier with its route, got %v", notif.texts)
	}
	if len(notif.routes) != 1 || string(notif.routes[0]) != "whatsapp" {
		t.Fatalf("legacy fallback route = %v, want whatsapp", notif.routes)
	}
	// owns-but-fails → kept (marked failed), Notifier NOT called for it.
	if !scripted.calledFor("id-F") {
		t.Fatalf("failing row must attempt the origin channel keyed on its identity")
	}
	if !contains(store.delivered, "owned") {
		t.Fatalf("delivered rows = %v, want the owned row marked delivered", store.delivered)
	}
	if contains(store.delivered, "failing") {
		t.Fatalf("owns-but-failed row must NOT be marked delivered (kept for retry)")
	}
	if !failedContains(store.failed, "failing") {
		t.Fatalf("owns-but-failed row must be marked failed for the next sweep, got %+v", store.failed)
	}
}

// scriptedDeliverer returns a per-identity tri-state and records every call, so the
// sweep route-back test can drive multiple rows with different outcomes in one sweep.
type scriptedDeliverer struct {
	byIdentity map[string]struct {
		delivered bool
		err       error
	}
	calls []struct{ identityID, text string }
}

func (s *scriptedDeliverer) DeliverToIdentity(_ context.Context, identityID, text string) (bool, error) {
	s.calls = append(s.calls, struct{ identityID, text string }{identityID, text})
	r := s.byIdentity[identityID]
	return r.delivered, r.err
}

func (s *scriptedDeliverer) calledFor(id string) bool {
	for _, c := range s.calls {
		if c.identityID == id {
			return true
		}
	}
	return false
}

func (s *scriptedDeliverer) textFor(id string) string {
	for _, c := range s.calls {
		if c.identityID == id {
			return c.text
		}
	}
	return ""
}

func contains(xs []string, x string) bool {
	return slices.Contains(xs, x)
}

func failedContains(xs []struct {
	id  string
	err string
}, id string) bool {
	for _, v := range xs {
		if v.id == id {
			return true
		}
	}
	return false
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
