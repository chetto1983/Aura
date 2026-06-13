package identity

import (
	"context"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeDBTX is an in-memory sqlc.DBTX so the Store's query methods can be exercised
// without a live Postgres: sqlc.New accepts any DBTX, and the test lives in-package so
// it can build Store{q: sqlc.New(fake)} directly. It records the SQL + args each method
// receives (so a test can assert the parameterized identity_id/capability are bound,
// never interpolated) and replays scripted results / errors for the success and failure
// branches. Mirrors internal/cachemetrics/fakedbtx_test.go (the canonical fake seam).
type fakeDBTX struct {
	execErr     error
	execTag     pgconn.CommandTag
	execSQL     string
	execArgs    []any
	queryErr    error
	queryRows   *fakeRows
	querySQL    string
	queryArgs   []any
	queryRowVal *fakeRow
	queryRowSQL string
}

func (f *fakeDBTX) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = sql
	f.execArgs = args
	return f.execTag, f.execErr
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

// fakeRow is a scripted pgx.Row. Scan copies the pre-set positional values into the
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

// fakeRows is a scripted pgx.Rows over a slice of positional value-sets. It honours the
// sqlc read loop: Next advances, Scan copies the current set, Err is checked after the
// loop. A scanErr fires on the row index it is keyed to; rowsErr surfaces from Err().
type fakeRows struct {
	rows    [][]any
	idx     int
	scanAt  int // 1-based row index that fails Scan (0 = never)
	scanErr error
	rowsErr error
	closed  bool
}

func (r *fakeRows) Close()                                       { r.closed = true }
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

// assignDest reflectively writes each scripted value into the matching *T destination,
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
