//go:build db_integration

package toolapprovals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/testenv"
)

// refusedResume is the SQLSTATE migration 0160's trigger raises (object_not_in_prerequisite_state),
// so a permission error cannot pass for the guard's refusal.
const refusedResume = "55000"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	password := testenv.Require(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, testenv.Require(t, "AURA_DB_MIGRATE_URL"))
	appURL := testenv.Require(t, "AURA_DB_URL")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", password, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, password); err != nil {
		t.Fatal("ensure database roles failed")
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("apply migrations failed: %v", err)
	}
	pool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatal("open app pool failed")
	}
	t.Cleanup(pool.Close)
	return pool
}

// fixture is one owner's conversation on the application pool, where RLS applies.
type fixture struct {
	t           *testing.T
	ctx         context.Context
	pool        *pgxpool.Pool
	store       *Store
	owner, conv string
}

func newFixture(t *testing.T) *fixture {
	pool := testPool(t)
	f := &fixture{t: t, ctx: context.Background(), pool: pool, store: NewStore(pool)}
	f.owner = f.seedOwner()
	f.conv = uuid.NewString()
	f.exec(f.owner, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)`, f.conv, f.owner)
	return f
}

func (f *fixture) seedOwner() string {
	f.t.Helper()
	id := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO aura.identities(id,name,kind) VALUES($1,$2,'user')`, id, "tool-approval-"+id[:8]); err != nil {
		f.t.Fatalf("seed identity: %v", err)
	}
	f.t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, id); err != nil {
			f.t.Errorf("clean up identity: %v", err)
		}
	})
	return id
}

func (f *fixture) exec(owner, sql string, args ...any) {
	f.t.Helper()
	if err := db.WithIdentityTxRaw(f.ctx, f.pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, sql, args...)
		return err
	}); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *fixture) input(seq, index int, callID string) Input {
	return Input{
		IdentityID: f.owner, ConversationID: f.conv, PauseToken: uuid.NewString(), ToolCallID: callID,
		AssistantSeq: seq, CallIndex: index, ToolName: "shell_exec", Source: SourceTool,
		ExecuteArgs: json.RawMessage(`{"command": "rm -rf build"}`), Fingerprint: strings.Repeat("a", 64),
		Binding: "tracked-cwd:/workspace/proj", RanNote: "in /workspace/proj", Brief: "rm -rf build",
		GrantTool: "shell_exec", GrantAction: "exec",
	}
}

func (f *fixture) insertTx(owner string, in Input) error {
	return db.WithIdentityTx(f.ctx, f.pool, owner, func(q *sqlc.Queries) error {
		return f.store.InsertTx(f.ctx, q, in)
	})
}

func (f *fixture) insert(in Input) Record {
	f.t.Helper()
	if err := f.insertTx(in.IdentityID, in); err != nil {
		f.t.Fatalf("insert %s at %d/%d: %v", in.ToolCallID, in.AssistantSeq, in.CallIndex, err)
	}
	return f.reread(in.PauseToken, in.AssistantSeq)
}

func (f *fixture) reread(pauseToken string, seq int) Record {
	f.t.Helper()
	records, err := f.store.Decided(f.ctx, f.owner, f.conv, seq)
	if err != nil {
		f.t.Fatalf("read turn %d: %v", seq, err)
	}
	for _, r := range records {
		if r.PauseToken == pauseToken {
			return r
		}
	}
	f.t.Fatalf("no record for pause %s at seq %d", pauseToken, seq)
	return Record{}
}

func (f *fixture) decide(owner string, d Decision) (Record, error) {
	var rec Record
	err := db.WithIdentityTx(f.ctx, f.pool, owner, func(q *sqlc.Queries) error {
		var err error
		rec, err = f.store.DecideTx(f.ctx, q, d)
		return err
	})
	return rec, err
}

// walk moves a record through states, in order, and returns it re-read. An accept goes through
// DecideTx, the only way a record is accepted.
func (f *fixture) walk(r Record, states ...Status) Record {
	f.t.Helper()
	from := r.Status
	for _, to := range states {
		var err error
		if from == StatusPending && to == StatusAccepted {
			_, err = f.decide(f.owner, Decision{PauseToken: r.PauseToken, To: to, Scope: "once", OperatorID: f.owner})
		} else {
			err = f.store.Transition(f.ctx, f.owner, r.ID, from, to)
		}
		if err != nil {
			f.t.Fatalf("%s -> %s: %v", from, to, err)
		}
		from = to
	}
	return f.reread(r.PauseToken, r.AssistantSeq)
}

func uid(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := db.ParseUUID("id", s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDecideIsOneShot(t *testing.T) {
	f := newFixture(t)
	in := f.input(4, 0, "call-1")
	rec := f.insert(in)
	if rec.Status != StatusPending || !rec.DecidedAt.IsZero() || rec.Scope != "" || rec.OperatorID != "" {
		t.Fatalf("inserted record = %+v, want an undecided pending record", rec)
	}
	if !reflect.DeepEqual(rec.Input, in) {
		t.Fatalf("stored input = %+v, want %+v", rec.Input, in)
	}

	type outcome struct {
		rec Record
		err error
	}
	outcomes := make([]outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Go(func() {
			<-start
			r, err := f.decide(f.owner, Decision{PauseToken: in.PauseToken, To: StatusAccepted, Scope: "session", OperatorID: f.owner})
			outcomes[i] = outcome{r, err}
		})
	}
	close(start)
	wg.Wait()
	var won, lost int
	for _, o := range outcomes {
		switch {
		case o.err == nil && o.rec.ID == rec.ID && o.rec.Status == StatusAccepted && o.rec.Scope == "session" &&
			o.rec.OperatorID == f.owner && !o.rec.DecidedAt.IsZero():
			won++
		case errors.Is(o.err, ErrUnavailable):
			lost++
		default:
			t.Fatalf("decide outcome = %+v", o)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("won %d, lost %d; want one decision and one ErrUnavailable", won, lost)
	}

	expired, err := f.decide(f.owner, Decision{PauseToken: f.insert(f.input(4, 1, "call-2")).PauseToken, To: StatusExpired})
	if err != nil || expired.Status != StatusExpired || expired.Scope != "" || expired.OperatorID != "" {
		t.Fatalf("an expiry without an operator = %+v, %v", expired, err)
	}
	if _, err := f.decide(f.owner, Decision{PauseToken: uuid.NewString(), To: StatusAccepted}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an unknown pause: err = %v, want ErrUnavailable", err)
	}
}

func TestTransitionsFollowTheTable(t *testing.T) {
	f := newFixture(t)
	reach := map[Status][]Status{StatusAccepted: {StatusAccepted}, StatusRunning: {StatusAccepted, StatusRunning}}
	allowed := []struct{ from, to Status }{
		{StatusPending, StatusDeclined}, {StatusPending, StatusExpired},
		{StatusPending, StatusNotRun}, {StatusPending, StatusCancelled},
		{StatusAccepted, StatusRunning}, {StatusAccepted, StatusNotRun}, {StatusAccepted, StatusCancelled},
		{StatusRunning, StatusDone}, {StatusRunning, StatusUnknown}, {StatusRunning, StatusNotRun},
	}
	for i, pair := range allowed {
		rec := f.walk(f.insert(f.input(i+1, 0, "call")), reach[pair.from]...)
		if err := f.store.Transition(f.ctx, f.owner, rec.ID, pair.from, pair.to); err != nil {
			t.Fatalf("%s -> %s refused: %v", pair.from, pair.to, err)
		}
		if got := f.reread(rec.PauseToken, rec.AssistantSeq); got.Status != pair.to || got.DecidedAt.IsZero() {
			t.Fatalf("%s -> %s left %+v", pair.from, pair.to, got)
		}
	}

	pending := f.insert(f.input(100, 0, "call-pending"))
	done := f.walk(f.insert(f.input(101, 0, "call-done")), StatusAccepted, StatusRunning, StatusDone)
	accepted := f.walk(f.insert(f.input(102, 0, "call-accepted")), StatusAccepted)
	for name, try := range map[string]struct {
		owner, id string
		from, to  Status
	}{
		"pending -> accepted, by id": {f.owner, pending.ID, StatusPending, StatusAccepted},
		"pending -> running":         {f.owner, pending.ID, StatusPending, StatusRunning},
		"done -> running":            {f.owner, done.ID, StatusDone, StatusRunning},
		"a stale from":               {f.owner, accepted.ID, StatusPending, StatusDeclined},
		"a malformed record":         {f.owner, "nope", StatusAccepted, StatusRunning},
	} {
		if err := f.store.Transition(f.ctx, try.owner, try.id, try.from, try.to); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", name, err)
		}
	}
	if got := f.reread(pending.PauseToken, 100); got.Status != StatusPending {
		t.Fatalf("a refused transition moved the record to %s", got.Status)
	}
}

func TestRepeatedCallIDAcrossRoundsIsTwoRecords(t *testing.T) {
	f := newFixture(t)
	f.insert(f.input(4, 0, "call-r"))
	f.insert(f.input(9, 0, "call-r"))
	open, err := f.store.Open(f.ctx, f.owner, f.conv)
	if err != nil || len(open) != 2 || open[0].AssistantSeq != 4 || open[1].AssistantSeq != 9 ||
		open[0].ToolCallID != "call-r" || open[1].ToolCallID != "call-r" {
		t.Fatalf("open = %+v, %v; want call-r at seq 4 and at seq 9", open, err)
	}
	if err := f.insertTx(f.owner, f.input(4, 0, "call-other")); err == nil {
		t.Fatal("a second record took the same (assistant_seq, call_index) slot")
	}
}

func TestResultCommittedOnlyAfterTheParkedTurn(t *testing.T) {
	f := newFixture(t)
	rec := f.insert(f.input(4, 0, "call-x"))
	turn := func(seq int, callID string) {
		f.exec(f.owner, `INSERT INTO aura.conversation_turns (conversation_id, seq, role, tool_call_id, content)
VALUES ($1, $2, 'tool', $3, 'ok')`, f.conv, seq, callID)
	}
	committed := func(owner string) bool {
		t.Helper()
		ok, err := f.store.ResultCommitted(f.ctx, owner, rec)
		if err != nil {
			t.Fatalf("result committed: %v", err)
		}
		return ok
	}
	turn(3, "call-x")
	turn(5, "call-y")
	if committed(f.owner) {
		t.Fatal("a result before the parked turn, or for another call, counted")
	}
	turn(6, "call-x")
	if !committed(f.owner) {
		t.Fatal("the result after the parked turn did not count")
	}
	if committed(f.seedOwner()) {
		t.Fatal("another owner saw the result")
	}
}

func TestOpenOrdersByCallIndex(t *testing.T) {
	f := newFixture(t)
	f.insert(f.input(4, 2, "c"))
	f.walk(f.insert(f.input(4, 0, "a")), StatusAccepted, StatusRunning)
	f.walk(f.insert(f.input(4, 1, "b")), StatusAccepted)
	f.insert(f.input(2, 5, "z"))
	f.walk(f.insert(f.input(4, 3, "d")), StatusDeclined)
	f.walk(f.insert(f.input(3, 0, "e")), StatusAccepted, StatusRunning, StatusDone)
	open, err := f.store.Open(f.ctx, f.owner, f.conv)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range open {
		got = append(got, fmt.Sprintf("%d/%d %s", r.AssistantSeq, r.CallIndex, r.Status))
	}
	want := []string{"2/5 pending", "4/0 running", "4/1 accepted", "4/2 pending"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("open = %q, want %q", got, want)
	}
}

func TestRecordsAreOwnerScoped(t *testing.T) {
	f := newFixture(t)
	rec := f.insert(f.input(4, 0, "call-o"))
	f.walk(f.insert(f.input(4, 1, "call-a")), StatusAccepted)
	foreign := f.seedOwner()
	open, err := f.store.Open(f.ctx, foreign, f.conv)
	if err != nil || len(open) != 0 {
		t.Fatalf("foreign open = %+v, %v", open, err)
	}
	calls, err := f.store.OpenCalls(f.ctx, foreign, f.conv)
	if err != nil || len(calls) != 0 {
		t.Fatalf("foreign open calls = %+v, %v", calls, err)
	}
	turn, err := f.store.Decided(f.ctx, foreign, f.conv, 4)
	if err != nil || len(turn) != 0 {
		t.Fatalf("foreign turn = %+v, %v", turn, err)
	}
	stale, err := f.store.StaleAccepted(f.ctx, foreign, time.Now().Add(time.Hour))
	if err != nil || len(stale) != 0 {
		t.Fatalf("foreign stale = %+v, %v", stale, err)
	}
	if err := f.store.Transition(f.ctx, foreign, rec.ID, StatusPending, StatusDeclined); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("foreign transition: err = %v", err)
	}
	if _, err := f.decide(foreign, Decision{PauseToken: rec.PauseToken, To: StatusAccepted}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("foreign decide: err = %v", err)
	}
	if err := f.insertTx(foreign, f.input(5, 0, "call-forged")); err == nil {
		t.Fatal("a foreign transaction wrote a record for the owner")
	}
	if got := f.reread(rec.PauseToken, 4); got.Status != StatusPending {
		t.Fatalf("the owner's record moved to %s", got.Status)
	}
}

func TestStaleAcceptedHonoursDecidedAt(t *testing.T) {
	f := newFixture(t)
	old := f.walk(f.insert(f.input(4, 0, "old")), StatusAccepted)
	f.walk(f.insert(f.input(4, 1, "fresh")), StatusAccepted)
	oldDeclined := f.walk(f.insert(f.input(4, 2, "declined")), StatusDeclined)
	f.insert(f.input(4, 3, "pending"))
	f.exec(f.owner, `UPDATE aura.tool_approvals SET decided_at = now() - interval '1 hour' WHERE id IN ($1::uuid, $2::uuid)`,
		old.ID, oldDeclined.ID)
	cutoff := time.Now().Add(-30 * time.Minute)
	stale, err := f.store.StaleAccepted(f.ctx, f.owner, cutoff)
	if err != nil || len(stale) != 1 || stale[0].ID != old.ID || !stale[0].DecidedAt.Before(cutoff) {
		t.Fatalf("stale = %+v, %v; want only the record accepted an hour ago", stale, err)
	}
}

func TestOpenCallsKeysByAssistantSeq(t *testing.T) {
	f := newFixture(t)
	f.walk(f.insert(f.input(4, 0, "call-k")), StatusAccepted)
	f.walk(f.insert(f.input(9, 0, "call-k")), StatusAccepted, StatusRunning, StatusDone)
	f.insert(f.input(9, 1, "call-p"))
	f.walk(f.insert(f.input(9, 2, "call-r")), StatusAccepted, StatusRunning)
	calls, err := f.store.OpenCalls(f.ctx, f.owner, f.conv)
	want := []CallKey{{AssistantSeq: 4, ToolCallID: "call-k"}, {AssistantSeq: 9, ToolCallID: "call-r"}}
	if err != nil || !reflect.DeepEqual(calls, want) {
		t.Fatalf("open calls = %+v, %v; want %+v", calls, err, want)
	}
}

func TestDecidedReturnsEveryRecordOfTheTurn(t *testing.T) {
	f := newFixture(t)
	f.walk(f.insert(f.input(4, 2, "c")), StatusDeclined)
	f.insert(f.input(4, 0, "a"))
	f.walk(f.insert(f.input(4, 1, "b")), StatusAccepted)
	f.insert(f.input(5, 0, "next-turn"))
	turn, err := f.store.Decided(f.ctx, f.owner, f.conv, 4)
	if err != nil || len(turn) != 3 {
		t.Fatalf("turn 4 = %+v, %v; want its three records", turn, err)
	}
	for i, want := range []Status{StatusPending, StatusAccepted, StatusDeclined} {
		r := turn[i]
		if r.CallIndex != i || r.Status != want || r.DecidedAt.IsZero() != (want == StatusPending) {
			t.Fatalf("record %d = %+v, want call index %d in %s", i, r, i, want)
		}
	}
}

// pause writes a pause row of the given type, as the runner's CommitPause does.
func (f *fixture) pause(callID, pauseType string) string {
	f.t.Helper()
	token := uuid.NewString()
	if err := db.WithIdentityTx(f.ctx, f.pool, f.owner, func(q *sqlc.Queries) error {
		return q.InsertPausedState(f.ctx, sqlc.InsertPausedStateParams{
			Token: uid(f.t, token), ConversationID: uid(f.t, f.conv), Kind: "approval",
			Question: "Run rm -rf build?", Priority: 80, ToolCallID: callID,
			ResumeContext: []byte(`{"type": "` + pauseType + `"}`),
		})
	}); err != nil {
		f.t.Fatalf("insert pause: %v", err)
	}
	return token
}

func (f *fixture) resumed(token string) bool {
	f.t.Helper()
	var row sqlc.AuraPausedStates
	if err := db.WithIdentityTx(f.ctx, f.pool, f.owner, func(q *sqlc.Queries) error {
		var err error
		row, err = q.GetPausedStateByToken(f.ctx, uid(f.t, token))
		return err
	}); err != nil {
		f.t.Fatalf("read pause: %v", err)
	}
	return row.ResumedAt.Valid
}

// markResumed is the bare resume a binary that predates the record runs.
func (f *fixture) markResumed(token string) error {
	return db.WithIdentityTx(f.ctx, f.pool, f.owner, func(q *sqlc.Queries) error {
		n, err := q.MarkPausedStateResumed(f.ctx, sqlc.MarkPausedStateResumedParams{
			Token: uid(f.t, token), ResumedAnswer: []byte(`{"action": "accept", "content": ""}`),
		})
		if err == nil && n != 1 {
			err = fmt.Errorf("resumed %d pauses, want 1", n)
		}
		return err
	})
}

func (f *fixture) parkedCall(callID, pauseType string) Input {
	in := f.input(4, 0, callID)
	in.PauseToken = f.pause(callID, pauseType)
	f.insert(in)
	return in
}

func TestTriggerRefusesResumingAToolApprovalPauseWithAPendingRecord(t *testing.T) {
	f := newFixture(t)
	in := f.parkedCall("call-t", "tool_approval")
	err := f.markResumed(in.PauseToken)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != refusedResume {
		t.Fatalf("bare resume of a pending tool approval: err = %v, want SQLSTATE %s", err, refusedResume)
	}
	if f.resumed(in.PauseToken) {
		t.Fatal("the refused resume stamped resumed_at")
	}
	if got := f.reread(in.PauseToken, 4); got.Status != StatusPending {
		t.Fatalf("the record moved to %s", got.Status)
	}
}

func TestTriggerAllowsTheResolveThatDecidesTheRecordFirst(t *testing.T) {
	f := newFixture(t)
	in := f.parkedCall("call-t", "tool_approval")
	asker := askuser.New(f.pool)
	err := db.WithIdentityTx(f.ctx, f.pool, f.owner, func(q *sqlc.Queries) error {
		if _, err := f.store.DecideTx(f.ctx, q, Decision{PauseToken: in.PauseToken, To: StatusAccepted, Scope: "once", OperatorID: f.owner}); err != nil {
			return err
		}
		return asker.MarkResumedFencedTx(f.ctx, q, in.PauseToken, askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "yes"}, "")
	})
	if err != nil {
		t.Fatalf("decide then resume: %v", err)
	}
	if !f.resumed(in.PauseToken) || f.reread(in.PauseToken, 4).Status != StatusAccepted {
		t.Fatal("the resolve did not commit both the decision and the resume")
	}
}

func TestTriggerIgnoresOtherPauseTypes(t *testing.T) {
	f := newFixture(t)
	// A record never shares an ask_user pause's token; one here proves the guard reads the
	// pause's type, not the record alone.
	in := f.parkedCall("call-q", "ask_user")
	if err := f.markResumed(in.PauseToken); err != nil {
		t.Fatalf("an ask_user pause did not resume: %v", err)
	}
	if !f.resumed(in.PauseToken) {
		t.Fatal("the ask_user pause stayed pending")
	}
}
