package skills

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeDBTX is an in-memory sqlc.DBTX so the AuditStore query methods (InsertAuditTx,
// List/auditFromRow) and the boundary conversions exercise without a live Postgres:
// sqlc.New accepts any DBTX and this in-package test builds AuditStore{q: sqlc.New(fake)}
// directly. It mirrors internal/cachemetrics/fakedbtx_test.go: it records the SQL + args
// each method receives and replays scripted results / errors. The append-only INSERT
// path returns a *pgconn.PgError so classifyAuditErr's SQLSTATE branches are exercised
// off the DB entirely.
type fakeDBTX struct {
	queryErr    error
	queryRows   *fakeRows
	querySQL    string
	queryArgs   []any
	queryRowVal *fakeRow
	queryRowSQL string
}

func (f *fakeDBTX) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeDBTX) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.querySQL = sql
	f.queryArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRows, nil
}

func (f *fakeDBTX) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queryRowSQL = sql
	f.queryArgs = args
	return f.queryRowVal
}

// fakeRow is a scripted pgx.Row: scan copies the pre-set positional values into the
// caller's destinations via reflection (sqlc passes &field pointers), or returns scanErr.
type fakeRow struct {
	values  []any
	scanErr error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return assignDest(dest, r.values)
}

// fakeRows is a scripted pgx.Rows over positional value-sets honouring sqlc's read loop:
// Next advances, Scan copies the current set, Err is checked after the loop. scanAt fires
// scanErr on that 1-based row index; rowsErr surfaces from Err().
type fakeRows struct {
	rows    [][]any
	idx     int
	scanAt  int
	scanErr error
	rowsErr error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.rowsErr }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanAt != 0 && r.idx == r.scanAt {
		return r.scanErr
	}
	return assignDest(dest, r.rows[r.idx-1])
}

// assignDest reflectively writes each scripted value into the matching *T destination —
// the same positional contract pgx.Rows.Scan honours.
func assignDest(dest, values []any) error {
	if len(dest) != len(values) {
		return errors.New("fakeScan: dest/value arity mismatch")
	}
	for i, d := range dest {
		rv := reflect.ValueOf(d)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return errors.New("fakeScan: destination is not a non-nil pointer")
		}
		rv.Elem().Set(reflect.ValueOf(values[i]))
	}
	return nil
}

// TestAuditInsertToParamsNullBoundary asserts the AuditInsert -> sqlc params boundary
// mapping: identity defaults to "local" when empty, ApprovalNone maps to a NULL (invalid)
// pgtype.Text, an empty token maps to a NULL pgtype.UUID, and a valid token round-trips
// into the UUID bytes. This is the pure NULL-boundary contract the real-PG tier only
// exercises with valid tokens.
func TestAuditInsertToParamsNullBoundary(t *testing.T) {
	t.Parallel()
	tok := uuid.New()
	in := AuditInsert{
		ActorID:          "model",
		SkillName:        "calc",
		Action:           AuditActivate,
		ContentHash:      "sha256:deadbeef",
		ApprovalSource:   ApprovalAskUser,
		PausedStateToken: tok.String(),
		GateRecommended:  true,
		GateTaken:        true,
	}
	p, err := in.toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if p.IdentityID != "local" {
		t.Fatalf("empty identity must default to local, got %q", p.IdentityID)
	}
	if !p.ApprovalSource.Valid || p.ApprovalSource.String != "ask_user" {
		t.Fatalf("approval source mapping = %+v, want valid ask_user", p.ApprovalSource)
	}
	if !p.PausedStateToken.Valid || uuid.UUID(p.PausedStateToken.Bytes) != tok {
		t.Fatalf("token mapping = %+v, want valid %s", p.PausedStateToken, tok)
	}
	if p.Action != string(AuditActivate) {
		t.Fatalf("action = %q, want activate", p.Action)
	}

	// The pending tuple: NULL approval source, NULL token, explicit identity preserved.
	pending := AuditInsert{IdentityID: "alice", SkillName: "x", Action: AuditCreate, ApprovalSource: ApprovalNone}
	pp, err := pending.toParams()
	if err != nil {
		t.Fatalf("toParams pending: %v", err)
	}
	if pp.IdentityID != "alice" {
		t.Fatalf("explicit identity must survive, got %q", pp.IdentityID)
	}
	if pp.ApprovalSource.Valid {
		t.Fatalf("ApprovalNone must map to a NULL approval source, got %+v", pp.ApprovalSource)
	}
	if pp.PausedStateToken.Valid {
		t.Fatalf("empty token must map to a NULL UUID, got %+v", pp.PausedStateToken)
	}
}

// TestAuditInsertToParamsRejectsBadToken pins the invalid-UUID branch of toParams — a
// malformed PausedStateToken is a structured error BEFORE any DB round-trip. The
// real-PG tier never reaches this (the writer always passes a canonical UUID), so it is
// only reachable here.
func TestAuditInsertToParamsRejectsBadToken(t *testing.T) {
	t.Parallel()
	in := AuditInsert{SkillName: "x", Action: AuditActivate, PausedStateToken: "not-a-uuid"}
	_, err := in.toParams()
	if err == nil || !strings.Contains(err.Error(), "invalid paused_state_token") {
		t.Fatalf("toParams with bad token = %v, want an invalid-token error", err)
	}
}

// TestInsertAuditTxBranches drives InsertAuditTx through a fake DBTX: a bad token short-
// circuits before the query (toParams error), a NULL-violating SQLSTATE classifies as the
// coherence sentinel, the privilege SQLSTATE classifies as the immutable sentinel, and a
// successful scan returns nil.
func TestInsertAuditTxBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	goodRow := auditRowValues(uuid.New(), time.Now(), "model", "local", "calc", "create", "sha256:x", "", true, false, false)

	// toParams failure: returns before touching the fake.
	q := sqlc.New(&fakeDBTX{})
	if err := InsertAuditTx(ctx, q, AuditInsert{SkillName: "x", Action: AuditCreate, PausedStateToken: "bad"}); err == nil {
		t.Fatal("InsertAuditTx with bad token must error before the query")
	}

	// 23514 check violation -> ErrAuditIncoherent.
	f := &fakeDBTX{queryRowVal: &fakeRow{scanErr: &pgconn.PgError{Code: "23514", Message: "coherence"}}}
	if err := InsertAuditTx(ctx, sqlc.New(f), AuditInsert{SkillName: "x", Action: AuditCreate}); !errors.Is(err, ErrAuditIncoherent) {
		t.Fatalf("23514 InsertAuditTx = %v, want ErrAuditIncoherent", err)
	}

	// 42501 insufficient_privilege (role grant / append-only trigger) -> ErrAuditImmutable.
	f = &fakeDBTX{queryRowVal: &fakeRow{scanErr: &pgconn.PgError{Code: "42501", Message: "denied"}}}
	if err := InsertAuditTx(ctx, sqlc.New(f), AuditInsert{SkillName: "x", Action: AuditCreate}); !errors.Is(err, ErrAuditImmutable) {
		t.Fatalf("42501 InsertAuditTx = %v, want ErrAuditImmutable", err)
	}

	// Success: the scripted RETURNING row scans cleanly.
	f = &fakeDBTX{queryRowVal: &fakeRow{values: goodRow}}
	if err := InsertAuditTx(ctx, sqlc.New(f), AuditInsert{SkillName: "calc", Action: AuditCreate}); err != nil {
		t.Fatalf("InsertAuditTx success = %v, want nil", err)
	}
}

// TestClassifyAuditErr pins the SQLSTATE-based classification (errors.As + pgErr.Code,
// never message matching): a non-pg error passes through unchanged, a non-mapped SQLSTATE
// passes through, and the two mapped codes wrap the sentinels.
func TestClassifyAuditErr(t *testing.T) {
	t.Parallel()
	plain := errors.New("context deadline exceeded")
	if got := classifyAuditErr(plain); !errors.Is(got, plain) {
		t.Fatalf("non-pg error must pass through, got %v", got)
	}
	if errors.Is(classifyAuditErr(plain), ErrAuditImmutable) || errors.Is(classifyAuditErr(plain), ErrAuditIncoherent) {
		t.Fatal("a non-pg error must not classify as a skill_audit sentinel")
	}

	other := classifyAuditErr(&pgconn.PgError{Code: "23505", Message: "unique"})
	if errors.Is(other, ErrAuditImmutable) || errors.Is(other, ErrAuditIncoherent) {
		t.Fatalf("an unmapped SQLSTATE must pass through, got %v", other)
	}

	if got := classifyAuditErr(&pgconn.PgError{Code: "42501"}); !errors.Is(got, ErrAuditImmutable) {
		t.Fatalf("42501 = %v, want ErrAuditImmutable", got)
	}
	if got := classifyAuditErr(&pgconn.PgError{Code: "23514"}); !errors.Is(got, ErrAuditIncoherent) {
		t.Fatalf("23514 = %v, want ErrAuditIncoherent", got)
	}
}

// TestAuditStoreListProjectsRows drives List + auditFromRow through a fake DBTX: it
// asserts the default limit fallback, the filter argument binding, the NULL vs non-NULL
// projection of approval_source / paused_state_token, and the newest-first row order.
func TestAuditStoreListProjectsRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id1, id2 := uuid.New(), uuid.New()
	tok := uuid.New()
	now := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)

	rows := [][]any{
		auditRowValues(id1, now, "model", "local", "calc", "activate", "sha256:a", "ask_user", true, true, false),
		auditRowValues(id2, now.Add(-time.Hour), "cli", "local", "calc", "create", "sha256:b", "", true, false, false),
	}
	withTok := rows[0]
	withTok[8] = pgtype.UUID{Bytes: tok, Valid: true} // paused_state_token column

	f := &fakeDBTX{queryRows: &fakeRows{rows: rows}}
	s := &AuditStore{q: sqlc.New(f)}

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	out, err := s.List(ctx, AuditFilter{SkillName: "calc", Since: since}) // Limit 0 -> default
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("List len = %d, want 2", len(out))
	}
	// Default limit (100) bound as $1; the name + since are bound (never interpolated).
	if len(f.queryArgs) != 3 {
		t.Fatalf("List bound %d args, want 3", len(f.queryArgs))
	}
	if lim, ok := f.queryArgs[0].(int32); !ok || lim != 100 {
		t.Fatalf("limit arg = %v, want int32(100)", f.queryArgs[0])
	}
	name, ok := f.queryArgs[1].(pgtype.Text)
	if !ok || !name.Valid || name.String != "calc" {
		t.Fatalf("name arg = %v, want valid pgtype.Text 'calc'", f.queryArgs[1])
	}
	if ts, ok := f.queryArgs[2].(pgtype.Timestamptz); !ok || !ts.Valid || !ts.Time.Equal(since) {
		t.Fatalf("since arg = %v, want bound timestamptz %s", f.queryArgs[2], since)
	}

	// Row 0: non-NULL approval source + token project onto plain strings.
	if out[0].ApprovalSource != ApprovalAskUser {
		t.Fatalf("row0 approval source = %q, want ask_user", out[0].ApprovalSource)
	}
	if out[0].PausedStateToken != tok.String() {
		t.Fatalf("row0 token = %q, want %s", out[0].PausedStateToken, tok)
	}
	if out[0].ID != id1.String() {
		t.Fatalf("row0 id = %q, want %s", out[0].ID, id1)
	}
	// Row 1: NULL approval source + NULL token project to empty strings.
	if out[1].ApprovalSource != "" || out[1].PausedStateToken != "" {
		t.Fatalf("row1 NULL columns must project to empty strings, got src=%q tok=%q", out[1].ApprovalSource, out[1].PausedStateToken)
	}
}

// TestAuditStoreListErrors drives the query-error and mid-iteration scan-error paths so
// List surfaces the wrapped failure instead of a partial slice.
func TestAuditStoreListErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	qErr := errors.New("connection reset")
	s := &AuditStore{q: sqlc.New(&fakeDBTX{queryErr: qErr})}
	if _, err := s.List(ctx, AuditFilter{Limit: 5}); !errors.Is(err, qErr) {
		t.Fatalf("List query error = %v, want wrapped connection reset", err)
	}

	scanErr := errors.New("scan boom")
	rows := [][]any{auditRowValues(uuid.New(), time.Now(), "m", "local", "x", "create", "h", "", true, false, false)}
	s = &AuditStore{q: sqlc.New(&fakeDBTX{queryRows: &fakeRows{rows: rows, scanAt: 1, scanErr: scanErr}})}
	if _, err := s.List(ctx, AuditFilter{Limit: 5}); !errors.Is(err, scanErr) {
		t.Fatalf("List scan error = %v, want wrapped scan boom", err)
	}

	rowsErr := errors.New("rows iteration failed")
	s = &AuditStore{q: sqlc.New(&fakeDBTX{queryRows: &fakeRows{rows: [][]any{}, rowsErr: rowsErr}})}
	if _, err := s.List(ctx, AuditFilter{Limit: 5}); !errors.Is(err, rowsErr) {
		t.Fatalf("List rows.Err = %v, want wrapped iteration failure", err)
	}
}

// auditRowValues builds the positional value-set the sqlc scan loop expects for one
// aura.skill_audit RETURNING/SELECT row, in column order. approvalSrc=="" maps to a NULL
// pgtype.Text; the paused_state_token defaults to NULL (a caller overrides index 8).
func auditRowValues(id uuid.UUID, created time.Time, actor, identity, skill, action, hash, approvalSrc string, gateRec, gateTaken, blockOverride bool) []any {
	src := pgtype.Text{}
	if approvalSrc != "" {
		src = pgtype.Text{String: approvalSrc, Valid: true}
	}
	return []any{
		pgtype.UUID{Bytes: id, Valid: true},
		pgtype.Timestamptz{Time: created, Valid: true},
		actor,
		identity,
		skill,
		action,
		hash,
		src,
		pgtype.UUID{}, // paused_state_token: NULL by default
		gateRec,
		gateTaken,
		blockOverride,
	}
}
