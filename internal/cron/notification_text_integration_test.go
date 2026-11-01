//go:build db_integration

package cron

import (
	"context"
	"testing"
	"time"
)

// The rows stay as a trail until their run goes, and a mail watch's body names a sender and a
// subject, which this table may hold only while the push waits (spec
// 2026-10-31-mail-calendar-watch-design.md §5.2). Run for real, two delivered rows still held both.
func TestANotificationHoldsItsTextOnlyWhileItWaits(t *testing.T) {
	bound := pendingNotificationAttemptBound()
	for name, tc := range map[string]struct {
		status   string
		attempts int
		settle   func(context.Context, *Store, string) error
		want     string
	}{
		"delivered": {status: "pending", settle: func(ctx context.Context, s *Store, id string) error {
			return s.MarkNotificationDelivered(ctx, id)
		}},
		"undeliverable": {status: "pending", settle: func(ctx context.Context, s *Store, id string) error {
			return s.MarkNotificationUndeliverable(ctx, id, "bot blocked")
		}},
		"failed on its last retry": {status: "failed", attempts: bound - 1, settle: func(ctx context.Context, s *Store, id string) error {
			return s.MarkNotificationFailed(ctx, id, "telegram unreachable")
		}},
		"failed with retries left": {status: "failed", attempts: bound - 2, want: "one-shot", settle: func(ctx context.Context, s *Store, id string) error {
			return s.MarkNotificationFailed(ctx, id, "telegram unreachable")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			pool := migratedPool(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			s := New(pool)
			_, runID := fireOneShot(t, ctx, s, "failed")
			id := owesNotification(t, ctx, s, runID, tc.status, tc.attempts)

			if err := tc.settle(ctx, s, id); err != nil {
				t.Fatalf("settle: %v", err)
			}
			var body string
			if err := pool.QueryRow(ctx, `SELECT body FROM aura.pending_notifications WHERE id = $1`, id).Scan(&body); err != nil {
				t.Fatalf("read body: %v", err)
			}
			if body != tc.want {
				t.Fatalf("body = %q, want %q", body, tc.want)
			}
		})
	}
}
