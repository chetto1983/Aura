package cron

// store_fake_test.go exercises the Store query methods WITHOUT a live Postgres by
// injecting an in-memory sqlc.DBTX (the internal/cachemetrics/fakedbtx_test.go idiom):
// Store{q: sqlc.New(fake)} can be built in-package, so the wrap/sentinel/edge branches
// the db_integration happy-path tier never reaches (a DB error from a VALID UUID, the
// 23505→ErrAlreadyRunning swallow, ErrNoRows→ErrTaskNotFound, the row projections of
// NULL columns) are covered here deterministically. The pool-bound writers
// (CreateRunAndAdvance, SweepDueNotifications, the held-conn insertRunOnConn/
// setMissedSinceOnConn/heartbeat, and claim) genuinely need the real-PG tier and stay
// there — this file never fabricates a pgxpool.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- in-memory sqlc.DBTX (mirrors internal/cachemetrics/fakedbtx_test.go) ---

// cronFakeDBTX is a single-shot in-memory DBTX. Each Store method under test issues
// exactly one q.* call, so one scripted result per fake is enough; it records the SQL
// + args so a test can assert the parameterized binding, and replays the success /
// failure branch.
type cronFakeDBTX struct {
	execErr     error
	execTag     pgconn.CommandTag
	gotSQL      string
	gotArgs     []any
	queryErr    error
	queryRows   *cronFakeRows
	queryRowVal *cronFakeRow
}

func (f *cronFakeDBTX) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.gotSQL = sql
	f.gotArgs = args
	return f.execTag, f.execErr
}

func (f *cronFakeDBTX) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.gotSQL = sql
	f.gotArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRows, nil
}

func (f *cronFakeDBTX) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.gotSQL = sql
	f.gotArgs = args
	return f.queryRowVal
}

// cronFakeRow is a scripted pgx.Row: scan copies the positional values into the sqlc
// &field destinations via reflection, or returns scanErr (the ErrNoRows / DB-error path).
type cronFakeRow struct {
	values  []any
	scanErr error
}

func (r *cronFakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return cronAssignDest(dest, r.values)
}

// cronFakeRows is a scripted pgx.Rows over positional value-sets. It honours the sqlc
// read loop (Next advances, Scan copies, Err is checked after the loop); rowsErr lets a
// test drive the post-loop rows.Err() failure branch.
type cronFakeRows struct {
	rows    [][]any
	idx     int
	rowsErr error
}

func (r *cronFakeRows) Close()                                       {}
func (r *cronFakeRows) Err() error                                   { return r.rowsErr }
func (r *cronFakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *cronFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *cronFakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *cronFakeRows) RawValues() [][]byte                          { return nil }
func (r *cronFakeRows) Conn() *pgx.Conn                              { return nil }

func (r *cronFakeRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *cronFakeRows) Scan(dest ...any) error {
	return cronAssignDest(dest, r.rows[r.idx-1])
}

// cronAssignDest reflectively writes each scripted value into the matching *T sqlc
// destination, the same positional contract pgx honours.
func cronAssignDest(dest, values []any) error {
	if len(dest) != len(values) {
		return errors.New("cronFakeScan: dest/value arity mismatch")
	}
	for i, d := range dest {
		rv := reflect.ValueOf(d)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return errors.New("cronFakeScan: destination is not a non-nil pointer")
		}
		rv.Elem().Set(reflect.ValueOf(values[i]))
	}
	return nil
}

// storeWithFake builds a pool-less Store whose query path is the in-memory fake. The
// nil pool is safe ONLY for the non-tx q.* methods under test here; a pool-bound write
// would nil-panic, which is the contract (those belong to the db_integration tier).
func storeWithFake(f *cronFakeDBTX) *Store {
	return &Store{q: sqlc.New(f)}
}

// --- fixtures: full positional value-sets matching each RETURNING/SELECT column ---

func uuidVal(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

const (
	fakeTaskID  = "11111111-1111-1111-1111-111111111111"
	fakeRunID   = "22222222-2222-2222-2222-222222222222"
	fakeConvID  = "33333333-3333-3333-3333-333333333333"
	fakeNotifID = "44444444-4444-4444-4444-444444444444"
)

// schedulerTaskRow returns the 16-column AuraSchedulerTasks value-set in the
// CreateTask/GetTask/ListActiveTasks/DueTasks column order.
func schedulerTaskRow(t *testing.T) []any {
	t.Helper()
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	return []any{
		uuidVal(t, fakeTaskID), // id
		"reminder",             // kind
		"cron",                 // schedule_kind
		pgtype.Text{String: "30 9 * * *", Valid: true}, // cron_expr
		pgtype.Int4{},                                // every_minutes (NULL)
		pgtype.Timestamptz{},                         // run_at (NULL)
		"Europe/Rome",                                // tz
		[]byte(`{"text":"hydrate"}`),                 // payload
		pgtype.Int4{Int32: 7, Valid: true},           // step_budget
		"active",                                     // status
		pgtype.Timestamptz{Time: now, Valid: true},   // next_run_at
		pgtype.Text{String: "whatsapp", Valid: true}, // notify_route
		"id-owner",                                   // identity_id
		uuidVal(t, fakeConvID),                       // origin_conversation_id
		pgtype.Timestamptz{Time: now, Valid: true},   // created_at
		pgtype.Timestamptz{Time: now, Valid: true},   // updated_at
	}
}

// errDB is a generic non-classified DB error so the wrap branches (never the sentinel
// swallow) are exercised.
var errDB = errors.New("connection reset by peer")

// --- CreateTask ---

func TestStoreFakeCreateTaskSuccessAndDefaults(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: schedulerTaskRow(t)}}
	s := storeWithFake(f)

	got, err := s.CreateTask(context.Background(), CreateTaskParams{
		Kind:    KindReminder,
		Spec:    ScheduleSpec{Kind: KindCron, CronExpr: "30 9 * * *", TZ: "Europe/Rome"},
		Payload: []byte(`{"text":"hydrate"}`),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got.ID != fakeTaskID || got.Kind != KindReminder || got.TZ != "Europe/Rome" {
		t.Fatalf("projection mismatch: %+v", got)
	}
	// Defaults are bound into the INSERT args: empty IdentityID→"local", empty Status→"active".
	args := f.gotArgs
	if len(args) != 14 {
		t.Fatalf("CreateTask must bind 14 params, got %d", len(args))
	}
	if id, ok := args[12].(string); !ok || id != "local" {
		t.Fatalf("empty IdentityID must default to \"local\" in the INSERT, got %v", args[12])
	}
	if status, ok := args[9].(string); !ok || status != "active" {
		t.Fatalf("empty Status must default to \"active\" in the INSERT, got %v", args[9])
	}
	// Payload is passed through (not the {} fallback) when non-empty.
	if pl, ok := args[7].([]byte); !ok || string(pl) != `{"text":"hydrate"}` {
		t.Fatalf("payload binding = %v", args[7])
	}
}

func TestStoreFakeCreateTaskEmptyPayloadFallsBackToEmptyObject(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: schedulerTaskRow(t)}}
	s := storeWithFake(f)

	if _, err := s.CreateTask(context.Background(), CreateTaskParams{
		Kind:       KindAgentJob,
		Spec:       ScheduleSpec{Kind: KindEvery, EveryMinutes: 5, TZ: "UTC"},
		IdentityID: "id-explicit",
		Status:     "pending_approval",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if pl, ok := f.gotArgs[7].([]byte); !ok || string(pl) != "{}" {
		t.Fatalf("empty payload must bind the {} fallback (payloadOrEmpty), got %v", f.gotArgs[7])
	}
	if f.gotArgs[12].(string) != "id-explicit" {
		t.Fatalf("explicit IdentityID must be bound verbatim, got %v", f.gotArgs[12])
	}
	if f.gotArgs[9].(string) != "pending_approval" {
		t.Fatalf("explicit Status (the atomic gate) must be bound verbatim, got %v", f.gotArgs[9])
	}
}

func TestStoreFakeCreateTaskDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: errDB}}
	s := storeWithFake(f)

	_, err := s.CreateTask(context.Background(), CreateTaskParams{Kind: KindReminder, Spec: ScheduleSpec{Kind: KindCron, CronExpr: "* * * * *", TZ: "UTC"}})
	if err == nil || !errors.Is(err, errDB) {
		t.Fatalf("CreateTask must wrap the DB error, got %v", err)
	}
}

// --- GetTask ---

func TestStoreFakeGetTaskSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: schedulerTaskRow(t)}}
	s := storeWithFake(f)

	got, err := s.GetTask(context.Background(), fakeTaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != fakeTaskID || got.NotifyRoute != "whatsapp" || got.OriginConversationID != fakeConvID {
		t.Fatalf("projection mismatch: %+v", got)
	}
}

func TestStoreFakeGetTaskInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if _, err := s.GetTask(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("GetTask must reject an invalid UUID before touching the DB")
	}
}

func TestStoreFakeGetTaskNotFoundMapsToSentinel(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: pgx.ErrNoRows}}
	s := storeWithFake(f)

	_, err := s.GetTask(context.Background(), fakeTaskID)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("a missing row must map to ErrTaskNotFound, got %v", err)
	}
}

func TestStoreFakeGetTaskDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: errDB}}
	s := storeWithFake(f)

	_, err := s.GetTask(context.Background(), fakeTaskID)
	if errors.Is(err, ErrTaskNotFound) || !errors.Is(err, errDB) {
		t.Fatalf("a non-ErrNoRows DB error must wrap (not the sentinel), got %v", err)
	}
}

// --- ListActiveTasks ---

func TestStoreFakeListActiveTasksSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRows: &cronFakeRows{rows: [][]any{schedulerTaskRow(t), schedulerTaskRow(t)}}}
	s := storeWithFake(f)

	got, err := s.ListActiveTasks(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTasks: %v", err)
	}
	if len(got) != 2 || got[0].ID != fakeTaskID {
		t.Fatalf("expected 2 projected tasks, got %+v", got)
	}
}

func TestStoreFakeListActiveTasksDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryErr: errDB}
	s := storeWithFake(f)

	if _, err := s.ListActiveTasks(context.Background()); !errors.Is(err, errDB) {
		t.Fatalf("ListActiveTasks must wrap the query error, got %v", err)
	}
}

// --- CancelTask ---

func TestStoreFakeCancelTaskSuccess(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)
	if err := s.CancelTask(context.Background(), fakeTaskID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if len(f.gotArgs) != 1 {
		t.Fatalf("CancelTask binds the id, got args %+v", f.gotArgs)
	}
}

func TestStoreFakeCancelTaskInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.CancelTask(context.Background(), "bogus"); err == nil {
		t.Fatal("CancelTask must reject an invalid UUID")
	}
}

func TestStoreFakeCancelTaskDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	s := storeWithFake(f)
	if err := s.CancelTask(context.Background(), fakeTaskID); !errors.Is(err, errDB) {
		t.Fatalf("CancelTask must wrap a valid-UUID DB error, got %v", err)
	}
}

// --- UpdateNextRunAt ---

func TestStoreFakeUpdateNextRunAtSuccessAndNullForZero(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)

	// A zero next time binds a NULL timestamptz (tsOrNull) — the one-shot retire path.
	if err := s.UpdateNextRunAt(context.Background(), fakeTaskID, time.Time{}); err != nil {
		t.Fatalf("UpdateNextRunAt: %v", err)
	}
	ts, ok := f.gotArgs[1].(pgtype.Timestamptz)
	if !ok || ts.Valid {
		t.Fatalf("a zero next must bind a NULL timestamptz, got %#v", f.gotArgs[1])
	}
}

func TestStoreFakeUpdateNextRunAtBindsFutureInUTC(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)

	next := time.Date(2026, 6, 14, 7, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	if err := s.UpdateNextRunAt(context.Background(), fakeTaskID, next); err != nil {
		t.Fatalf("UpdateNextRunAt: %v", err)
	}
	ts := f.gotArgs[1].(pgtype.Timestamptz)
	if !ts.Valid || !ts.Time.Equal(next.UTC()) || ts.Time.Location() != time.UTC {
		t.Fatalf("next must bind as UTC, got %v", ts.Time)
	}
}

func TestStoreFakeUpdateNextRunAtInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.UpdateNextRunAt(context.Background(), "bogus", time.Now()); err == nil {
		t.Fatal("UpdateNextRunAt must reject an invalid UUID")
	}
}

func TestStoreFakeUpdateNextRunAtDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	s := storeWithFake(f)
	if err := s.UpdateNextRunAt(context.Background(), fakeTaskID, time.Now()); !errors.Is(err, errDB) {
		t.Fatalf("UpdateNextRunAt must wrap a valid-UUID DB error, got %v", err)
	}
}
