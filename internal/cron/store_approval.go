package cron

// store_approval.go — the durable store methods for the pending-approval reminder sweep
// (fix-plan 1.7 / Amendment #92). Split from store.go (concern + 600-LOC cap).

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// ListDueApprovalReminders returns channel-owned pending_approval tasks whose throttle
// stamp is due — never reminded, or reminded at/before cutoff (= now - cadence). The
// SQL excludes CLI/cockpit-local identities and locks the rows FOR UPDATE SKIP LOCKED so
// concurrent ticks never double-nudge the same task.
func (s *Store) ListDueApprovalReminders(ctx context.Context, cutoff time.Time, limit int) ([]Task, error) {
	rows, err := s.q.ListDuePendingApprovalReminders(ctx, sqlc.ListDuePendingApprovalRemindersParams{
		ApprovalRemindedAt: tsOrNull(cutoff),
		Limit:              int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list due approval reminders: %w", err)
	}
	tasks := make([]Task, len(rows))
	for i, r := range rows {
		tasks[i] = taskFromRow(r)
	}
	return tasks, nil
}

// MarkApprovalReminded stamps approval_reminded_at=now() so a pending approval re-nudges
// at most once per cadence. Stamped after EVERY attempt (delivered or not) — Amendment
// #92 point 4 — so a permanently-failing channel cannot spam the tick.
func (s *Store) MarkApprovalReminded(ctx context.Context, id string) error {
	u, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.MarkApprovalReminded(ctx, u); err != nil {
		return fmt.Errorf("mark approval reminded %q: %w", id, err)
	}
	return nil
}
