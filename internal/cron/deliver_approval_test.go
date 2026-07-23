package cron

import (
	"context"
	"errors"
	"slices"
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
	return slices.Contains(ids, want)
}

// fakeEnsurer records EnsureApprovalPause calls and returns a per-task token (or an error).
type fakeEnsurer struct {
	tokenByTask map[string]string
	err         error
	calls       []struct{ taskID, convID, kind string }
}

func (f *fakeEnsurer) EnsureApprovalPause(_ context.Context, taskID, conversationID, kind string) (string, error) {
	f.calls = append(f.calls, struct{ taskID, convID, kind string }{taskID, conversationID, kind})
	if f.err != nil {
		return "", f.err
	}
	if f.tokenByTask != nil {
		return f.tokenByTask[taskID], nil
	}
	return "tok-" + taskID, nil
}

func (f *fakeEnsurer) calledFor(taskID string) bool {
	for _, c := range f.calls {
		if c.taskID == taskID {
			return true
		}
	}
	return false
}

// fakeApprovalChannel records DeliverApproval pushes with a scriptable tri-state per identity.
type fakeApprovalChannel struct {
	byIdentity map[string]struct {
		delivered bool
		err       error
	}
	calls []struct{ identityID, token, taskID, kind string }
}

func (f *fakeApprovalChannel) DeliverApproval(_ context.Context, identityID, token, taskID, kind string) (bool, error) {
	f.calls = append(f.calls, struct{ identityID, token, taskID, kind string }{identityID, token, taskID, kind})
	r := f.byIdentity[identityID]
	return r.delivered, r.err
}

func (f *fakeApprovalChannel) pushFor(identityID string) (string, bool) {
	for _, c := range f.calls {
		if c.identityID == identityID {
			return c.token, true
		}
	}
	return "", false
}

// The headline contract (Amendment #92 revised): every selected pending_approval task has its
// pause ENSURED on its origin conversation and pushed to the origin channel, AND is stamped —
// REGARDLESS of the push outcome (pushed / no-push-channel / channel-error). A WebUI-origin row
// (no push channel) is still ensured + stamped (it pulls the pause).
func TestSweepApprovalRemindersEnsuresPushesAndStampsRegardlessOfOutcome(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{tasks: []Task{
		{ID: "aaaaaaaa-1111-1111-1111-111111111111", Kind: "agent_job", IdentityID: "id-DELIV", OriginConversationID: "conv-A"},
		{ID: "bbbbbbbb-2222-2222-2222-222222222222", Kind: "reminder", IdentityID: "local", OriginConversationID: "conv-WEBUI"}, // WebUI-origin: no push channel
		{ID: "cccccccc-3333-3333-3333-333333333333", Kind: "agent_job", IdentityID: "id-ERR", OriginConversationID: "conv-C"},
	}}
	ensurer := &fakeEnsurer{tokenByTask: map[string]string{
		"aaaaaaaa-1111-1111-1111-111111111111": "tok-A",
		"bbbbbbbb-2222-2222-2222-222222222222": "tok-B",
		"cccccccc-3333-3333-3333-333333333333": "tok-C",
	}}
	channel := &fakeApprovalChannel{byIdentity: map[string]struct {
		delivered bool
		err       error
	}{
		"id-DELIV": {delivered: true},
		"local":    {delivered: false},                 // no push channel owns it (WebUI pulls)
		"id-ERR":   {err: errors.New("telegram down")}, // owns-but-failed
	}}

	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ApprovalPauseEnsurer: ensurer, ApprovalChannel: channel})
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}

	// ALL rows (incl. the WebUI-origin one) have their pause ensured on the origin conversation.
	for _, tc := range []struct{ taskID, conv string }{
		{"aaaaaaaa-1111-1111-1111-111111111111", "conv-A"},
		{"bbbbbbbb-2222-2222-2222-222222222222", "conv-WEBUI"},
		{"cccccccc-3333-3333-3333-333333333333", "conv-C"},
	} {
		if !ensurer.calledFor(tc.taskID) {
			t.Fatalf("task %s must have its pause ensured on %s", tc.taskID, tc.conv)
		}
	}
	// The push carries the ENSURED token for the delivered row.
	if tok, ok := channel.pushFor("id-DELIV"); !ok || tok != "tok-A" {
		t.Fatalf("push for id-DELIV token = %q ok=%v, want tok-A", tok, ok)
	}
	// EVERY row is stamped regardless of outcome (the throttle contract) — including the
	// channel-error row and the WebUI-origin no-push row.
	for _, id := range []string{
		"aaaaaaaa-1111-1111-1111-111111111111",
		"bbbbbbbb-2222-2222-2222-222222222222",
		"cccccccc-3333-3333-3333-333333333333",
	} {
		if !remindedContains(store.reminded, id) {
			t.Fatalf("task %s must be stamped MarkApprovalReminded regardless of outcome, got %v", id, store.reminded)
		}
	}
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

// TestSweepApprovalRemindersEnsureFailureStillStamps proves a failing EnsureApprovalPause does
// NOT push (no token) but STILL stamps the row (a permanently-failing ensure cannot spam the tick).
func TestSweepApprovalRemindersEnsureFailureStillStamps(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{tasks: []Task{
		{ID: "dddddddd-4444-4444-4444-444444444444", Kind: "agent_job", IdentityID: "id-X", OriginConversationID: "conv-X"},
	}}
	ensurer := &fakeEnsurer{err: errors.New("mint failed")}
	channel := &fakeApprovalChannel{}

	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ApprovalPauseEnsurer: ensurer, ApprovalChannel: channel})
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}
	if len(channel.calls) != 0 {
		t.Fatalf("a failed ensure must not push (no token), got %d pushes", len(channel.calls))
	}
	if !remindedContains(store.reminded, "dddddddd-4444-4444-4444-444444444444") {
		t.Fatalf("a failed ensure must still stamp the row, got %v", store.reminded)
	}
}

func TestSweepApprovalRemindersKillSwitchDisables(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "0") // <=0 disables
	store := &fakeApprovalReminderStore{tasks: []Task{{ID: "x", Kind: "agent_job", IdentityID: "id", OriginConversationID: "c"}}}
	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ApprovalPauseEnsurer: &fakeEnsurer{}, ApprovalChannel: &fakeApprovalChannel{}})
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}
	if len(store.listCalls) != 0 {
		t.Fatalf("kill switch must issue no list query, got %d", len(store.listCalls))
	}
}

func TestSweepApprovalRemindersNilSeamsAreNoop(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{tasks: []Task{{ID: "x", Kind: "agent_job", IdentityID: "id", OriginConversationID: "c"}}}
	// No ensurer / no channel → no-op (kill-switch parity when the roots don't wire them).
	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store})
	if err := d.sweepApprovalReminders(context.Background()); err != nil {
		t.Fatalf("sweepApprovalReminders: %v", err)
	}
	if len(store.listCalls) != 0 {
		t.Fatalf("nil seams must issue no list query, got %d", len(store.listCalls))
	}
}

func TestSweepApprovalRemindersStoreErrorPropagates(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC", "3600")
	store := &fakeApprovalReminderStore{listErr: errors.New("db down")}
	d := NewDispatch(nil, DispatchDeps{ApprovalReminderStore: store, ApprovalPauseEnsurer: &fakeEnsurer{}, ApprovalChannel: &fakeApprovalChannel{}})
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
