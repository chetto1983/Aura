//go:build db_integration

// Integration tests for internal/toolinvocations against the real append-only
// aura.tool_invocations ledger (migration 0011). Requires a running Postgres with
// the migrations applied through 0011:
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/toolinvocations -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset, so a
// skipped tier can never pass as green in the pipeline. The package goleak gate
// (main_test.go TestMain) fails the package on any leaked pgx pool goroutine.
package toolinvocations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localIdentity is the seeded single-user identity (migration 0004) that the
// conversations FK resolves against.
const localIdentity = "00000000-0000-0000-0000-000000000001"

// envOrSkip mirrors internal/skills + internal/identity: skip locally, fail-loud
// under CI so a missing DSN never reports a falsely-green integration job.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapURL composes the superuser DSN EnsureRoles needs from the password.
func bootstrapURL(t *testing.T, pwd string) string {
	t.Helper()
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
}

// migratedPool ensures roles + migrations (through 0011) are applied, then returns
// an aura_app pool ready for the ledger store. Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := envOrSkip(t, "AURA_DB_URL")

	if err := db.EnsureRoles(ctx, bootstrapURL(t, pwd), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedConversation inserts a fresh conversation under the seeded local identity so
// the tool_invocations FK (conversation_id -> aura.conversations) resolves, and
// returns its id. Append-only ledger rows referencing it are left in place; a fresh
// conversation per run keeps the suite isolated.
func seedConversation(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	convID := uuid.Must(uuid.NewV7()).String()
	seedAsOwner(t, pool, localIdentity,
		"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'test-model', 'active')",
		convID, localIdentity)
	return convID
}

// TestLedgerRoundTrip proves the no-skip-as-green floor for 18-01-T2: a start fact
// and an end fact for one (conversation_id, request_id, tool_call_id) insert into the
// real append-only ledger, and ListByConversation reads both back correlated and in
// seq order with their lifecycle fields intact.
func TestLedgerRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	convID := seedConversation(t, pool)
	requestID := uuid.Must(uuid.NewV7()).String()
	const toolCallID = "call-rt-1"
	startedAt := time.Now().UTC().Truncate(time.Microsecond)
	endedAt := startedAt.Add(37 * time.Millisecond)
	exitCode := 0

	start := Event{
		ConversationID: convID,
		RequestID:      requestID,
		ToolCallID:     toolCallID,
		ToolName:       "shell_exec",
		Event:          EventStart,
		Seq:            1,
		StartedAt:      startedAt,
		Arguments:      `{"command":"echo hi"}`,
		ArgsBytes:      len(`{"command":"echo hi"}`),
	}
	if err := store.Insert(ctx, start); err != nil {
		t.Fatalf("Insert start: %v", err)
	}

	end := Event{
		ConversationID:  convID,
		RequestID:       requestID,
		ToolCallID:      toolCallID,
		ToolName:        "shell_exec",
		Event:           EventEnd,
		Seq:             2,
		StartedAt:       startedAt,
		EndedAt:         endedAt,
		DurationMS:      37,
		Status:          "ok",
		ResultPreview:   "hi\n",
		PreviewBytes:    3,
		ResultBytes:     3,
		ResultTruncated: false,
		ExitCode:        &exitCode,
		Meta:            map[string]any{"exit_code": 0, "timed_out": false},
	}
	if err := store.Insert(ctx, end); err != nil {
		t.Fatalf("Insert end: %v", err)
	}

	rows, err := store.ListByConversation(ctx, convID)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListByConversation returned %d rows, want 2 (start+end correlated)", len(rows))
	}

	got0, got1 := rows[0], rows[1]
	// seq ASC ordering: start (seq 1) before end (seq 2).
	if got0.Event != EventStart || got1.Event != EventEnd {
		t.Fatalf("event ordering: got [%s, %s], want [start, end]", got0.Event, got1.Event)
	}
	// Both facts correlate by the same (conversation_id, request_id, tool_call_id).
	for i, r := range rows {
		if r.ConversationID != convID || r.RequestID != requestID || r.ToolCallID != toolCallID {
			t.Fatalf("row %d not correlated: conv=%s req=%s call=%s", i, r.ConversationID, r.RequestID, r.ToolCallID)
		}
		if r.ToolName != "shell_exec" {
			t.Errorf("row %d tool_name = %q, want shell_exec", i, r.ToolName)
		}
	}
	// Start fact carries its args, no end-only fields.
	if got0.Arguments != `{"command":"echo hi"}` || got0.Seq != 1 {
		t.Errorf("start fact = %+v", got0)
	}
	if got0.Status != "" || got0.DurationMS != 0 {
		t.Errorf("start fact carries end-only fields: status=%q duration=%d", got0.Status, got0.DurationMS)
	}
	// End fact carries the result/lifecycle facts.
	if got1.Status != "ok" || got1.DurationMS != 37 || got1.ResultBytes != 3 || got1.PreviewBytes != 3 {
		t.Errorf("end fact = %+v", got1)
	}
	if got1.ExitCode == nil || *got1.ExitCode != 0 {
		t.Errorf("end fact exit_code = %v, want 0", got1.ExitCode)
	}
	if got1.Meta["exit_code"].(float64) != 0 || got1.Meta["timed_out"].(bool) {
		t.Errorf("end fact meta = %#v", got1.Meta)
	}
}

// TestLedgerAppendOnly proves the migration 0011 tamper-evidence: aura_app may
// INSERT and SELECT but UPDATE/DELETE/TRUNCATE are rejected (trigger raises
// insufficient_privilege OR the role lacks the grant), so the forensic ground truth
// the call-budget gate reads cannot be silently rewritten (T-18-01-R).
func TestLedgerAppendOnly(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	convID := seedConversation(t, pool)
	requestID := uuid.Must(uuid.NewV7()).String()
	if err := store.Insert(ctx, Event{
		ConversationID: convID,
		RequestID:      requestID,
		ToolCallID:     "call-immut-1",
		ToolName:       "shell_exec",
		Event:          EventStart,
		Seq:            1,
		StartedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	if _, err := pool.Exec(ctx, "UPDATE aura.tool_invocations SET tool_name = 'tampered' WHERE conversation_id = $1", convID); err == nil {
		t.Error("UPDATE as aura_app: want denied (trigger or role), got nil error")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.tool_invocations WHERE conversation_id = $1", convID); err == nil {
		t.Error("DELETE as aura_app: want denied (trigger or role), got nil error")
	}
	if _, err := pool.Exec(ctx, "TRUNCATE aura.tool_invocations"); err == nil {
		t.Error("TRUNCATE as aura_app: want denied (statement trigger or role), got nil error")
	}

	rows, err := store.ListByConversation(ctx, convID)
	if err != nil {
		t.Fatalf("ListByConversation after mutation attempts: %v", err)
	}
	if len(rows) != 1 || rows[0].ToolName != "shell_exec" {
		t.Fatalf("ledger row tampered or removed: want 1 row with tool_name shell_exec, got %d", len(rows))
	}
}

// TestReserveIdempotencyKey proves the GATE-04 idempotency key against the real UNIQUE
// index: the first Reserve of a (conv,req,toolCall) start acquires (rows==1); a second
// Reserve of the SAME key does not (rows==0) and, once an end fact exists, replays it.
func TestReserveIdempotencyKey(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	convID := seedConversation(t, pool)
	requestID := uuid.Must(uuid.NewV7()).String()
	const toolCallID = "call-reserve-1"

	start := Event{
		ConversationID: convID,
		RequestID:      requestID,
		ToolCallID:     toolCallID,
		ToolName:       "shell_exec",
		Event:          EventStart,
		Seq:            1,
		StartedAt:      time.Now().UTC(),
		Meta:           map[string]any{"reservation": true},
	}

	acquired, replay, err := store.Reserve(ctx, start)
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if !acquired || replay != nil {
		t.Fatalf("first Reserve = (acquired=%v, replay=%v), want (true, nil)", acquired, replay)
	}

	// Second Reserve of the same key: slot held, but no end yet → rows==0, replay nil.
	acquired, replay, err = store.Reserve(ctx, start)
	if err != nil {
		t.Fatalf("second Reserve (in-flight): %v", err)
	}
	if acquired || replay != nil {
		t.Fatalf("second Reserve (in-flight) = (acquired=%v, replay=%v), want (false, nil)", acquired, replay)
	}

	// Record the end fact, then a third Reserve replays it.
	end := Event{
		ConversationID: convID,
		RequestID:      requestID,
		ToolCallID:     toolCallID,
		ToolName:       "shell_exec",
		Event:          EventEnd,
		Seq:            2,
		StartedAt:      start.StartedAt,
		EndedAt:        time.Now().UTC(),
		DurationMS:     5,
		Status:         "ok",
		ResultPreview:  "reserved-output",
		PreviewBytes:   15,
		ResultBytes:    15,
	}
	if err := store.Insert(ctx, end); err != nil {
		t.Fatalf("Insert end: %v", err)
	}

	acquired, replay, err = store.Reserve(ctx, start)
	if err != nil {
		t.Fatalf("third Reserve (replay): %v", err)
	}
	if acquired {
		t.Fatal("third Reserve acquired a held slot, want replay")
	}
	if replay == nil || replay.ResultPreview != "reserved-output" {
		t.Fatalf("third Reserve replay = %+v, want the recorded end", replay)
	}
}

// TestGetEndAbsent proves GetEnd maps a missing end row to (nil, nil) — a valid replay
// state (reserved-but-not-finished), never an error.
func TestGetEndAbsent(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	convID := seedConversation(t, pool)
	end, err := store.GetEnd(ctx, convID, uuid.Must(uuid.NewV7()).String(), "call-absent")
	if err != nil {
		t.Fatalf("GetEnd for a missing end must not error: %v", err)
	}
	if end != nil {
		t.Fatalf("GetEnd = %+v, want nil (no end recorded)", end)
	}
}

// TestListInFlightBefore proves the reconciler anti-join: a start∧¬end older than the
// cutoff is returned; a completed (start+end) call and a start newer than the cutoff are not.
func TestListInFlightBefore(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	convID := seedConversation(t, pool)
	old := time.Now().UTC().Add(-time.Hour)
	recent := time.Now().UTC()

	// (a) an old in-flight start (no end) — SHOULD appear.
	inflightReq := uuid.Must(uuid.NewV7()).String()
	if err := store.Insert(ctx, Event{
		ConversationID: convID, RequestID: inflightReq, ToolCallID: "call-inflight",
		ToolName: "shell_exec", Event: EventStart, Seq: 1, StartedAt: old,
	}); err != nil {
		t.Fatalf("insert in-flight start: %v", err)
	}

	// (b) an old COMPLETED call (start+end) — should NOT appear.
	doneReq := uuid.Must(uuid.NewV7()).String()
	if err := store.Insert(ctx, Event{
		ConversationID: convID, RequestID: doneReq, ToolCallID: "call-done",
		ToolName: "shell_exec", Event: EventStart, Seq: 1, StartedAt: old,
	}); err != nil {
		t.Fatalf("insert done start: %v", err)
	}
	if err := store.Insert(ctx, Event{
		ConversationID: convID, RequestID: doneReq, ToolCallID: "call-done",
		ToolName: "shell_exec", Event: EventEnd, Seq: 2, StartedAt: old,
		EndedAt: old.Add(time.Second), DurationMS: 1000, Status: "ok",
	}); err != nil {
		t.Fatalf("insert done end: %v", err)
	}

	// (c) a RECENT in-flight start (after the cutoff) — should NOT appear.
	recentReq := uuid.Must(uuid.NewV7()).String()
	if err := store.Insert(ctx, Event{
		ConversationID: convID, RequestID: recentReq, ToolCallID: "call-recent",
		ToolName: "shell_exec", Event: EventStart, Seq: 1, StartedAt: recent,
	}); err != nil {
		t.Fatalf("insert recent start: %v", err)
	}

	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	inflight, err := store.ListInFlightBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListInFlightBefore: %v", err)
	}

	var sawInflight, sawDone, sawRecent bool
	for _, e := range inflight {
		switch e.ToolCallID {
		case "call-inflight":
			sawInflight = true
		case "call-done":
			sawDone = true
		case "call-recent":
			sawRecent = true
		}
	}
	if !sawInflight {
		t.Error("old start∧¬end (call-inflight) must be listed as a crash-orphan candidate")
	}
	if sawDone {
		t.Error("completed call (call-done) must NOT be listed (it has an end)")
	}
	if sawRecent {
		t.Error("recent start (call-recent) must NOT be listed (younger than the cutoff)")
	}
}
