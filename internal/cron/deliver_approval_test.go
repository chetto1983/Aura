package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The tick wires the sweep by asserting this seam on the dispatcher (scheduler.go).
var _ approvalReminderSweeper = (*Dispatch)(nil)

// fakeApprovalReminderStore records the sweep's list/stamp calls without a pool.
type fakeApprovalReminderStore struct {
	tasks     []Task
	listErr   error
	remindErr error
	listCalls []struct {
		cutoff time.Time
		limit  int
	}
	reminded []string
}

func (f *fakeApprovalReminderStore) ListDueApprovalReminders(_ context.Context, cutoff time.Time, limit int) ([]Task, error) {
	f.listCalls = append(f.listCalls, struct {
		cutoff time.Time
		limit  int
	}{cutoff, limit})
	return f.tasks, f.listErr
}

func (f *fakeApprovalReminderStore) MarkApprovalReminded(_ context.Context, id string) error {
	f.reminded = append(f.reminded, id)
	return f.remindErr
}

func remindedContains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// The headline contract (Amendment #92 point 4): every selected pending_approval task is
// delivered to its origin channel AND stamped, REGARDLESS of the delivery outcome
// (delivered / no-channel / channel-error) — so a pending approval re-nudges at most once
// per cadence and a permanently-failing channel cannot spam the tick.
func TestSweepApprovalRemindersDeliversAndStampsRegardlessOfOutcome(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{tasks: []Task{
		{ID: "aaaaaaaa-1111-1111-1111-111111111111", Kind: "agent_job", IdentityID: "id-DELIV"},
		{ID: "bbbbbbbb-2222-2222-2222-222222222222", Kind: "reminder", IdentityID: "id-NOCHAN"},
		{ID: "cccccccc-3333-3333-3333-333333333333", Kind: "agent_job", IdentityID: "id-ERR"},
	}}
	scripted := &scriptedDeliverer{byIdentity: map[string]struct {
		delivered bool
		err       error
	}{
		"id-DELIV":  {delivered: true},
		"id-NOCHAN": {delivered: false},                 // no channel owns it
		"id-ERR":    {err: errors.New("telegram down")}, // owns-but-failed
	}}

	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ChannelDeliverer: scripted})
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}

	// All three attempt origin-channel delivery keyed on their identity.
	for _, id := range []string{"id-DELIV", "id-NOCHAN", "id-ERR"} {
		if !scripted.calledFor(id) {
			t.Fatalf("task with identity %q must attempt origin-channel delivery", id)
		}
	}
	// The nudge carries the kind + a short id, never the payload.
	var deliverText string
	for _, c := range scripted.calls {
		if c.identityID == "id-DELIV" {
			deliverText = c.text
		}
	}
	if !strings.Contains(deliverText, "agent_job") || !strings.Contains(deliverText, "aaaaaaaa") {
		t.Fatalf("reminder text = %q, want kind + short id", deliverText)
	}
	// EVERY row is stamped regardless of delivery outcome (the throttle contract).
	for _, id := range []string{
		"aaaaaaaa-1111-1111-1111-111111111111",
		"bbbbbbbb-2222-2222-2222-222222222222",
		"cccccccc-3333-3333-3333-333333333333",
	} {
		if !remindedContains(store.reminded, id) {
			t.Fatalf("task %s must be stamped MarkApprovalReminded regardless of outcome, got %v", id, store.reminded)
		}
	}
	// The cutoff is now - cadence (1h), so it is in the past.
	if len(store.listCalls) != 1 {
		t.Fatalf("expected exactly one list query, got %d", len(store.listCalls))
	}
	if got := store.listCalls[0].cutoff; !got.Before(time.Now()) {
		t.Fatalf("cutoff %v must be in the past (now - cadence)", got)
	}
	if store.listCalls[0].limit != approvalReminderSweepLimit {
		t.Fatalf("limit = %d, want %d", store.listCalls[0].limit, approvalReminderSweepLimit)
	}
}

func TestSweepApprovalRemindersKillSwitchDisables(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "0") // <=0 disables
	store := &fakeApprovalReminderStore{tasks: []Task{{ID: "x", Kind: "agent_job", IdentityID: "id"}}}
	scripted := &scriptedDeliverer{}
	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ChannelDeliverer: scripted})
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}
	if len(store.listCalls) != 0 {
		t.Fatalf("kill switch must issue no list query, got %d", len(store.listCalls))
	}
	if len(scripted.calls) != 0 {
		t.Fatalf("kill switch must deliver nothing, got %d", len(scripted.calls))
	}
}

func TestSweepApprovalRemindersNoDelivererIsNoop(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{tasks: []Task{{ID: "x", Kind: "agent_job", IdentityID: "id"}}}
	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store}) // no ChannelDeliverer
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}
	if len(store.listCalls) != 0 {
		t.Fatalf("a nil deliverer must issue no list query, got %d", len(store.listCalls))
	}
}

func TestSweepApprovalRemindersStoreErrorPropagates(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{listErr: errors.New("db down")}
	scripted := &scriptedDeliverer{}
	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ChannelDeliverer: scripted})
	if err := d.sweepApprovalReminders(context.Background()); err == nil {
		t.Fatal("a store list error must propagate so the tick logs it")
	}
}

func TestApprovalReminderInterval(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", 3600 * time.Second},    // unset/empty → default
		{"0", 0},                    // explicit disable
		{"-5", 0},                   // negative → disable
		{"120", 120 * time.Second},  // positive → verbatim
		{"abc", 3600 * time.Second}, // invalid → default
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", c.env)
			if got := approvalReminderInterval(); got != c.want {
				t.Fatalf("approvalReminderInterval() with %q = %v, want %v", c.env, got, c.want)
			}
		})
	}
}
