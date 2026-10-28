//go:build db_integration

package cron

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A task's Telegram recipients and a pending push's one account survive the store
// (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md, R1 and R4): what the
// tool named is what the dispatcher and the sweep read back, and a task that named none reads
// back nil, never an empty list the CHECK of 0145 would have refused. A row marked undeliverable
// leaves the sweep for good.
func TestTelegramRecipientsRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)
	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	const identity = "id-telegram-recipients"
	create := func(recipients []int64) Task {
		t.Helper()
		task, err := s.CreateTask(ctx, CreateTaskParams{
			Kind: KindReminder, Spec: spec, Payload: []byte(`{"text":"hydrate"}`),
			NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "telegram",
			IdentityID: identity, TelegramRecipients: recipients,
		})
		if err != nil {
			t.Fatalf("CreateTask(%v): %v", recipients, err)
		}
		t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
		return task
	}

	want := []int64{9_000_145_001, 9_000_145_002}
	named := create(want)
	unnamed := create(nil)
	for name, c := range map[string]struct {
		task Task
		want []int64
	}{"named": {named, want}, "unnamed": {unnamed, nil}} {
		if !slices.Equal(c.task.TelegramRecipients, c.want) {
			t.Errorf("%s: CreateTask returned recipients %v, want %v", name, c.task.TelegramRecipients, c.want)
		}
		got, err := s.GetTask(ctx, c.task.ID)
		if err != nil {
			t.Fatalf("%s: GetTask: %v", name, err)
		}
		if !slices.Equal(got.TelegramRecipients, c.want) || (c.want == nil) != (got.TelegramRecipients == nil) {
			t.Errorf("%s: GetTask read recipients %#v, want %#v", name, got.TelegramRecipients, c.want)
		}
	}

	run, err := s.InsertRun(ctx, named.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	for _, account := range []int64{want[1], 0} {
		inserted, err := s.InsertPendingNotification(ctx, InsertPendingNotificationParams{
			RunID: run.ID, NotifyRoute: "telegram", Body: "hydrate",
			NotifyAfter: time.Now().UTC().Add(-time.Second), Status: "pending",
			IdentityID: identity, TelegramUserID: account,
		})
		if err != nil {
			t.Fatalf("InsertPendingNotification(%d): %v", account, err)
		}
		if inserted.TelegramUserID != account {
			t.Errorf("InsertPendingNotification returned account %d, want %d", inserted.TelegramUserID, account)
		}
		rows, err := s.SweepDueNotifications(ctx, pendingNotificationAttemptBound(), pendingNotificationSweepLimit)
		if err != nil {
			t.Fatalf("SweepDueNotifications: %v", err)
		}
		i := slices.IndexFunc(rows, func(r PendingNotification) bool { return r.ID == inserted.ID })
		if i < 0 {
			t.Fatalf("the sweep did not return row %s (%d rows)", inserted.ID, len(rows))
		}
		if rows[i].TelegramUserID != account {
			t.Errorf("the sweep read account %d, want %d", rows[i].TelegramUserID, account)
		}
		if err := s.MarkNotificationUndeliverable(ctx, inserted.ID, "telegram account is no longer linked"); err != nil {
			t.Fatalf("MarkNotificationUndeliverable: %v", err)
		}
		var status, lastErr string
		if err := pool.QueryRow(ctx, `SELECT status, last_error FROM aura.pending_notifications WHERE id = $1`, inserted.ID).Scan(&status, &lastErr); err != nil {
			t.Fatalf("read row %s: %v", inserted.ID, err)
		}
		if status != "undeliverable" || lastErr != "telegram account is no longer linked" {
			t.Errorf("row %s reads (%s, %q), want it undeliverable with the reason", inserted.ID, status, lastErr)
		}
		rows, err = s.SweepDueNotifications(ctx, pendingNotificationAttemptBound(), pendingNotificationSweepLimit)
		if err != nil {
			t.Fatalf("SweepDueNotifications: %v", err)
		}
		if slices.ContainsFunc(rows, func(r PendingNotification) bool { return r.ID == inserted.ID }) {
			t.Errorf("the sweep still returns undeliverable row %s", inserted.ID)
		}
	}
}
