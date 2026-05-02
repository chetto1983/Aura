package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// conversationsSchemaSQL creates the conversations archive table used by
// internal/conversation.ArchiveStore. Kept here so the shared SQLite DB is
// fully migrated whenever a Store is opened, regardless of call order.
const conversationsSchemaSQL = `
CREATE TABLE IF NOT EXISTS conversations (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id           INTEGER NOT NULL,
  user_id           INTEGER NOT NULL,
  turn_index        INTEGER NOT NULL,
  role              TEXT    NOT NULL,
  content           TEXT    NOT NULL,
  tool_calls        TEXT,
  tool_call_id      TEXT,
  llm_calls         INTEGER NOT NULL DEFAULT 0,
  tool_calls_count  INTEGER NOT NULL DEFAULT 0,
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  tokens_in         INTEGER NOT NULL DEFAULT 0,
  tokens_out        INTEGER NOT NULL DEFAULT 0,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(chat_id, turn_index)
);
CREATE INDEX IF NOT EXISTS idx_conv_chat ON conversations(chat_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_conv_user ON conversations(user_id, created_at);
`

// schemaSQL bootstraps the scheduled_tasks table and its index. Idempotent;
// safe to run on every startup.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '',
    recipient_id    TEXT NOT NULL DEFAULT '',
    schedule_kind   TEXT NOT NULL,
    schedule_at     TEXT,
    schedule_daily  TEXT,
    next_run_at     TEXT NOT NULL,
    last_run_at     TEXT,
    last_error      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_due
    ON scheduled_tasks(status, next_run_at);
`

// Store wraps a *sql.DB with the SQL needed by the scheduler. The underlying
// DB is owned by the caller; Store does not Close it so the scheduler can
// share a connection with other Aura subsystems (e.g. search).
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the SQLite file at path and applies the
// scheduler schema. The caller is responsible for closing the returned
// Store.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open scheduler db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping scheduler db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// NewStoreWithDB wraps an existing *sql.DB so the scheduler can share a
// connection with other subsystems on the same DB file.
func NewStoreWithDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the underlying DB. Safe to call only when Store owns the DB
// (created via OpenStore). Callers using NewStoreWithDB must close their
// own DB.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("scheduler migrate: %w", err)
	}
	if _, err := s.db.Exec(conversationsSchemaSQL); err != nil {
		return fmt.Errorf("scheduler migrate conversations: %w", err)
	}
	return nil
}

// Upsert inserts a task or, if a task with the same name exists, updates
// the schedule + payload. Returns the resulting Task. Idempotent — used
// at startup to ensure system jobs (the nightly wiki maintenance) are
// always present without producing duplicates.
func (s *Store) Upsert(ctx context.Context, t *Task) (*Task, error) {
	if t.Name == "" {
		return nil, errors.New("scheduler: task name required")
	}
	if err := validateScheduleFields(t); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = StatusActive
	}

	scheduleAt := nullTimeFromTask(t)
	scheduleDaily := nullStringFromTask(t)
	lastRunAt := sql.NullString{}
	if !t.LastRunAt.IsZero() {
		lastRunAt = sql.NullString{String: t.LastRunAt.UTC().Format(time.RFC3339), Valid: true}
	}

	const q = `
		INSERT INTO scheduled_tasks
			(name, kind, payload, recipient_id, schedule_kind, schedule_at, schedule_daily,
			 next_run_at, last_run_at, last_error, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			kind            = excluded.kind,
			payload         = excluded.payload,
			recipient_id    = excluded.recipient_id,
			schedule_kind   = excluded.schedule_kind,
			schedule_at     = excluded.schedule_at,
			schedule_daily  = excluded.schedule_daily,
			next_run_at     = excluded.next_run_at,
			status          = excluded.status,
			updated_at      = excluded.updated_at
	`
	if _, err := s.db.ExecContext(ctx, q,
		t.Name, string(t.Kind), t.Payload, t.RecipientID, string(t.ScheduleKind),
		scheduleAt, scheduleDaily,
		t.NextRunAt.UTC().Format(time.RFC3339), lastRunAt, t.LastError,
		string(t.Status), t.CreatedAt.UTC().Format(time.RFC3339), t.UpdatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return nil, fmt.Errorf("scheduler upsert: %w", err)
	}
	return s.GetByName(ctx, t.Name)
}

// GetByName returns the task with the given name, or sql.ErrNoRows.
func (s *Store) GetByName(ctx context.Context, name string) (*Task, error) {
	const q = `SELECT ` + selectColumns + ` FROM scheduled_tasks WHERE name = ?`
	row := s.db.QueryRowContext(ctx, q, name)
	return scanTask(row)
}

// List returns all tasks, optionally filtered to a single status. Sorted
// by next_run_at ascending so the LLM sees the next-up task first.
func (s *Store) List(ctx context.Context, statusFilter Status) ([]*Task, error) {
	q := `SELECT ` + selectColumns + ` FROM scheduled_tasks`
	args := []any{}
	if statusFilter != "" {
		q += ` WHERE status = ?`
		args = append(args, string(statusFilter))
	}
	q += ` ORDER BY next_run_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("scheduler list: %w", err)
	}
	defer rows.Close()

	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DueTasks returns active tasks whose next_run_at is at or before now.
// Used by the tick loop to find what to fire.
func (s *Store) DueTasks(ctx context.Context, now time.Time) ([]*Task, error) {
	const q = `
		SELECT ` + selectColumns + `
		FROM scheduled_tasks
		WHERE status = ? AND next_run_at <= ?
		ORDER BY next_run_at ASC
	`
	rows, err := s.db.QueryContext(ctx, q, string(StatusActive), now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("scheduler due tasks: %w", err)
	}
	defer rows.Close()

	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkFired updates the task's last_run_at, next_run_at, status, and
// last_error after a fire attempt. The caller computes next_run_at and
// status from the task's schedule; this method only persists.
func (s *Store) MarkFired(ctx context.Context, id int64, lastRun, nextRun time.Time, status Status, lastErr string) error {
	const q = `
		UPDATE scheduled_tasks
		SET last_run_at = ?, next_run_at = ?, status = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, q,
		lastRun.UTC().Format(time.RFC3339),
		nextRun.UTC().Format(time.RFC3339),
		string(status),
		lastErr,
		time.Now().UTC().Format(time.RFC3339),
		id,
	)
	if err != nil {
		return fmt.Errorf("scheduler mark fired: %w", err)
	}
	return nil
}

// Cancel flips a task to status='cancelled' so the tick loop ignores it.
// Returns false if no task with that name exists.
func (s *Store) Cancel(ctx context.Context, name string) (bool, error) {
	const q = `
		UPDATE scheduled_tasks
		SET status = ?, updated_at = ?
		WHERE name = ? AND status = ?
	`
	res, err := s.db.ExecContext(ctx, q,
		string(StatusCancelled), time.Now().UTC().Format(time.RFC3339),
		name, string(StatusActive),
	)
	if err != nil {
		return false, fmt.Errorf("scheduler cancel: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Delete removes a task row. Used by tests for isolation; production
// callers should prefer Cancel so audit history is preserved.
func (s *Store) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("scheduler delete: %w", err)
	}
	return nil
}

// validateScheduleFields enforces the at/daily exclusivity invariant.
func validateScheduleFields(t *Task) error {
	switch t.ScheduleKind {
	case ScheduleAt:
		if t.ScheduleAt.IsZero() {
			return errors.New("scheduler: schedule_at required when ScheduleKind=at")
		}
		if t.ScheduleDaily != "" {
			return errors.New("scheduler: schedule_daily must be empty when ScheduleKind=at")
		}
	case ScheduleDaily:
		if t.ScheduleDaily == "" {
			return errors.New("scheduler: schedule_daily required when ScheduleKind=daily")
		}
		if !t.ScheduleAt.IsZero() {
			return errors.New("scheduler: schedule_at must be empty when ScheduleKind=daily")
		}
		if _, _, err := ParseDailyTime(t.ScheduleDaily); err != nil {
			return err
		}
	default:
		return fmt.Errorf("scheduler: unknown schedule_kind %q", t.ScheduleKind)
	}
	return nil
}

func nullTimeFromTask(t *Task) sql.NullString {
	if t.ScheduleKind != ScheduleAt || t.ScheduleAt.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: t.ScheduleAt.UTC().Format(time.RFC3339), Valid: true}
}

func nullStringFromTask(t *Task) sql.NullString {
	if t.ScheduleKind != ScheduleDaily || t.ScheduleDaily == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: t.ScheduleDaily, Valid: true}
}

const selectColumns = `id, name, kind, payload, recipient_id, schedule_kind,
	schedule_at, schedule_daily, next_run_at, last_run_at, last_error, status,
	created_at, updated_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows so scanTask can
// be reused from QueryRow and Query call sites.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(r rowScanner) (*Task, error) {
	var (
		t              Task
		scheduleAt     sql.NullString
		scheduleDaily  sql.NullString
		lastRunAt      sql.NullString
		nextRun        string
		createdAt      string
		updatedAt      string
		kindRaw        string
		scheduleKind   string
		statusRaw      string
	)
	if err := r.Scan(
		&t.ID, &t.Name, &kindRaw, &t.Payload, &t.RecipientID, &scheduleKind,
		&scheduleAt, &scheduleDaily,
		&nextRun, &lastRunAt, &t.LastError, &statusRaw,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	t.Kind = TaskKind(kindRaw)
	t.ScheduleKind = ScheduleKind(scheduleKind)
	t.Status = Status(statusRaw)
	if scheduleAt.Valid {
		ts, err := time.Parse(time.RFC3339, scheduleAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse schedule_at: %w", err)
		}
		t.ScheduleAt = ts.UTC()
	}
	if scheduleDaily.Valid {
		t.ScheduleDaily = scheduleDaily.String
	}
	if lastRunAt.Valid {
		ts, err := time.Parse(time.RFC3339, lastRunAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse last_run_at: %w", err)
		}
		t.LastRunAt = ts.UTC()
	}
	nr, err := time.Parse(time.RFC3339, nextRun)
	if err != nil {
		return nil, fmt.Errorf("parse next_run_at: %w", err)
	}
	t.NextRunAt = nr.UTC()
	ca, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	t.CreatedAt = ca.UTC()
	ua, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	t.UpdatedAt = ua.UTC()
	return &t, nil
}

// ParseDailyTime parses a "HH:MM" daily schedule into hour and minute.
// Strict: must be exactly two digits each, hour 0-23, minute 0-59.
func ParseDailyTime(s string) (hour, minute int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, fmt.Errorf("scheduler: daily schedule must be HH:MM, got %q", s)
	}
	if _, err := fmt.Sscanf(parts[0], "%d", &hour); err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("scheduler: invalid hour in %q", s)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minute); err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("scheduler: invalid minute in %q", s)
	}
	return hour, minute, nil
}

// NextDailyRun computes the next UTC instant that matches HH:MM in loc,
// strictly after `after`. Used both at task creation (to set initial
// next_run_at) and after firing (to advance).
func NextDailyRun(daily string, loc *time.Location, after time.Time) (time.Time, error) {
	hour, minute, err := ParseDailyTime(daily)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	afterLocal := after.In(loc)
	candidate := time.Date(afterLocal.Year(), afterLocal.Month(), afterLocal.Day(), hour, minute, 0, 0, loc)
	if !candidate.After(afterLocal) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC(), nil
}
