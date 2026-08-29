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
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertRunOnConn opens a running agent_job_runs row using the supplied held conn,
// so the INSERT shares the advisory-lock session (claim.go). It mirrors InsertRun
// but binds the generated query to conn instead of the pool.
func (s *Store) insertRunOnConn(ctx context.Context, conn *pgxpool.Conn, taskID string, stepBudget int) (Run, error) {
	tu, err := db.ParseUUID("uuid", taskID)
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
	u, err := db.ParseUUID("uuid", id)
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

// defaultRunHistoryLimit bounds an unspecified ListRunsForTask page (GOV-03 board
// pagination). A non-positive limit falls back to this; the handler may pass its own.
const defaultRunHistoryLimit = 25

// ListRunsForTask returns the run history for one task, newest-first (started_at
// DESC), honoring limit/offset for the GOV-03 paginated board. A non-positive limit
// falls back to defaultRunHistoryLimit; both limit and offset are clamped to a safe
// int32 range so an out-of-range page never wraps negative (parity with DueTasks /
// SweepDueNotifications, CodeQL go/incorrect-integer-conversion). It mutates nothing.
func (s *Store) ListRunsForTask(ctx context.Context, taskID string, limit, offset int) ([]Run, error) {
	tu, err := db.ParseUUID("uuid", taskID)
	if err != nil {
		return nil, fmt.Errorf("list runs for task: %w", err)
	}
	lim := int32(defaultRunHistoryLimit)
	if limit > 0 && limit <= math.MaxInt32 {
		lim = int32(limit)
	}
	var off int32
	if offset > 0 && offset <= math.MaxInt32 {
		off = int32(offset)
	}
	rows, err := s.q.ListRunsForTask(ctx, sqlc.ListRunsForTaskParams{TaskID: tu, Limit: lim, Offset: off})
	if err != nil {
		return nil, fmt.Errorf("list runs for task %q: %w", taskID, err)
	}
	out := make([]Run, 0, len(rows))
	for _, r := range rows {
		out = append(out, runFromRow(r))
	}
	return out, nil
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
	var lim int32 = 1
	if limit > 0 && limit <= math.MaxInt32 {
		lim = int32(limit)
	}
	rows, err := s.q.DueTasks(ctx, lim)
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
	// IdentityID is the stable owning-identity snapshot (Phase 20 R6/Fork 1): the
	// channel-independent route-back key for the deferred/failed sweep.
	IdentityID string
	// SteerQueueID is the migration-0105 sibling of RunID: set instead of RunID
	// when the row's owner is an aura.steer_queue row being retried (plan 51-10's
	// delegation nudge sweep), which has no aura.agent_job_runs row. "" for every
	// scheduler-originated row (unchanged).
	SteerQueueID string
	// OriginConversationID is recovered from the durable owner during a sweep:
	// scheduler run -> task origin, or steer row -> owning conversation. It is
	// never inferred from IdentityID.
	OriginConversationID string
}

// InsertPendingNotificationParams carries one durable notification queue write.
// Exactly one of RunID / SteerQueueID must be set — mirrors the DB-level
// pending_notifications_owner_chk CHECK (migration 0105) so a wiring bug fails loud in
// Go before the round trip, not as an opaque constraint-violation error.
type InsertPendingNotificationParams struct {
	// RunID is the owning aura.agent_job_runs row (the scheduler's own callers,
	// unchanged). Leave empty when SteerQueueID is set instead.
	RunID string
	// SteerQueueID is the owning aura.steer_queue row (plan 51-10's delegation
	// nudge sweep, owns-but-failed leg). Leave empty when RunID is set instead.
	SteerQueueID string
	NotifyRoute  string
	Body         string
	NotifyAfter  time.Time
	Attempts     int
	LastError    string
	Status       string
	// IdentityID snapshots the owning identity so the sweep can route explicitly.
	IdentityID string
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
// later scheduler sweep: either a quiet-hours defer, a failed MCP self-send, or (plan
// 51-10) a delegation nudge's owns-but-failed leg. Exactly one of RunID /
// SteerQueueID must be set — mirrored in Go ahead of the DB-level
// pending_notifications_owner_chk CHECK (migration 0105) so a wiring bug returns a
// named Go error rather than an opaque constraint violation.
func (s *Store) InsertPendingNotification(ctx context.Context, p InsertPendingNotificationParams) (PendingNotification, error) {
	if (p.RunID == "") == (p.SteerQueueID == "") {
		return PendingNotification{}, fmt.Errorf("insert pending notification: exactly one of RunID / SteerQueueID must be set")
	}
	if !ValidNotifyRoute(p.NotifyRoute) {
		return PendingNotification{}, fmt.Errorf("insert pending notification: notify route %q is not explicit", p.NotifyRoute)
	}
	if strings.TrimSpace(p.IdentityID) == "" {
		return PendingNotification{}, fmt.Errorf("insert pending notification: identity is required")
	}
	var runID, steerQueueID pgtype.UUID
	if p.RunID != "" {
		var err error
		runID, err = db.ParseUUID("uuid", p.RunID)
		if err != nil {
			return PendingNotification{}, fmt.Errorf("insert pending notification: %w", err)
		}
	}
	if p.SteerQueueID != "" {
		var err error
		steerQueueID, err = db.ParseUUID("steer_queue_id", p.SteerQueueID)
		if err != nil {
			return PendingNotification{}, fmt.Errorf("insert pending notification: %w", err)
		}
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
		ID:           newUUID(),
		RunID:        runID,
		SteerQueueID: steerQueueID,
		NotifyRoute:  p.NotifyRoute,
		Body:         p.Body,
		NotifyAfter:  tsOrNull(notifyAfter),
		Attempts:     int32(p.Attempts),
		LastError:    text(p.LastError),
		Status:       status,
		IdentityID:   p.IdentityID,
	})
	if err != nil {
		return PendingNotification{}, fmt.Errorf("insert pending notification for run %q: %w", p.RunID, err)
	}
	return pendingNotificationFromRow(row), nil
}

// SweepDueNotifications selects a bounded batch of due/retryable notifications in
// a real transaction so FOR UPDATE SKIP LOCKED has effect across concurrent ticks.
func (s *Store) SweepDueNotifications(ctx context.Context, attemptBound, limit int) ([]PendingNotification, error) {
	// Convert inside the proven-safe branch so each int32 narrowing is guarded at the
	// conversion site (CodeQL go/incorrect-integer-conversion, mirroring DueTasks); a
	// non-positive or overflowing bound floors to 1 rather than wrapping negative.
	var attempts int32 = 1
	if attemptBound > 0 && attemptBound <= math.MaxInt32 {
		attempts = int32(attemptBound)
	}
	var lim int32 = 1
	if limit > 0 && limit <= math.MaxInt32 {
		lim = int32(limit)
	}
	var rows []sqlc.SweepDueNotificationsRow
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		var err error
		rows, err = q.SweepDueNotifications(ctx, sqlc.SweepDueNotificationsParams{
			Attempts: attempts,
			Limit:    lim,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("sweep due notifications: %w", err)
	}
	out := make([]PendingNotification, 0, len(rows))
	for _, r := range rows {
		out = append(out, pendingNotificationFromSweepRow(r))
	}
	return out, nil
}

// MarkNotificationDelivered records a successful sweep delivery.
func (s *Store) MarkNotificationDelivered(ctx context.Context, id string) error {
	u, err := db.ParseUUID("uuid", id)
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
	u, err := db.ParseUUID("uuid", id)
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
	u, err := db.ParseUUID("uuid", runID)
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
		ID:           uuidString(r.ID),
		RunID:        uuidString(r.RunID),
		NotifyRoute:  r.NotifyRoute,
		Body:         r.Body,
		NotifyAfter:  r.NotifyAfter.Time,
		Attempts:     int(r.Attempts),
		LastError:    r.LastError.String,
		Status:       r.Status,
		CreatedAt:    r.CreatedAt.Time,
		UpdatedAt:    r.UpdatedAt.Time,
		IdentityID:   r.IdentityID,
		SteerQueueID: uuidString(r.SteerQueueID),
	}
}

func pendingNotificationFromSweepRow(r sqlc.SweepDueNotificationsRow) PendingNotification {
	return PendingNotification{
		ID:                   uuidString(r.ID),
		RunID:                uuidString(r.RunID),
		NotifyRoute:          r.NotifyRoute,
		Body:                 r.Body,
		NotifyAfter:          r.NotifyAfter.Time,
		Attempts:             int(r.Attempts),
		LastError:            r.LastError.String,
		Status:               r.Status,
		CreatedAt:            r.CreatedAt.Time,
		UpdatedAt:            r.UpdatedAt.Time,
		IdentityID:           r.IdentityID,
		SteerQueueID:         uuidString(r.SteerQueueID),
		OriginConversationID: r.OriginConversationID,
	}
}
