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

// DueTasks returns up to limit active tasks whose next_run_at has passed, locked
// FOR UPDATE SKIP LOCKED so concurrent workers never collide on the same row (the
// tick loop's batch pickup). limit is the max-concurrent headroom. The limit is
// clamped at the store boundary (defensive parity with envInt/int4OrNull): a
// non-positive limit would yield LIMIT 0 (no task ever dispatched) and a value past
// 2^31 would wrap to a negative LIMIT (a Postgres error), so any out-of-range input
// is floored to 1 rather than silently misbehaving (WR-02).
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
