package manager

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

// fakeDBTX is an in-memory sqlc.DBTX so the MCPAuditStore query methods exercise the
// boundary conversions without a live Postgres (mirrors internal/skills'
// fakeDBTX_test.go). It records the SQL + args each method receives and replays
// scripted results / errors, so the append-only INSERT path can return a
// *pgconn.PgError and drive classifyMCPAuditErr's SQLSTATE branch off the DB entirely.
type fakeDBTX struct {
	queryErr    error
	queryRows   *fakeRows
	queryArgs   []any
	queryRowVal *fakeRow
}

func (f *fakeDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeDBTX) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	f.queryArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRows, nil
}

func (f *fakeDBTX) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	f.queryArgs = args
	return f.queryRowVal
}

// fakeRow is a scripted pgx.Row: Scan copies the pre-set positional values into the
// caller's destinations via reflection, or returns scanErr.
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

// fakeRows is a scripted pgx.Rows over positional value-sets honouring sqlc's read
// loop: Next advances, Scan copies the current set, Err is checked after the loop.
type fakeRows struct {
	rows    [][]any
	idx     int
	rowsErr error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.rowsErr }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map                         { return pgtype.NewMap() }

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	return assignDest(dest, r.rows[r.idx-1])
}

// assignDest reflectively writes each scripted value into the matching *T destination
// — the same positional contract pgx.Rows.Scan honours.
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

// TestMCPAuditInsertToParamsNullBoundary asserts the MCPAuditInsert -> sqlc params
// boundary mapping: an empty Reason maps to a NULL (invalid) pgtype.Text, and a
// non-empty reason round-trips into a valid pgtype.Text. The other columns pass
// through verbatim.
func TestMCPAuditInsertToParamsNullBoundary(t *testing.T) {
	t.Parallel()

	// Empty reason (the install/edit/enable/disable/remove case) -> NULL.
	p := MCPAuditInsert{ActorIdentityID: "local", Action: "install", ServerName: "calc"}.toParams()
	if p.ActorIdentityID != "local" || p.Action != "install" || p.ServerName != "calc" {
		t.Fatalf("verbatim columns mismatch: %+v", p)
	}
	if p.Reason.Valid {
		t.Fatalf("empty reason must map to a NULL pgtype.Text, got %+v", p.Reason)
	}

	// Non-empty reason (the `trust` case) -> valid pgtype.Text.
	tp := MCPAuditInsert{ActorIdentityID: "local", Action: "trust", ServerName: "calc", Reason: "vetted"}.toParams()
	if !tp.Reason.Valid || tp.Reason.String != "vetted" {
		t.Fatalf("non-empty reason must map to valid pgtype.Text, got %+v", tp.Reason)
	}
}

// TestInsertMCPAuditTxBranches drives InsertMCPAuditTx through a fake DBTX: the
// privilege SQLSTATE (the append-only role grant / trigger) classifies as the
// immutable sentinel, and a successful scan returns nil.
func TestInsertMCPAuditTxBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 42501 insufficient_privilege (role grant / append-only trigger) -> ErrMCPAuditImmutable.
	f := &fakeDBTX{queryRowVal: &fakeRow{scanErr: &pgconn.PgError{Code: "42501", Message: "denied"}}}
	if err := InsertMCPAuditTx(ctx, sqlc.New(f), MCPAuditInsert{ServerName: "x", Action: "edit"}); !errors.Is(err, ErrMCPAuditImmutable) {
		t.Fatalf("42501 InsertMCPAuditTx = %v, want ErrMCPAuditImmutable", err)
	}

	// Success: the scripted RETURNING row scans cleanly.
	good := mcpAuditRowValues(uuid.New(), time.Now(), "local", "install", "calc", "")
	f = &fakeDBTX{queryRowVal: &fakeRow{values: good}}
	if err := InsertMCPAuditTx(ctx, sqlc.New(f), MCPAuditInsert{ServerName: "calc", Action: "install"}); err != nil {
		t.Fatalf("InsertMCPAuditTx success = %v, want nil", err)
	}
}

// TestClassifyMCPAuditErr pins the SQLSTATE-based classification (errors.As +
// pgErr.Code, never message matching): a non-pg error passes through, a non-mapped
// SQLSTATE passes through, and 42501 wraps the immutable sentinel.
func TestClassifyMCPAuditErr(t *testing.T) {
	t.Parallel()
	plain := errors.New("context deadline exceeded")
	if got := classifyMCPAuditErr(plain); !errors.Is(got, plain) {
		t.Fatalf("non-pg error must pass through, got %v", got)
	}
	if errors.Is(classifyMCPAuditErr(plain), ErrMCPAuditImmutable) {
		t.Fatal("a non-pg error must not classify as the immutable sentinel")
	}
	other := classifyMCPAuditErr(&pgconn.PgError{Code: "23505", Message: "unique"})
	if errors.Is(other, ErrMCPAuditImmutable) {
		t.Fatalf("an unmapped SQLSTATE must pass through, got %v", other)
	}
	if got := classifyMCPAuditErr(&pgconn.PgError{Code: "42501"}); !errors.Is(got, ErrMCPAuditImmutable) {
		t.Fatalf("42501 = %v, want ErrMCPAuditImmutable", got)
	}
}

// mcpAuditRowValues builds the positional value-set the sqlc scan loop expects for one
// aura.mcp_audit RETURNING/SELECT row, in column order. reason=="" maps to a NULL
// pgtype.Text.
func mcpAuditRowValues(id uuid.UUID, created time.Time, actor, action, server, reason string) []any {
	r := pgtype.Text{}
	if reason != "" {
		r = pgtype.Text{String: reason, Valid: true}
	}
	return []any{
		pgtype.UUID{Bytes: id, Valid: true},
		pgtype.Timestamptz{Time: created, Valid: true},
		actor,
		action,
		server,
		r,
	}
}
