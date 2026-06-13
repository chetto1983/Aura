package cron

// store_runs_fake_test.go covers the agent_job_runs + run-loop-selection Store methods
// (InsertRun/Heartbeat/CompleteRun/GetRun/DueTasks/ScanStaleRuns) over the in-memory
// sqlc.DBTX fake declared in store_fake_test.go. It targets the branches the
// db_integration happy-path tier cannot reach from a real Postgres: the generic
// DB-error wraps, the ErrNoRows→ErrTaskNotFound mapping, the non-23505 PgError path of
// CompleteRun (vs the 23505 idempotency swallow), the DueTasks WR-02 limit clamp, and
// the post-loop rows.Err() stream failure. The pool-bound CreateRunAndAdvance (db.WithTx)
// and the held-conn insertRunOnConn/setMissedSinceOnConn stay in the real-PG tier.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// runRow returns the 12-column AuraAgentJobRuns value-set in the InsertRun/GetRun order.
func runRow(t *testing.T) []any {
	t.Helper()
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	return []any{
		uuidVal(t, fakeRunID),                      // id
		uuidVal(t, fakeTaskID),                     // task_id
		"running",                                  // status
		pgtype.Int4{Int32: 7, Valid: true},         // step_budget
		pgtype.Timestamptz{Time: now, Valid: true}, // started_at
		pgtype.Timestamptz{Time: now, Valid: true}, // last_heartbeat_at
		pgtype.Text{},                              // completed_with_hash (NULL)
		pgtype.Text{},                              // summary (NULL)
		pgtype.Text{},                              // last_error (NULL)
		pgtype.Timestamptz{},                       // missed_since (NULL)
		pgtype.UUID{},                              // paused_state_token (NULL)
		pgtype.Timestamptz{},                       // completed_at (NULL)
	}
}

// --- InsertRun ---

func TestStoreFakeInsertRunSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: runRow(t)}}
	s := storeWithFake(f)

	got, err := s.InsertRun(context.Background(), fakeTaskID, 7)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if got.ID != fakeRunID || got.TaskID != fakeTaskID || got.Status != "running" || got.StepBudget != 7 {
		t.Fatalf("run projection mismatch: %+v", got)
	}
}

func TestStoreFakeInsertRunInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if _, err := s.InsertRun(context.Background(), "bogus", 1); err == nil {
		t.Fatal("InsertRun must reject an invalid task UUID")
	}
}

func TestStoreFakeInsertRunDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: errDB}}
	s := storeWithFake(f)
	if _, err := s.InsertRun(context.Background(), fakeTaskID, 1); !errors.Is(err, errDB) {
		t.Fatalf("InsertRun must wrap the DB error, got %v", err)
	}
}

// --- Heartbeat ---

func TestStoreFakeHeartbeatSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)
	if err := s.Heartbeat(context.Background(), fakeRunID); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

func TestStoreFakeHeartbeatInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.Heartbeat(context.Background(), "bogus"); err == nil {
		t.Fatal("Heartbeat must reject an invalid run UUID")
	}
}

func TestStoreFakeHeartbeatDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	s := storeWithFake(f)
	if err := s.Heartbeat(context.Background(), fakeRunID); !errors.Is(err, errDB) {
		t.Fatalf("Heartbeat must wrap a valid-UUID DB error, got %v", err)
	}
}

// --- CompleteRun (incl. the 23505→ErrAlreadyRunning swallow) ---

func TestStoreFakeCompleteRunSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)
	if err := s.CompleteRun(context.Background(), CompleteRunParams{
		RunID: fakeRunID, Status: "completed", Summary: "ok", CompletedHash: fakeRunID,
	}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if len(f.gotArgs) != 5 || f.gotArgs[1].(string) != "completed" {
		t.Fatalf("CompleteRun must bind 5 params with the status, got %+v", f.gotArgs)
	}
}

func TestStoreFakeCompleteRunInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.CompleteRun(context.Background(), CompleteRunParams{RunID: "bogus", Status: "completed"}); err == nil {
		t.Fatal("CompleteRun must reject an invalid run UUID")
	}
}

func TestStoreFakeCompleteRunUniqueViolationSwallowedAsSentinel(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: &pgconn.PgError{Code: "23505", Message: "duplicate key"}}
	s := storeWithFake(f)

	err := s.CompleteRun(context.Background(), CompleteRunParams{RunID: fakeRunID, Status: "completed", CompletedHash: fakeRunID})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("a 23505 must map to ErrAlreadyRunning (SC#2 idempotency swallow), got %v", err)
	}
}

func TestStoreFakeCompleteRunOtherPgErrorWraps(t *testing.T) {
	t.Parallel()
	// A non-23505 PgError is a real failure, NOT the idempotency swallow.
	f := &cronFakeDBTX{execErr: &pgconn.PgError{Code: "23503", Message: "fk violation"}}
	s := storeWithFake(f)

	err := s.CompleteRun(context.Background(), CompleteRunParams{RunID: fakeRunID, Status: "failed", CompletedHash: fakeRunID})
	if errors.Is(err, ErrAlreadyRunning) || err == nil {
		t.Fatalf("a non-23505 PgError must surface as a wrapped error, not ErrAlreadyRunning, got %v", err)
	}
}

// --- GetRun ---

func TestStoreFakeGetRunSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: runRow(t)}}
	s := storeWithFake(f)

	got, err := s.GetRun(context.Background(), fakeRunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.ID != fakeRunID || got.TaskID != fakeTaskID {
		t.Fatalf("run projection mismatch: %+v", got)
	}
}

func TestStoreFakeGetRunInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if _, err := s.GetRun(context.Background(), "bogus"); err == nil {
		t.Fatal("GetRun must reject an invalid run UUID")
	}
}

func TestStoreFakeGetRunNotFoundMapsToSentinel(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: pgx.ErrNoRows}}
	s := storeWithFake(f)
	if _, err := s.GetRun(context.Background(), fakeRunID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("a missing run must map to ErrTaskNotFound, got %v", err)
	}
}

func TestStoreFakeGetRunDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: errDB}}
	s := storeWithFake(f)
	if _, err := s.GetRun(context.Background(), fakeRunID); errors.Is(err, ErrTaskNotFound) || !errors.Is(err, errDB) {
		t.Fatalf("a non-ErrNoRows run error must wrap (not the sentinel), got %v", err)
	}
}

// --- DueTasks (incl. the WR-02 limit clamp) ---

func TestStoreFakeDueTasksSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRows: &cronFakeRows{rows: [][]any{schedulerTaskRow(t)}}}
	s := storeWithFake(f)

	got, err := s.DueTasks(context.Background(), 4)
	if err != nil {
		t.Fatalf("DueTasks: %v", err)
	}
	if len(got) != 1 || got[0].ID != fakeTaskID {
		t.Fatalf("expected 1 due task, got %+v", got)
	}
	if lim, ok := f.gotArgs[0].(int32); !ok || lim != 4 {
		t.Fatalf("a valid limit must bind verbatim, got %v", f.gotArgs[0])
	}
}

func TestStoreFakeDueTasksClampsNonPositiveLimit(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRows: &cronFakeRows{}}
	s := storeWithFake(f)

	if _, err := s.DueTasks(context.Background(), 0); err != nil {
		t.Fatalf("DueTasks: %v", err)
	}
	if lim := f.gotArgs[0].(int32); lim != 1 {
		t.Fatalf("a non-positive limit must clamp to 1 (WR-02), got %d", lim)
	}
}

func TestStoreFakeDueTasksClampsOverflowLimit(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRows: &cronFakeRows{}}
	s := storeWithFake(f)

	if _, err := s.DueTasks(context.Background(), int(^uint(0)>>1)); err != nil { // math.MaxInt
		t.Fatalf("DueTasks: %v", err)
	}
	if lim := f.gotArgs[0].(int32); lim != 1 {
		t.Fatalf("a > MaxInt32 limit must clamp to 1 (no int32 wrap), got %d", lim)
	}
}

func TestStoreFakeDueTasksDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryErr: errDB}
	s := storeWithFake(f)
	if _, err := s.DueTasks(context.Background(), 4); !errors.Is(err, errDB) {
		t.Fatalf("DueTasks must wrap the query error, got %v", err)
	}
}

func TestStoreFakeDueTasksRowsErrWraps(t *testing.T) {
	t.Parallel()
	// The post-loop rows.Err() failure path (a stream error after Next returned true).
	f := &cronFakeDBTX{queryRows: &cronFakeRows{rows: [][]any{schedulerTaskRow(t)}, rowsErr: errDB}}
	s := storeWithFake(f)
	if _, err := s.DueTasks(context.Background(), 4); !errors.Is(err, errDB) {
		t.Fatalf("DueTasks must surface a rows.Err() stream failure, got %v", err)
	}
}

// --- ScanStaleRuns ---

func TestStoreFakeScanStaleRunsSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRows: &cronFakeRows{rows: [][]any{
		{uuidVal(t, fakeRunID), uuidVal(t, fakeTaskID)},
	}}}
	s := storeWithFake(f)

	got, err := s.ScanStaleRuns(context.Background(), staleRecoverySeconds)
	if err != nil {
		t.Fatalf("ScanStaleRuns: %v", err)
	}
	if len(got) != 1 || got[0].RunID != fakeRunID || got[0].TaskID != fakeTaskID {
		t.Fatalf("stale-run projection mismatch: %+v", got)
	}
	if secs, ok := f.gotArgs[0].(float64); !ok || secs != staleRecoverySeconds {
		t.Fatalf("stale window seconds must bind verbatim, got %v", f.gotArgs[0])
	}
}

func TestStoreFakeScanStaleRunsDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryErr: errDB}
	s := storeWithFake(f)
	if _, err := s.ScanStaleRuns(context.Background(), 90); !errors.Is(err, errDB) {
		t.Fatalf("ScanStaleRuns must wrap the query error, got %v", err)
	}
}
