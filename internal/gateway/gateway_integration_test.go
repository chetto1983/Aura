//go:build db_integration

// Integration proof tier for the synchronous reservation gate (GATE-03 + GATE-04)
// against the live append-only aura.tool_invocations ledger (migration 0011). Requires
// a running Postgres with migrations applied through 0011:
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/gateway -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset, so a skipped
// tier can never pass as green. The package goleak gate (main_test.go) fails on any
// leaked pgx pool goroutine.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localIdentity is the seeded single-user identity (migration 0004) the conversations FK
// resolves against.
const localIdentity = "00000000-0000-0000-0000-000000000001"

// skillRestoreArgs classifies to Normal (mutating but NOT GateRecommended) so it drives the
// auto-allow → reserve funnel without an approval pause.
var skillRestoreArgs = json.RawMessage(`{"action":"restore"}`)

// spyTool is a tools.Tool that counts Execute calls and, optionally, runs onExec inside
// Execute so a test can assert the reservation start row is already committed at that moment.
type spyTool struct {
	spec   tools.Spec
	count  int
	onExec func()
	result tools.ToolResult
}

func (s *spyTool) Spec() tools.Spec { return s.spec }

func (s *spyTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	s.count++
	if s.onExec != nil {
		s.onExec()
	}
	return s.result, nil
}

// gatedExec mirrors execTool's ordering exactly (llm_agent_retry.go): Decide first, then
// Deny→typed error / Approve→the approval-required tool RESULT (no Execute) / Replay→
// recorded outcome (no Execute) / Allow→Execute.
func gatedExec(ctx context.Context, g *Gateway, tool tools.Tool, args json.RawMessage, key ReservationKey) (tools.ToolResult, Verdict, error) {
	v, derr := g.Decide(ctx, tool.Spec(), args, key)
	if derr != nil {
		return tools.ToolResult{}, v, derr
	}
	switch v.Decision {
	case Deny:
		return tools.ToolResult{}, v, &ErrDenied{Reason: v.Reason, Tier: v.Tier}
	case Approve:
		if v.ApprovalRequest != nil {
			return *v.ApprovalRequest, v, nil
		}
		return tools.ToolResult{}, v, nil
	}
	if v.Replay != nil {
		return *v.Replay, v, nil
	}
	res, err := tool.Execute(ctx, args)
	return res, v, err
}

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

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(ownerCtx(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
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
	ensureLocalIdentity(t, pool)
	return pool
}

// ensureLocalIdentity re-seeds the `local` identity if a parallel/coverage run wiped it, so
// the conversations FK (and transitively tool_invocations) never fails with FK 23503.
func ensureLocalIdentity(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	if err := pool.QueryRow(ownerCtx(),
		"SELECT count(*) FROM aura.identities WHERE id = $1", localIdentity).Scan(&n); err != nil {
		t.Fatalf("check local identity: %v", err)
	}
	if n > 0 {
		return
	}
	if _, err := pool.Exec(ownerCtx(),
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, 'local', 'system') ON CONFLICT (id) DO NOTHING",
		localIdentity); err != nil {
		t.Fatalf("re-seed local identity (wiped by a parallel run?): %v", err)
	}
}

func seedConversation(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	convID := uuid.Must(uuid.NewV7()).String()
	seedAsOwner(t, pool, localIdentity,
		"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'test-model', 'active')",
		convID, localIdentity)
	return convID
}

func newKey(convID, toolCallID string) ReservationKey {
	return ReservationKey{
		ConversationID: convID,
		RequestID:      uuid.Must(uuid.NewV7()).String(),
		ToolCallID:     toolCallID,
	}
}

// startRowCount returns the number of committed `start` rows for the reservation triple.
func startRowCount(t *testing.T, pool *pgxpool.Pool, key ReservationKey) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ownerCtx(),
		`SELECT count(*) FROM aura.tool_invocations
		 WHERE conversation_id = $1 AND request_id = $2 AND tool_call_id = $3 AND event_kind = 'start'`,
		key.ConversationID, key.RequestID, key.ToolCallID).Scan(&n); err != nil {
		t.Fatalf("count start rows: %v", err)
	}
	return n
}

// insertEnd records a terminal end fact for the triple (simulating the async observer that
// writes the real outcome after Execute), so a subsequent Reserve can replay it.
func insertEnd(t *testing.T, store *toolinvocations.Store, key ReservationKey, preview, sidecar string) {
	t.Helper()
	if err := store.Insert(ownerCtx(), toolinvocations.Event{
		ConversationID:    key.ConversationID,
		RequestID:         key.RequestID,
		ToolCallID:        key.ToolCallID,
		ToolName:          "skill_manage",
		Event:             toolinvocations.EventEnd,
		Seq:               2,
		StartedAt:         time.Now().UTC(),
		EndedAt:           time.Now().UTC(),
		DurationMS:        3,
		Status:            "ok",
		ResultPreview:     preview,
		PreviewBytes:      len(preview),
		ResultBytes:       len(preview),
		ResultTruncated:   sidecar != "",
		ResultSidecarPath: sidecar,
	}); err != nil {
		t.Fatalf("insert end fact: %v", err)
	}
}

// tripleEvents returns the start and end rows recorded for one reservation triple.
func tripleEvents(t *testing.T, store *toolinvocations.Store, key ReservationKey) (starts, ends []toolinvocations.Event) {
	t.Helper()
	rows, err := store.ListByConversation(ownerCtx(), key.ConversationID)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	for _, r := range rows {
		if r.RequestID != key.RequestID || r.ToolCallID != key.ToolCallID {
			continue
		}
		switch r.Event {
		case toolinvocations.EventStart:
			starts = append(starts, r)
		case toolinvocations.EventEnd:
			ends = append(ends, r)
		}
	}
	return starts, ends
}

// TestReserveBeforeExecute proves the order invariant: the reservation start row is
// committed in Postgres BEFORE the spy tool's Execute runs (GATE-03).
func TestReserveBeforeExecute(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)

	convID := seedConversation(t, pool)
	key := newKey(convID, "call-order-1")

	startPresentAtExec := -1
	spy := &spyTool{
		spec:   tools.Spec{Name: "skill_manage", Mutating: true},
		result: tools.ToolResult{Preview: "executed"},
		onExec: func() { startPresentAtExec = startRowCount(t, pool, key) },
	}

	_, v, err := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key)
	if err != nil {
		t.Fatalf("gatedExec: %v", err)
	}
	if v.Decision != Allow || v.Replay != nil {
		t.Fatalf("verdict = %+v, want allow with no replay", v)
	}
	if spy.count != 1 {
		t.Fatalf("Execute count = %d, want 1", spy.count)
	}
	if startPresentAtExec != 1 {
		t.Fatalf("start rows present at Execute = %d, want 1 (reservation must commit BEFORE Execute)", startPresentAtExec)
	}
}

// TestReservationFailBlocks proves SC-3 / GATE-03 fail-closed: a reservation INSERT that
// fails (a conversation_id with no FK row) BLOCKS the mutating tool — Execute count == 0,
// verdict deny.
func TestReservationFailBlocks(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)

	// A conversation UUID that was never seeded → the reservation INSERT violates the
	// conversation_id FK (23503) → Reserve errors → fail-closed deny.
	badKey := newKey(uuid.Must(uuid.NewV7()).String(), "call-fail-1")

	spy := &spyTool{spec: tools.Spec{Name: "skill_manage", Mutating: true}}
	_, v, err := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, badKey)

	if v.Decision != Deny {
		t.Fatalf("verdict = %q, want deny (reservation failed)", v.Decision)
	}
	var denied *ErrDenied
	if !errors.As(err, &denied) {
		t.Fatalf("err = %v, want *ErrDenied", err)
	}
	if spy.count != 0 {
		t.Fatalf("Execute count = %d, want 0 (a failed reservation must block Execute)", spy.count)
	}
}

// TestIdempotentReplay proves GATE-04: a duplicate reservation key replays the recorded
// outcome instead of re-invoking Execute — Execute count == 1 exactly across two dispatches.
func TestIdempotentReplay(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)

	convID := seedConversation(t, pool)
	key := newKey(convID, "call-idem-1")
	spy := &spyTool{spec: tools.Spec{Name: "skill_manage", Mutating: true}, result: tools.ToolResult{Preview: "first-output"}}

	// First dispatch: reserve acquires, Execute runs.
	if _, v, err := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key); err != nil || v.Decision != Allow {
		t.Fatalf("first dispatch = (%+v, %v), want allow", v, err)
	}
	if spy.count != 1 {
		t.Fatalf("Execute count after first dispatch = %d, want 1", spy.count)
	}
	// The async observer records the real end fact.
	insertEnd(t, store, key, "first-output", "")

	// Second dispatch, SAME key: rows==0 → replay, Execute NOT called again.
	res, v, err := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key)
	if err != nil {
		t.Fatalf("second dispatch err: %v", err)
	}
	if v.Replay == nil {
		t.Fatal("second dispatch must carry a replay (rows==0)")
	}
	if spy.count != 1 {
		t.Fatalf("Execute count after replay = %d, want 1 (idempotent)", spy.count)
	}
	// A Layer A replay carries replayedMarker (HARN-03, D-10) appended to the recorded end.
	if res.Preview != "first-output"+replayedMarker {
		t.Fatalf("replay preview = %q, want the recorded end plus the replay marker", res.Preview)
	}
}

// TestReplayMissingSidecar proves Pitfall 6: a recorded end whose sidecar is gone replays
// the preview plus a `result expired` marker, with no error and no dangling FullPath.
func TestReplayMissingSidecar(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)

	convID := seedConversation(t, pool)
	key := newKey(convID, "call-sidecar-1")
	spy := &spyTool{spec: tools.Spec{Name: "skill_manage", Mutating: true}, result: tools.ToolResult{Preview: "big-output"}}

	// First dispatch acquires + executes; then record an end pointing at a GC'd sidecar.
	if _, _, err := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	insertEnd(t, store, key, "partial preview", "/nonexistent/definitely-gc-ed.result")

	res, v, err := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key)
	if err != nil {
		t.Fatalf("replay with missing sidecar must not error: %v", err)
	}
	if v.Replay == nil {
		t.Fatal("expected a replay (rows==0)")
	}
	if res.FullPath != "" {
		t.Fatalf("missing sidecar must clear FullPath, got %q", res.FullPath)
	}
	if !strings.Contains(res.Preview, "partial preview") || !strings.Contains(res.Preview, "result expired") {
		t.Fatalf("preview = %q, want partial preview + result-expired marker", res.Preview)
	}
	if spy.count != 1 {
		t.Fatalf("Execute count = %d, want 1 (replay, not re-execute)", spy.count)
	}
}

// skillDeleteArgs classifies to Destructive — the only tier that stops the turn — so it
// drives the approval path against the live ledger.
var skillDeleteArgs = json.RawMessage(`{"action":"delete","name":"obsolete-skill"}`)

// TestGatewayApprovalResumeReentersAndReservesOnce proves D-03 point 2 through the
// production carrier (the ledger the resume hook writes): an operator approval recorded
// via RecordResolvedApproval lets the resumed re-drive
// re-enter Decide -> routeApprove.Consume -> Verdict{Allow, OperatorID} -> the SINGLE 35-04
// reservation, executing exactly once with operator_id in that one start's Meta and NO
// competing executed row. A retry (re-recorded, same triple) replays idempotently (rows==0
// -> Verdict.Replay) with no second Execute — the reservation, not the ledger, guarantees
// exactly-once (the ledger is one-shot on Consume).
func TestGatewayApprovalResumeReentersAndReservesOnce(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	convID := seedConversation(t, pool)

	mutatingSpec := tools.Spec{Name: "skill_manage", Mutating: true}
	fp := gatewayArgsFingerprint(skillDeleteArgs)
	key := newKey(convID, "call-resume-1")

	// The host-side resume hook recorded the operator's accept (the production carrier).
	g.RecordResolvedApproval(convID, "skill_manage", fp, ResolvedApproval{Approved: true, OperatorID: "op-1"})

	startAtExec := -1
	spy := &spyTool{
		spec:   mutatingSpec,
		result: tools.ToolResult{Preview: "swarm-launched"},
		onExec: func() { startAtExec = startRowCount(t, pool, key) },
	}

	// (1) The resumed re-drive re-enters Decide via the ledger → Allow → reserve → Execute once.
	// The resume is an INTERACTIVE turn: the runner marks its ctx with a live responder
	// (runner.go:551 gateway.WithResponder), so the WR-01 deny-before-Consume gate
	// (ProfileServerProduction || !responderPresent) is not tripped and the cross-turn Consume
	// runs. A headless (no-responder) re-drive would correctly deny here (WR-01).
	_, v, err := gatedExec(WithResponder(ownerCtx()), g, spy, skillDeleteArgs, key)
	if err != nil || v.Decision != Allow {
		t.Fatalf("resumed re-drive = (%+v, %v), want allow via the ledger", v, err)
	}
	if v.OperatorID != "op-1" {
		t.Fatalf("resumed verdict operator id = %q, want op-1", v.OperatorID)
	}
	if spy.count != 1 || startAtExec != 1 {
		t.Fatalf("resumed call: Execute count = %d, start rows at Execute = %d; want 1/1", spy.count, startAtExec)
	}

	// The async observer records the real end fact for the executed call.
	insertEnd(t, store, key, "swarm-launched", "")

	// (2) A retry of the SAME triple (re-recorded approval) replays: rows==0 → Verdict.Replay,
	// Execute NOT called again (the reservation is the exactly-once guarantee).
	g.RecordResolvedApproval(convID, "skill_manage", fp, ResolvedApproval{Approved: true, OperatorID: "op-1"})
	res, rv, rerr := gatedExec(WithResponder(ownerCtx()), g, spy, skillDeleteArgs, key)
	if rerr != nil {
		t.Fatalf("resumed retry err: %v", rerr)
	}
	if rv.Replay == nil || spy.count != 1 {
		t.Fatalf("resumed retry: replay=%v Execute count=%d, want replay + count 1", rv.Replay, spy.count)
	}
	// A Layer A replay carries replayedMarker (HARN-03, D-10) appended to the recorded end.
	if res.Preview != "swarm-launched"+replayedMarker {
		t.Fatalf("replay preview = %q, want the recorded end plus the replay marker", res.Preview)
	}

	// Ledger shape: exactly ONE start (operator_id in Meta) + ONE end for the triple, NO
	// competing executed row (the operator_id rides the single reservation start — D-03 point 2).
	starts, ends := tripleEvents(t, store, key)
	if len(starts) != 1 {
		t.Fatalf("triple has %d start rows, want exactly 1 (no competing executed Insert)", len(starts))
	}
	if len(ends) != 1 {
		t.Fatalf("triple has %d end rows, want exactly 1", len(ends))
	}
	if starts[0].Meta["operator_id"] != "op-1" || starts[0].Meta["approved"] != true {
		t.Fatalf("start meta = %v, want operator_id/op-1 + approved:true", starts[0].Meta)
	}
}

// TestGatewayApprovalDeclineStaysFailClosed proves fail-closed on every non-accept outcome:
// with NO ledger entry (a decline records nothing), a hardened+responder mutating
// GateRecommended call returns Approve + a non-nil ApprovalRequest (WITHHELD, Execute==0);
// under server_production it Denies (Execute==0) and records the degraded_deny fact.
func TestGatewayApprovalDeclineStaysFailClosed(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	convID := seedConversation(t, pool)
	mutatingSpec := tools.Spec{Name: "skill_manage", Mutating: true}

	// (a) hardened + responder, NO ledger approval → Approve + ApprovalRequest, WITHHELD.
	hardened := New(config.ProfileSingleUserHardened, store)
	spy := &spyTool{spec: mutatingSpec, result: tools.ToolResult{Preview: "should-not-run"}}
	res, v, err := gatedExec(WithResponder(ownerCtx()), hardened, spy, skillDeleteArgs, newKey(convID, "call-decline-1"))
	if err != nil {
		t.Fatalf("withheld approve must not error: %v", err)
	}
	if v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("no-ledger hardened verdict = %+v, want approve + approval request (withheld)", v)
	}
	if !strings.Contains(res.Preview, "gateway_approval_required") {
		t.Fatalf("withheld result preview = %q, want the gateway_approval_required relay", res.Preview)
	}
	if spy.count != 0 {
		t.Fatalf("withheld call Execute count = %d, want 0 (fail-closed)", spy.count)
	}

	// (b) server_production → Deny (even with a responder), Execute==0.
	prod := New(config.ProfileServerProduction, store)
	prodSpy := &spyTool{spec: mutatingSpec}
	_, pv, perr := gatedExec(WithResponder(ownerCtx()), prod, prodSpy, skillDeleteArgs, newKey(convID, "call-decline-2"))
	if pv.Decision != Deny {
		t.Fatalf("production verdict = %q, want deny", pv.Decision)
	}
	var denied *ErrDenied
	if !errors.As(perr, &denied) {
		t.Fatalf("production err = %v, want *ErrDenied", perr)
	}
	if prodSpy.count != 0 {
		t.Fatalf("production Execute count = %d, want 0 (fail-closed)", prodSpy.count)
	}
}

// The D-01/D-04 round-discrimination end-to-end proof for SC#1/SC#2 (45-02-T1) lives in
// gateway_round_ordinal_integration_test.go — a concern split per CLAUDE.md's 600-LOC cap
// — reusing envOrSkip/migratedPool/seedConversation/spyTool/insertEnd defined above.
