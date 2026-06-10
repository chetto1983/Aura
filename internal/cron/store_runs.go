package cron

// store_runs.go carries the run-row writers that operate on a specific HELD
// connection (claim.go's advisory-lock conn) rather than the pool, plus the
// recovery scan wrappers. Split out of store.go to keep each file ≤600 LOC
// (CLAUDE.md NO GOD CLASS) and to keep the held-conn concern co-located.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertRunOnConn opens a running agent_job_runs row using the supplied held conn,
// so the INSERT shares the advisory-lock session (claim.go). It mirrors InsertRun
// but binds the generated query to conn instead of the pool.
func (s *Store) insertRunOnConn(ctx context.Context, conn *pgxpool.Conn, taskID string, stepBudget int) (Run, error) {
	tu, err := parseUUID(taskID)
	if err != nil {
		return Run{}, fmt.Errorf("insert run on conn: %w", err)
	}
	q := sqlc.New(conn)
	row, err := q.InsertRun(ctx, sqlc.InsertRunParams{
		ID:         newUUID(),
		TaskID:     tu,
		StepBudget: int4OrNull(stepBudget),
	})
	if err != nil {
		return Run{}, fmt.Errorf("insert run on conn for task %q: %w", taskID, err)
	}
	return runFromRow(row), nil
}

// setMissedSinceOnConn stamps a catch-up run's missed_since on the held conn (the
// same advisory-lock session the run opened on), so the boot catch-up fire (D-18)
// records the ORIGINAL slipped instant in agent_job_runs — the forensics trail the
// MarkUnknownRecovery path already writes for the orphan case. A zero missedSince is
// a no-op (a normal tick run carries none). Parameterized SQL, never concatenated.
func (s *Store) setMissedSinceOnConn(ctx context.Context, conn *pgxpool.Conn, runID string, missedSince time.Time) error {
	if missedSince.IsZero() {
		return nil
	}
	if _, err := conn.Exec(ctx,
		`UPDATE aura.agent_job_runs SET missed_since = $2 WHERE id = $1`,
		runID, missedSince.UTC()); err != nil {
		return fmt.Errorf("set missed_since on run %q: %w", runID, err)
	}
	return nil
}

// GetRun fetches one run by id. A missing row is ErrTaskNotFound (wrapped) —
// reusing the package's not-found sentinel rather than a run-specific one.
func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	u, err := parseUUID(id)
	if err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	row, err := s.q.GetRun(ctx, u)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Run{}, fmt.Errorf("get run %q: %w", id, ErrTaskNotFound)
		}
		return Run{}, fmt.Errorf("get run %q: %w", id, err)
	}
	return runFromRow(row), nil
}

// DueTasks returns up to limit active tasks whose next_run_at has passed (the tick
// loop's batch pickup). It does NOT row-lock: the query runs on the autocommit pool,
// where a FOR UPDATE SKIP LOCKED would release the instant the SELECT returns (inert,
// L5). Concurrency correctness is held by the per-task pg_try_advisory_lock each worker
// takes in claim.go, which keeps a due task a singleton across workers for the run's
// lifetime. limit is the max-concurrent headroom. The limit is clamped at the store
// boundary (defensive parity with envInt/int4OrNull): a non-positive limit would yield
// LIMIT 0 (no task ever dispatched) and a value past 2^31 would wrap to a negative
// LIMIT (a Postgres error), so any out-of-range input is floored to 1 rather than
// silently misbehaving (WR-02).
func (s *Store) DueTasks(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 || limit > math.MaxInt32 {
		limit = 1
	}
	rows, err := s.q.DueTasks(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("due tasks: %w", err)
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, taskFromRow(r))
	}
	return out, nil
}

// StaleRun identifies a run whose heartbeat lapsed past the recovery window.
type StaleRun struct {
	RunID  string
	TaskID string
}

// PendingNotification is the domain projection of aura.pending_notifications.
type PendingNotification struct {
	ID          string
	RunID       string
	NotifyRoute string
	Body        string
	NotifyAfter time.Time
	Attempts    int
	LastError   string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// InsertPendingNotificationParams carries one durable notification queue write.
type InsertPendingNotificationParams struct {
	RunID       string
	NotifyRoute string
	Body        string
	NotifyAfter time.Time
	Attempts    int
	LastError   string
	Status      string
}

// ScanStaleRuns returns running rows whose last_heartbeat_at is older than
// staleSeconds — the boot orphan scan input (recover.go). A run still ticking its
// heartbeat is excluded by construction.
func (s *Store) ScanStaleRuns(ctx context.Context, staleSeconds float64) ([]StaleRun, error) {
	rows, err := s.q.ScanStaleRuns(ctx, staleSeconds)
	if err != nil {
		return nil, fmt.Errorf("scan stale runs: %w", err)
	}
	out := make([]StaleRun, 0, len(rows))
	for _, r := range rows {
		out = append(out, StaleRun{
			RunID:  uuidString(r.ID),
			TaskID: uuidString(r.TaskID),
		})
	}
	return out, nil
}

// InsertPendingNotification persists a notification that must be delivered by a
// later scheduler sweep: either a quiet-hours defer or a failed MCP self-send.
func (s *Store) InsertPendingNotification(ctx context.Context, p InsertPendingNotificationParams) (PendingNotification, error) {
	runID, err := parseUUID(p.RunID)
	if err != nil {
		return PendingNotification{}, fmt.Errorf("insert pending notification: %w", err)
	}
	status := p.Status
	if status == "" {
		status = "pending"
	}
	notifyAfter := p.NotifyAfter
	if notifyAfter.IsZero() {
		notifyAfter = time.Now().UTC()
	}
	row, err := s.q.InsertPendingNotification(ctx, sqlc.InsertPendingNotificationParams{
		ID:          newUUID(),
		RunID:       runID,
		NotifyRoute: text(p.NotifyRoute),
		Body:        p.Body,
		NotifyAfter: tsOrNull(notifyAfter),
		Attempts:    int32(p.Attempts),
		LastError:   text(p.LastError),
		Status:      status,
	})
	if err != nil {
		return PendingNotification{}, fmt.Errorf("insert pending notification for run %q: %w", p.RunID, err)
	}
	return pendingNotificationFromRow(row), nil
}

// SweepDueNotifications selects a bounded batch of due/retryable notifications in
// a real transaction so FOR UPDATE SKIP LOCKED has effect across concurrent ticks.
func (s *Store) SweepDueNotifications(ctx context.Context, attemptBound, limit int) ([]PendingNotification, error) {
	if attemptBound <= 0 || attemptBound > math.MaxInt32 {
		attemptBound = 1
	}
	if limit <= 0 || limit > math.MaxInt32 {
		limit = 1
	}
	var rows []sqlc.AuraPendingNotifications
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var err error
		rows, err = q.SweepDueNotifications(ctx, sqlc.SweepDueNotificationsParams{
			Attempts: int32(attemptBound),
			Limit:    int32(limit),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("sweep due notifications: %w", err)
	}
	out := make([]PendingNotification, 0, len(rows))
	for _, r := range rows {
		out = append(out, pendingNotificationFromRow(r))
	}
	return out, nil
}

// MarkNotificationDelivered records a successful sweep delivery.
func (s *Store) MarkNotificationDelivered(ctx context.Context, id string) error {
	u, err := parseUUID(id)
	if err != nil {
		return fmt.Errorf("mark notification delivered: %w", err)
	}
	if err := s.q.MarkNotificationDelivered(ctx, u); err != nil {
		return fmt.Errorf("mark notification delivered %q: %w", id, err)
	}
	return nil
}

// MarkNotificationFailed records an undelivered sweep attempt and increments the
// retry counter. Once attempts reaches the dispatcher bound, the sweep stops
// re-selecting the row.
func (s *Store) MarkNotificationFailed(ctx context.Context, id, lastErr string) error {
	u, err := parseUUID(id)
	if err != nil {
		return fmt.Errorf("mark notification failed: %w", err)
	}
	if err := s.q.MarkNotificationFailed(ctx, sqlc.MarkNotificationFailedParams{ID: u, LastError: text(lastErr)}); err != nil {
		return fmt.Errorf("mark notification failed %q: %w", id, err)
	}
	return nil
}

// MarkUnknownRecovery transitions a stale run to unknown_recovery (D-02 audit row):
// it stays in agent_job_runs forever (no DELETE grant) as the repudiation trail for
// a run whose worker died mid-flight.
func (s *Store) MarkUnknownRecovery(ctx context.Context, runID string) error {
	u, err := parseUUID(runID)
	if err != nil {
		return fmt.Errorf("mark unknown_recovery: %w", err)
	}
	if err := s.q.MarkUnknownRecovery(ctx, u); err != nil {
		return fmt.Errorf("mark unknown_recovery run %q: %w", runID, err)
	}
	return nil
}

func pendingNotificationFromRow(r sqlc.AuraPendingNotifications) PendingNotification {
	return PendingNotification{
		ID:          uuidString(r.ID),
		RunID:       uuidString(r.RunID),
		NotifyRoute: r.NotifyRoute.String,
		Body:        r.Body,
		NotifyAfter: r.NotifyAfter.Time,
		Attempts:    int(r.Attempts),
		LastError:   r.LastError.String,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt.Time,
		UpdatedAt:   r.UpdatedAt.Time,
	}
}
