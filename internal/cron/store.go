package cron

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

// Store wraps a *sql.DB with the SQL needed by the scheduler. OpenStore-created
// stores own their DB handle; NewStoreWithDB-created stores share a caller-owned
// pool with other Aura subsystems.
type Store struct {
	db    *sql.DB
	owned bool
}

// TaskReader is the read side used by dashboards, LLM tools, and wake guards.
type TaskReader interface {
	GetByName(ctx context.Context, name string) (*Task, error)
	List(ctx context.Context, statusFilter Status) ([]*Task, error)
}

// TaskWriter is the task mutation side used by dashboards and LLM tools.
type TaskWriter interface {
	Upsert(ctx context.Context, t *Task) (*Task, error)
	Cancel(ctx context.Context, name string) (bool, error)
	Delete(ctx context.Context, name string) error
}

// Repository is the ordinary task store boundary for read/write task flows.
type Repository interface {
	TaskReader
	TaskWriter
}

// RuntimeRepository is the narrower tick-loop persistence boundary. It is
// separate from Repository so future stores can keep scheduler execution
// internals out of ordinary dashboard/tool fakes.
type RuntimeRepository interface {
	DueTasks(ctx context.Context, now time.Time) ([]*Task, error)
	MarkFired(ctx context.Context, id int64, lastRun, nextRun time.Time, status Status, lastErr string) error
}

// ManualRunRecorder persists extra agent-job execution metadata without
// changing the task's schedule.
type ManualRunRecorder interface {
	RecordManualRun(ctx context.Context, id int64, lastRun time.Time, lastErr string) error
	RecordAgentJobResult(ctx context.Context, id int64, output, metricsJSON, wakeSignature string) error
}

// AgentJobRepository is the Telegram runtime surface for manual agent-job
// execution and wake-signature history.
type AgentJobRepository interface {
	TaskReader
	ManualRunRecorder
}

// OpenStore opens (or creates) the SQLite file at path and applies the
// scheduler schema. The caller is responsible for closing the returned
// Store.
func OpenStore(path string) (*Store, error) {
	db, err := auradb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scheduler db: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("scheduler migrate: %w", err)
	}
	return &Store{db: db, owned: true}, nil
}

// NewStoreWithDB wraps an existing *sql.DB so the scheduler can share a
// connection with other subsystems on the same DB file.
func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("scheduler: db required")
	}
	return &Store{db: db, owned: false}, nil
}

// Close closes the underlying DB. Safe to call only when Store owns the DB
// (created via OpenStore). Callers using NewStoreWithDB must close their
// own DB.
func (s *Store) Close() error {
	if s == nil || !s.owned {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying *sql.DB so other subsystems (e.g. the
// conversation archive) can share the same SQLite connection.
func (s *Store) DB() *sql.DB {
	return s.db
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
			 schedule_weekdays, schedule_every_minutes,
			 next_run_at, last_run_at, last_error, last_output, last_metrics_json,
			 wake_signature, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			kind                   = excluded.kind,
			payload                = excluded.payload,
			recipient_id           = excluded.recipient_id,
			schedule_kind          = excluded.schedule_kind,
			schedule_at            = excluded.schedule_at,
			schedule_daily         = excluded.schedule_daily,
			schedule_weekdays      = excluded.schedule_weekdays,
			schedule_every_minutes = excluded.schedule_every_minutes,
			next_run_at            = excluded.next_run_at,
			status                 = excluded.status,
			updated_at             = excluded.updated_at
	`
	if _, err := s.db.ExecContext(ctx, q,
		t.Name, string(t.Kind), t.Payload, t.RecipientID, string(t.ScheduleKind),
		scheduleAt, scheduleDaily, t.ScheduleWeekdays, t.ScheduleEveryMinutes,
		t.NextRunAt.UTC().Format(time.RFC3339), lastRunAt, t.LastError,
		t.LastOutput, t.LastMetricsJSON, t.WakeSignature,
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

// RecordManualRun records an explicit "run now" attempt without changing the
// task's schedule or lifecycle. Manual runs are extra executions; recurring
// tasks should keep their next_run_at exactly as scheduled.
func (s *Store) RecordManualRun(ctx context.Context, id int64, lastRun time.Time, lastErr string) error {
	const q = `
		UPDATE scheduled_tasks
		SET last_run_at = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, q,
		lastRun.UTC().Format(time.RFC3339),
		lastErr,
		time.Now().UTC().Format(time.RFC3339),
		id,
	)
	if err != nil {
		return fmt.Errorf("scheduler record manual run: %w", err)
	}
	return nil
}

func (s *Store) RecordAgentJobResult(ctx context.Context, id int64, output, metricsJSON, wakeSignature string) error {
	const q = `
		UPDATE scheduled_tasks
		SET last_output = ?, last_metrics_json = ?, wake_signature = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, q,
		output,
		metricsJSON,
		wakeSignature,
		time.Now().UTC().Format(time.RFC3339),
		id,
	)
	if err != nil {
		return fmt.Errorf("scheduler record agent job result: %w", err)
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
// minEveryMinutes caps interval-recurrence at 1 minute. Tighter bursts
// would race the tick loop; the bot is happy to run every minute on the
// nose. Operators can set higher values freely.
const minEveryMinutes = 1

func validateScheduleFields(t *Task) error {
	switch t.ScheduleKind {
	case ScheduleAt:
		if t.ScheduleAt.IsZero() {
			return errors.New("scheduler: schedule_at required when ScheduleKind=at")
		}
		if t.ScheduleDaily != "" {
			return errors.New("scheduler: schedule_daily must be empty when ScheduleKind=at")
		}
		if t.ScheduleEveryMinutes != 0 {
			return errors.New("scheduler: schedule_every_minutes must be 0 when ScheduleKind=at")
		}
		if t.ScheduleWeekdays != "" {
			return errors.New("scheduler: schedule_weekdays must be empty when ScheduleKind=at")
		}
	case ScheduleDaily:
		if t.ScheduleDaily == "" {
			return errors.New("scheduler: schedule_daily required when ScheduleKind=daily")
		}
		if !t.ScheduleAt.IsZero() {
			return errors.New("scheduler: schedule_at must be empty when ScheduleKind=daily")
		}
		if t.ScheduleEveryMinutes != 0 {
			return errors.New("scheduler: schedule_every_minutes must be 0 when ScheduleKind=daily")
		}
		if _, _, err := ParseDailyTime(t.ScheduleDaily); err != nil {
			return err
		}
		if t.ScheduleWeekdays != "" {
			normalized, err := NormalizeWeekdays(t.ScheduleWeekdays)
			if err != nil {
				return err
			}
			t.ScheduleWeekdays = normalized
		}
	case ScheduleEvery:
		if t.ScheduleEveryMinutes < minEveryMinutes {
			return fmt.Errorf("scheduler: schedule_every_minutes must be >= %d when ScheduleKind=every", minEveryMinutes)
		}
		if !t.ScheduleAt.IsZero() {
			return errors.New("scheduler: schedule_at must be empty when ScheduleKind=every")
		}
		if t.ScheduleDaily != "" {
			return errors.New("scheduler: schedule_daily must be empty when ScheduleKind=every")
		}
		if t.ScheduleWeekdays != "" {
			return errors.New("scheduler: schedule_weekdays must be empty when ScheduleKind=every")
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
	schedule_at, schedule_daily, schedule_weekdays, schedule_every_minutes,
	next_run_at, last_run_at, last_error, last_output, last_metrics_json,
	wake_signature, status,
	created_at, updated_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows so scanTask can
// be reused from QueryRow and Query call sites.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(r rowScanner) (*Task, error) {
	var (
		t             Task
		scheduleAt    sql.NullString
		scheduleDaily sql.NullString
		scheduleDays  string
		lastRunAt     sql.NullString
		nextRun       string
		createdAt     string
		updatedAt     string
		kindRaw       string
		scheduleKind  string
		statusRaw     string
	)
	if err := r.Scan(
		&t.ID, &t.Name, &kindRaw, &t.Payload, &t.RecipientID, &scheduleKind,
		&scheduleAt, &scheduleDaily, &scheduleDays, &t.ScheduleEveryMinutes,
		&nextRun, &lastRunAt, &t.LastError, &t.LastOutput, &t.LastMetricsJSON,
		&t.WakeSignature, &statusRaw,
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
	t.ScheduleWeekdays = scheduleDays
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
	return NextDailyRunOnWeekdays(daily, "", loc, after)
}

// NextDailyRunOnWeekdays computes the next daily run, optionally limited to
// selected weekdays. weekdays is a comma-separated list using mon,tue,wed,
// thu,fri,sat,sun; empty means every day.
func NextDailyRunOnWeekdays(daily, weekdays string, loc *time.Location, after time.Time) (time.Time, error) {
	hour, minute, err := ParseDailyTime(daily)
	if err != nil {
		return time.Time{}, err
	}
	allowed, err := parseWeekdaySet(weekdays)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	afterLocal := after.In(loc)
	candidate := time.Date(afterLocal.Year(), afterLocal.Month(), afterLocal.Day(), hour, minute, 0, 0, loc)
	if !candidate.After(afterLocal) || !weekdayAllowed(candidate.Weekday(), allowed) {
		candidate = candidate.AddDate(0, 0, 1)
		for i := 0; i < 7 && !weekdayAllowed(candidate.Weekday(), allowed); i++ {
			candidate = candidate.AddDate(0, 0, 1)
		}
	}
	return candidate.UTC(), nil
}

// NormalizeWeekdays validates and canonicalizes selected weekdays. It accepts
// compact day names plus common "weekdays/business/feriali" and "weekend"
// shortcuts. Empty returns empty.
func NormalizeWeekdays(in string) (string, error) {
	set, err := parseWeekdaySet(in)
	if err != nil {
		return "", err
	}
	if len(set) == 0 {
		return "", nil
	}
	ordered := []struct {
		day  time.Weekday
		name string
	}{
		{time.Monday, "mon"},
		{time.Tuesday, "tue"},
		{time.Wednesday, "wed"},
		{time.Thursday, "thu"},
		{time.Friday, "fri"},
		{time.Saturday, "sat"},
		{time.Sunday, "sun"},
	}
	out := make([]string, 0, len(set))
	for _, item := range ordered {
		if set[item.day] {
			out = append(out, item.name)
		}
	}
	return strings.Join(out, ","), nil
}

func parseWeekdaySet(in string) (map[time.Weekday]bool, error) {
	in = strings.TrimSpace(strings.ToLower(in))
	out := map[time.Weekday]bool{}
	if in == "" {
		return out, nil
	}
	parts := strings.FieldsFunc(in, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' '
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch part {
		case "weekday", "weekdays", "business", "business_days", "workdays", "feriali":
			out[time.Monday] = true
			out[time.Tuesday] = true
			out[time.Wednesday] = true
			out[time.Thursday] = true
			out[time.Friday] = true
		case "weekend", "weekends", "fine-settimana":
			out[time.Saturday] = true
			out[time.Sunday] = true
		case "mon", "monday", "lun", "lunedi", "lunedi'":
			out[time.Monday] = true
		case "tue", "tuesday", "mar", "martedi", "martedi'":
			out[time.Tuesday] = true
		case "wed", "wednesday", "mer", "mercoledi", "mercoledi'":
			out[time.Wednesday] = true
		case "thu", "thursday", "gio", "giovedi", "giovedi'":
			out[time.Thursday] = true
		case "fri", "friday", "ven", "venerdi", "venerdi'":
			out[time.Friday] = true
		case "sat", "saturday", "sab", "sabato":
			out[time.Saturday] = true
		case "sun", "sunday", "dom", "domenica":
			out[time.Sunday] = true
		default:
			return nil, fmt.Errorf("scheduler: invalid weekday %q", part)
		}
	}
	return out, nil
}

func weekdayAllowed(day time.Weekday, allowed map[time.Weekday]bool) bool {
	return len(allowed) == 0 || allowed[day]
}
