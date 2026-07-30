package cron

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors so callers classify failures without string-matching (D-04).
// ErrTaskNotFound is a missing scheduler_tasks lookup; ErrAlreadyRunning is the
// idempotency rejection when a duplicate completion hits the completed_with_hash
// UNIQUE constraint (the SC#2 swallow point).
var (
	ErrTaskNotFound   = errors.New("scheduler task not found")
	ErrAlreadyRunning = errors.New("agent job already running for this completion hash")
)

// TaskKind is the task category (D-28). One handler per kind in downstream waves.
type TaskKind string

// The task kinds; each gets a dedicated handler in downstream waves. skill_ttl_sweep
// (D-16) is the Phase-11 system-seeded TTL sweep — the 0010 migration widened the
// scheduler_tasks.kind CHECK to admit it. It is NOT model-schedulable (the task tool's
// kind enum is unchanged); only serve.go seeds it daily.
//
// KindIdentityPurge (Phase 36 D-27) is the grace-window soft-delete purge sweep — the
// 0033 migration widened the scheduler_tasks.kind CHECK to admit it. Like skill_ttl_sweep
// it is system-seeded (seedIdentityPurgeSweep), never model-schedulable. Its literal MUST
// equal handlers.KindIdentityPurge ("identity_purge") — the cron store writes the row, the
// dispatcher routes the same string to the handler handlers.NewIdentityPurgeHandler builds.
//
// KindSandboxReap (Phase 37 D-08) is the idle-suspend reaper sweep — the 0034 migration
// widened the scheduler_tasks.kind CHECK to admit it. Like identity_purge it is system-seeded
// (seedSandboxReapSweep, plan 37-05), never model-schedulable. Its literal MUST equal
// handlers.KindSandboxReap ("sandbox_reap") — the cron store writes the row, the dispatcher
// routes the same string to the handler handlers.NewSandboxReapHandler builds.
//
// KindShareExpirySweep (Phase 37F D-15/OQ3) is the share-link Garage-byte GC sweep — the 0040
// migration already widened the scheduler_tasks.kind CHECK to admit it. Like sandbox_reap it is
// system-seeded (seedShareExpirySweep), never model-schedulable. Its literal MUST equal
// handlers.KindShareExpirySweep ("share_expiry_sweep") — the cron store writes the row, the
// dispatcher routes the same string to the handler handlers.NewShareExpiryHandler builds.
const (
	KindReminder         TaskKind = "reminder"
	KindAgentJob         TaskKind = "agent_job"
	KindBackupPostgres   TaskKind = "backup_postgres"
	KindBackupNeo4j      TaskKind = "backup_neo4j"
	KindSkillTTLSweep    TaskKind = "skill_ttl_sweep"
	KindIdentityPurge    TaskKind = "identity_purge"
	KindSandboxReap      TaskKind = "sandbox_reap"
	KindShareExpirySweep TaskKind = "share_expiry_sweep"
	KindRetentionSweep   TaskKind = "retention_sweep"
)

// Store wraps a pgx pool and the generated Queries — the identity-04-02 canonical
// shape (D-A4-01): non-tx reads use s.q; atomic multi-statement writes wrap
// db.WithTx (CreateRunAndAdvance is the first place cron needs it).
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// Task is the domain projection of aura.scheduler_tasks — plain Go types at the
// package boundary instead of the sqlc pgtype wrappers.
type Task struct {
	ID                   string
	Kind                 TaskKind
	ScheduleKind         ScheduleKind
	CronExpr             string
	EveryMinutes         int
	RunAt                time.Time
	TZ                   string
	Payload              []byte
	StepBudget           int
	Status               string
	NextRunAt            time.Time
	NotifyRoute          string
	IdentityID           string
	OriginConversationID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Run is the domain projection of aura.agent_job_runs.
type Run struct {
	ID                string
	TaskID            string
	Status            string
	StepBudget        int
	StartedAt         time.Time
	LastHeartbeatAt   time.Time
	CompletedWithHash string
	Summary           string
	LastError         string
	MissedSince       time.Time
	PausedStateToken  string
	CompletedAt       time.Time
}

// CreateTaskParams carries the resolved fields for a new task. NextRunAt is the
// first fire computed by the caller via NextRunAt(spec, now). Status is the INITIAL
// status the row is created with; an empty Status defaults to "active". A caller
// that scoring routed to pending_approval sets Status here so the gate is written in
// the SAME INSERT — a destructive task is NEVER momentarily active (T-10-12/D-27).
type CreateTaskParams struct {
	Kind                 TaskKind
	Spec                 ScheduleSpec
	Payload              []byte
	StepBudget           int
	NextRunAt            time.Time
	NotifyRoute          string
	Status               string
	IdentityID           string
	OriginConversationID string
}

// CreateTask inserts a new task at its initial Status (default "active") and returns
// its domain projection. The status is part of the single INSERT, so a gated
// (pending_approval) task is never persisted as active first and then flipped — a
// crash between two statements can no longer leave a destructive task claimable
// (WR-03 atomic gate; the sqlc query already binds status as a parameter).
func (s *Store) CreateTask(ctx context.Context, p CreateTaskParams) (Task, error) {
	identityID := p.IdentityID
	if identityID == "" {
		identityID = "local"
	}
	status := p.Status
	if status == "" {
		status = "active"
	}
	row, err := s.q.CreateTask(ctx, sqlc.CreateTaskParams{
		ID:                   newUUID(),
		Kind:                 string(p.Kind),
		ScheduleKind:         string(p.Spec.Kind),
		CronExpr:             text(p.Spec.CronExpr),
		EveryMinutes:         int4OrNull(p.Spec.EveryMinutes),
		RunAt:                tsOrNull(p.Spec.RunAt),
		Tz:                   p.Spec.TZ,
		Payload:              payloadOrEmpty(p.Payload),
		StepBudget:           int4OrNull(p.StepBudget),
		Status:               status,
		NextRunAt:            tsOrNull(p.NextRunAt),
		NotifyRoute:          text(p.NotifyRoute),
		IdentityID:           identityID,
		OriginConversationID: uuidOrNull(p.OriginConversationID),
	})
	if err != nil {
		return Task{}, fmt.Errorf("create task: %w", err)
	}
	return taskFromRow(row), nil
}

// GetTask fetches one task by id. A missing row is ErrTaskNotFound (wrapped).
func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return Task{}, fmt.Errorf("get task: %w", err)
	}
	row, err := s.q.GetTask(ctx, u)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, fmt.Errorf("get task %q: %w", id, ErrTaskNotFound)
		}
		return Task{}, fmt.Errorf("get task %q: %w", id, err)
	}
	return taskFromRow(row), nil
}

// ListActiveTasks returns every active task ordered by its next fire.
func (s *Store) ListActiveTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.q.ListActiveTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active tasks: %w", err)
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, taskFromRow(r))
	}
	return out, nil
}

// CancelTask soft-cancels a task (status='cancelled'). Cancelling an absent id is
// a no-op (the UPDATE affects zero rows, no error).
func (s *Store) CancelTask(ctx context.Context, id string) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	if err := s.q.CancelTask(ctx, u); err != nil {
		return fmt.Errorf("cancel task %q: %w", id, err)
	}
	return nil
}

// UpdateNextRunAt advances a task's next fire (post-run reschedule).
func (s *Store) UpdateNextRunAt(ctx context.Context, id string, next time.Time) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("update next_run_at: %w", err)
	}
	if err := s.q.UpdateNextRunAt(ctx, sqlc.UpdateNextRunAtParams{ID: u, NextRunAt: tsOrNull(next)}); err != nil {
		return fmt.Errorf("update next_run_at %q: %w", id, err)
	}
	return nil
}

// InsertRun opens a new running agent_job_runs row for taskID.
func (s *Store) InsertRun(ctx context.Context, taskID string, stepBudget int) (Run, error) {
	tu, err := db.ParseUUID("uuid", taskID)
	if err != nil {
		return Run{}, fmt.Errorf("insert run: %w", err)
	}
	row, err := s.q.InsertRun(ctx, sqlc.InsertRunParams{
		ID:         newUUID(),
		TaskID:     tu,
		StepBudget: int4OrNull(stepBudget),
	})
	if err != nil {
		return Run{}, fmt.Errorf("insert run for task %q: %w", taskID, err)
	}
	return runFromRow(row), nil
}

// CreateRunAndAdvance is the atomic claim-then-reschedule write: it opens a run
// row AND advances the task's next_run_at in ONE transaction (db.WithTx), so a
// failure between the two never leaves a claimed task that will never fire again
// (or a fired task with no run row). Returns the opened run.
func (s *Store) CreateRunAndAdvance(ctx context.Context, taskID string, stepBudget int, next time.Time) (Run, error) {
	tu, err := db.ParseUUID("uuid", taskID)
	if err != nil {
		return Run{}, fmt.Errorf("create run and advance: %w", err)
	}
	var run Run
	err = db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		row, err := q.InsertRun(ctx, sqlc.InsertRunParams{ID: newUUID(), TaskID: tu, StepBudget: int4OrNull(stepBudget)})
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if err := q.UpdateNextRunAt(ctx, sqlc.UpdateNextRunAtParams{ID: tu, NextRunAt: tsOrNull(next)}); err != nil {
			return fmt.Errorf("advance next_run_at: %w", err)
		}
		run = runFromRow(row)
		return nil
	})
	if err != nil {
		return Run{}, fmt.Errorf("create run and advance for task %q: %w", taskID, err)
	}
	return run, nil
}

// Heartbeat bumps last_heartbeat_at on a run row (called on the held conn in
// 10-03; this Store method serves the non-held path and tests).
func (s *Store) Heartbeat(ctx context.Context, runID string) error {
	u, err := db.ParseUUID("uuid", runID)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if err := s.q.UpdateHeartbeat(ctx, u); err != nil {
		return fmt.Errorf("heartbeat run %q: %w", runID, err)
	}
	return nil
}

// CompleteRunParams carries a terminal run transition.
type CompleteRunParams struct {
	RunID         string
	Status        string // completed | failed
	Summary       string
	LastError     string
	CompletedHash string // idempotency key → completed_with_hash UNIQUE
}

// CompleteRun writes a terminal status. A duplicate completion hash trips the
// UNIQUE constraint (23505); that is swallowed as ErrAlreadyRunning so an
// at-least-once redelivery never double-records a completion (SC#2 idempotency).
func (s *Store) CompleteRun(ctx context.Context, p CompleteRunParams) error {
	u, err := db.ParseUUID("uuid", p.RunID)
	if err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	err = s.q.CompleteRun(ctx, sqlc.CompleteRunParams{
		ID:                u,
		Status:            p.Status,
		Summary:           text(p.Summary),
		LastError:         text(p.LastError),
		CompletedWithHash: text(p.CompletedHash),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("complete run %q: %w", p.RunID, ErrAlreadyRunning)
		}
		return fmt.Errorf("complete run %q: %w", p.RunID, err)
	}
	return nil
}

// taskFromRow projects a generated scheduler_tasks row to the domain Task.
func taskFromRow(r sqlc.AuraSchedulerTasks) Task {
	return Task{
		ID:                   uuid.UUID(r.ID.Bytes).String(),
		Kind:                 TaskKind(r.Kind),
		ScheduleKind:         ScheduleKind(r.ScheduleKind),
		CronExpr:             r.CronExpr.String,
		EveryMinutes:         int(r.EveryMinutes.Int32),
		RunAt:                r.RunAt.Time,
		TZ:                   r.Tz,
		Payload:              r.Payload,
		StepBudget:           int(r.StepBudget.Int32),
		Status:               r.Status,
		NextRunAt:            r.NextRunAt.Time,
		NotifyRoute:          r.NotifyRoute.String,
		IdentityID:           r.IdentityID,
		OriginConversationID: uuidString(r.OriginConversationID),
		CreatedAt:            r.CreatedAt.Time,
		UpdatedAt:            r.UpdatedAt.Time,
	}
}

// runFromRow projects a generated agent_job_runs row to the domain Run.
func runFromRow(r sqlc.AuraAgentJobRuns) Run {
	return Run{
		ID:                uuid.UUID(r.ID.Bytes).String(),
		TaskID:            uuid.UUID(r.TaskID.Bytes).String(),
		Status:            r.Status,
		StepBudget:        int(r.StepBudget.Int32),
		StartedAt:         r.StartedAt.Time,
		LastHeartbeatAt:   r.LastHeartbeatAt.Time,
		CompletedWithHash: r.CompletedWithHash.String,
		Summary:           r.Summary.String,
		LastError:         r.LastError.String,
		MissedSince:       r.MissedSince.Time,
		PausedStateToken:  uuidString(r.PausedStateToken),
		CompletedAt:       r.CompletedAt.Time,
	}
}

// --- pgtype conversion helpers (in-package; never leak pgtype past the boundary) ---

func newUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
}

func uuidOrNull(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func int4OrNull(n int) pgtype.Int4 {
	if n <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(n), Valid: true}
}

func tsOrNull(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func payloadOrEmpty(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// isUniqueViolation classifies a pgx error as SQLSTATE 23505 via errors.As +
// pgErr.Code — never string-matching the message (RESEARCH Pitfall 2).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
