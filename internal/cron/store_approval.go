package cron

// store_approval.go — the durable store methods for the pending-approval reminder sweep
// (fix-plan 1.7 / Amendment #92). Split from store.go (concern + 600-LOC cap).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
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
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return err
	}
	if err := s.q.MarkApprovalReminded(ctx, u); err != nil {
		return fmt.Errorf("mark approval reminded %q: %w", id, err)
	}
	return nil
}

// ListLapsedApprovals lists the one-shots whose time passed while they waited for approval.
func (s *Store) ListLapsedApprovals(ctx context.Context, limit int) ([]Task, error) {
	rows, err := s.q.ListLapsedApprovals(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list lapsed approvals: %w", err)
	}
	tasks := make([]Task, len(rows))
	for i, r := range rows {
		tasks[i] = taskFromRow(r)
	}
	return tasks, nil
}

// ExpireLapsedApproval commits the cancellation, terminal run, origin turn and queued
// pushes together. A second sweep loses the task CAS before writing any of them.
func (s *Store) ExpireLapsedApproval(ctx context.Context, task Task, homeID string, notifyAfter time.Time, origin LapsedOriginRecorder) error {
	id, err := db.ParseUUID("task id", task.ID)
	if err != nil {
		return err
	}
	notice := lapsedApprovalText(task)
	runID := newUUID()
	err = db.WithIdentityTx(ctx, s.pool, operationIdentity(task.IdentityID), func(q *sqlc.Queries) error {
		row, err := q.ExpireLapsedApproval(ctx, sqlc.ExpireLapsedApprovalParams{
			TaskID: id, IdentityID: task.IdentityID, Deadline: tsOrNull(task.NextRunAt), RunID: runID, Notice: notice,
			OriginID: uuidOrNull(task.OriginConversationID), HomeID: uuidOrNull(homeID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || row.NotifyRoute == string(RouteNone) {
			return err
		}
		if row.OriginConversationID.Valid {
			if origin == nil {
				return errors.New("missing origin conversation recorder")
			}
			if err := origin.AppendAssistantTurnTx(ctx, q, row.OriginConversationID.String(), notice); err != nil {
				return err
			}
		}
		recipients := telegramRecipients(Task{NotifyRoute: row.NotifyRoute, TelegramRecipients: row.TelegramRecipients})
		if len(recipients) == 0 {
			recipients = []int64{0}
		}
		for _, recipient := range recipients {
			_, err := insertPendingNotification(ctx, q, InsertPendingNotificationParams{
				RunID: runID.String(), NotifyRoute: row.NotifyRoute, IdentityID: task.IdentityID,
				Body: notice, NotifyAfter: notifyAfter, TelegramUserID: recipient,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("expire lapsed approval %q: %w", task.ID, err)
	}
	return nil
}
