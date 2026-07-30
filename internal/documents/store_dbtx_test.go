package documents

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

// jobRowValues returns the 17 positional column values, in sqlc Scan order, for an
// aura.document_ingest_jobs row. The store's queries scan into &field pointers of the
// matching types, so the scripted slice must mirror them exactly.
func jobRowValues(id pgtype.UUID, sourceID, status string, sparse, embedded int32, errText pgtype.Text, searchable, completed pgtype.Timestamptz) []any {
	now := pgtype.Timestamptz{Time: time.Unix(1000, 0).UTC(), Valid: true}
	return []any{
		id,                // id
		sourceID,          // source_id
		"local",           // source_kind
		"doc_1",           // document_id
		"hash",            // content_hash
		"/tmp/manual.pdf", // original_path
		"manual.pdf",      // file_name
		"application/pdf", // mime_type
		int64(42),         // size_bytes
		status,            // status
		sparse,            // sparse_chunks
		embedded,          // embedded_chunks
		errText,           // error
		now,               // created_at
		now,               // updated_at
		searchable,        // searchable_at
		completed,         // completed_at
	}
}

func TestPostgresJobStoreCreateBindsParamsAndMapsRow(t *testing.T) {
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	fake := &fakeDocDBTX{
		rowVal: &fakeDocRow{values: jobRowValues(pgID, "cli", string(JobAccepted), 0, 0, pgtype.Text{}, pgtype.Timestamptz{}, pgtype.Timestamptz{})},
	}
	store := &PostgresJobStore{q: sqlc.New(fake)}

	job, err := store.Create(t.Context(), CreateJobParams{
		SourceID:     "cli",
		SourceKind:   "local",
		DocumentID:   "doc_1",
		ContentHash:  "hash",
		OriginalPath: "/tmp/manual.pdf",
		FileName:     "manual.pdf",
		MIMEType:     "application/pdf",
		SizeBytes:    42,
		Status:       JobAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != id.String() {
		t.Fatalf("job id = %q, want %q", job.ID, id.String())
	}
	if job.SourceID != "cli" || job.Status != JobAccepted || job.SizeBytes != 42 {
		t.Fatalf("mapped job = %#v", job)
	}
	// Params must be bound positionally, never interpolated into the SQL text.
	if !strings.Contains(fake.queryRowSQL, "INSERT INTO aura.document_ingest_jobs") {
		t.Fatalf("create SQL = %q", fake.queryRowSQL)
	}
	if len(fake.queryArgs) != 9 || fake.queryArgs[0] != "cli" || fake.queryArgs[8] != string(JobAccepted) {
		t.Fatalf("create args = %#v", fake.queryArgs)
	}
}

func TestPostgresJobStoreCreatePropagatesScanError(t *testing.T) {
	fake := &fakeDocDBTX{rowVal: &fakeDocRow{scanErr: errors.New("insert violated constraint")}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.Create(t.Context(), CreateJobParams{SourceID: "cli", Status: JobAccepted})
	if err == nil || !strings.Contains(err.Error(), "constraint") {
		t.Fatalf("want constraint error, got %v", err)
	}
}

func TestPostgresJobStoreGetParsesUUIDAndMapsRow(t *testing.T) {
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	errText := pgtype.Text{String: "boom", Valid: true}
	fake := &fakeDocDBTX{
		rowVal: &fakeDocRow{values: jobRowValues(pgID, "cli", string(JobFailed), 2, 0, errText, pgtype.Timestamptz{}, pgtype.Timestamptz{})},
	}
	store := &PostgresJobStore{q: sqlc.New(fake)}

	job, err := store.Get(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != id.String() || job.Status != JobFailed || job.Error != "boom" || job.SparseChunks != 2 {
		t.Fatalf("mapped job = %#v", job)
	}
	// The id must be bound as a typed pgtype.UUID, not the raw string.
	if len(fake.queryArgs) != 1 {
		t.Fatalf("get args = %#v", fake.queryArgs)
	}
	if _, ok := fake.queryArgs[0].(pgtype.UUID); !ok {
		t.Fatalf("get arg type = %T, want pgtype.UUID", fake.queryArgs[0])
	}
}

func TestPostgresJobStoreGetRejectsInvalidUUID(t *testing.T) {
	store := &PostgresJobStore{q: sqlc.New(&fakeDocDBTX{})}
	_, err := store.Get(t.Context(), "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "invalid job id") {
		t.Fatalf("want invalid id error, got %v", err)
	}
}

func TestPostgresJobStoreGetPropagatesQueryError(t *testing.T) {
	fake := &fakeDocDBTX{rowVal: &fakeDocRow{scanErr: errors.New("no rows in result set")}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.Get(t.Context(), uuid.NewString())
	if err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("want no-rows error, got %v", err)
	}
}

func TestPostgresJobStoreGetByDocumentIDBindsDocumentID(t *testing.T) {
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	fake := &fakeDocDBTX{
		rowVal: &fakeDocRow{values: jobRowValues(pgID, "cli", string(JobSearchable), 5, 0, pgtype.Text{}, pgtype.Timestamptz{Time: time.Unix(2000, 0).UTC(), Valid: true}, pgtype.Timestamptz{})},
	}
	store := &PostgresJobStore{q: sqlc.New(fake)}

	job, err := store.GetByDocumentID(t.Context(), "doc_1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobSearchable || job.SearchableAt.IsZero() {
		t.Fatalf("mapped job = %#v", job)
	}
	if len(fake.queryArgs) != 1 || fake.queryArgs[0] != "doc_1" {
		t.Fatalf("getByDocumentID args = %#v", fake.queryArgs)
	}
}

func TestPostgresJobStoreGetByDocumentIDPropagatesError(t *testing.T) {
	fake := &fakeDocDBTX{rowVal: &fakeDocRow{scanErr: errors.New("no rows in result set")}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.GetByDocumentID(t.Context(), "doc_missing")
	if err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("want no-rows error, got %v", err)
	}
}

func TestPostgresJobStoreUpdateStatusBindsErrorText(t *testing.T) {
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	fake := &fakeDocDBTX{
		rowVal: &fakeDocRow{values: jobRowValues(pgID, "cli", string(JobFailed), 0, 0, pgtype.Text{String: "extract failed", Valid: true}, pgtype.Timestamptz{}, pgtype.Timestamptz{})},
	}
	store := &PostgresJobStore{q: sqlc.New(fake)}

	job, err := store.UpdateStatus(t.Context(), id.String(), JobFailed, "extract failed")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobFailed || job.Error != "extract failed" {
		t.Fatalf("mapped job = %#v", job)
	}
	if len(fake.queryArgs) != 3 {
		t.Fatalf("updateStatus args = %#v", fake.queryArgs)
	}
	gotText, ok := fake.queryArgs[2].(pgtype.Text)
	if !ok || !gotText.Valid || gotText.String != "extract failed" {
		t.Fatalf("error text arg = %#v", fake.queryArgs[2])
	}
}

func TestPostgresJobStoreUpdateStatusRejectsInvalidUUID(t *testing.T) {
	store := &PostgresJobStore{q: sqlc.New(&fakeDocDBTX{})}
	_, err := store.UpdateStatus(t.Context(), "bad", JobFailed, "boom")
	if err == nil || !strings.Contains(err.Error(), "invalid job id") {
		t.Fatalf("want invalid id error, got %v", err)
	}
}

func TestPostgresJobStoreUpdateStatusPropagatesError(t *testing.T) {
	fake := &fakeDocDBTX{rowVal: &fakeDocRow{scanErr: errors.New("deadlock detected")}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.UpdateStatus(t.Context(), uuid.NewString(), JobFailed, "boom")
	if err == nil || !strings.Contains(err.Error(), "deadlock") {
		t.Fatalf("want deadlock error, got %v", err)
	}
}

func TestPostgresJobStoreUpdateProgressBindsCounts(t *testing.T) {
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	fake := &fakeDocDBTX{
		rowVal: &fakeDocRow{values: jobRowValues(pgID, "cli", string(JobComplete), 7, 7, pgtype.Text{}, pgtype.Timestamptz{Time: time.Unix(3000, 0).UTC(), Valid: true}, pgtype.Timestamptz{Time: time.Unix(4000, 0).UTC(), Valid: true})},
	}
	store := &PostgresJobStore{q: sqlc.New(fake)}

	job, err := store.UpdateProgress(t.Context(), id.String(), JobComplete, 7, 7)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobComplete || job.SparseChunks != 7 || job.EmbeddedChunks != 7 || job.CompletedAt.IsZero() {
		t.Fatalf("mapped job = %#v", job)
	}
	if len(fake.queryArgs) != 4 {
		t.Fatalf("updateProgress args = %#v", fake.queryArgs)
	}
	if !strings.Contains(fake.queryRowSQL, "error = NULL") {
		t.Fatalf("successful progress must clear a stale error:\n%s", fake.queryRowSQL)
	}
	if fake.queryArgs[2] != int32(7) || fake.queryArgs[3] != int32(7) {
		t.Fatalf("count args = %#v", fake.queryArgs)
	}
}

func TestPostgresJobStoreUpdateProgressRejectsInvalidUUID(t *testing.T) {
	store := &PostgresJobStore{q: sqlc.New(&fakeDocDBTX{})}
	_, err := store.UpdateProgress(t.Context(), "bad", JobSearchable, 1, 0)
	if err == nil || !strings.Contains(err.Error(), "invalid job id") {
		t.Fatalf("want invalid id error, got %v", err)
	}
}

func TestPostgresJobStoreUpdateProgressPropagatesError(t *testing.T) {
	fake := &fakeDocDBTX{rowVal: &fakeDocRow{scanErr: errors.New("statement timeout")}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.UpdateProgress(t.Context(), uuid.NewString(), JobSearchable, 1, 0)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestPostgresJobStoreListRecentDefaultsLimitAndMapsRows(t *testing.T) {
	idA := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	idB := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	fake := &fakeDocDBTX{
		rows: &fakeDocRows{rows: [][]any{
			jobRowValues(idA, "cli", string(JobComplete), 3, 3, pgtype.Text{}, pgtype.Timestamptz{}, pgtype.Timestamptz{}),
			jobRowValues(idB, "telegram", string(JobSearchable), 1, 0, pgtype.Text{}, pgtype.Timestamptz{}, pgtype.Timestamptz{}),
		}},
	}
	store := &PostgresJobStore{q: sqlc.New(fake)}

	jobs, err := store.ListRecent(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	if jobs[0].Status != JobComplete || jobs[1].SourceID != "telegram" {
		t.Fatalf("mapped jobs = %#v", jobs)
	}
	// limit <= 0 falls back to 20.
	if len(fake.queryArgs) != 1 || fake.queryArgs[0] != int32(20) {
		t.Fatalf("listRecent limit arg = %#v", fake.queryArgs)
	}
	if !fake.rows.closed {
		t.Fatal("ListRecent did not close rows")
	}
}

func TestPostgresJobStoreListRecentPassesPositiveLimit(t *testing.T) {
	fake := &fakeDocDBTX{rows: &fakeDocRows{}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	jobs, err := store.ListRecent(t.Context(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	if fake.queryArgs[0] != int32(5) {
		t.Fatalf("listRecent limit arg = %#v", fake.queryArgs)
	}
}

func TestPostgresJobStoreListRecentPropagatesQueryError(t *testing.T) {
	fake := &fakeDocDBTX{queryErr: errors.New("connection refused")}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.ListRecent(t.Context(), 10)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("want connection error, got %v", err)
	}
}

func TestPostgresJobStoreListRecentPropagatesRowsError(t *testing.T) {
	fake := &fakeDocDBTX{rows: &fakeDocRows{
		rows:    [][]any{jobRowValues(pgtype.UUID{Bytes: uuid.New(), Valid: true}, "cli", string(JobComplete), 1, 1, pgtype.Text{}, pgtype.Timestamptz{}, pgtype.Timestamptz{})},
		rowsErr: errors.New("read tcp: reset"),
	}}
	store := &PostgresJobStore{q: sqlc.New(fake)}
	_, err := store.ListRecent(t.Context(), 10)
	if err == nil || !strings.Contains(err.Error(), "reset") {
		t.Fatalf("want rows error, got %v", err)
	}
}

func TestNewPostgresJobStoreWiresQueries(t *testing.T) {
	// NewPostgresJobStore takes a *pgxpool.Pool; passing nil still constructs a usable
	// struct (sqlc.New just stores the DBTX). This exercises the constructor without a live pool.
	store := NewPostgresJobStore(nil)
	if store == nil || store.q == nil {
		t.Fatalf("NewPostgresJobStore = %#v", store)
	}
}

// fakeDocDBTX is an in-memory sqlc.DBTX so PostgresJobStore's query methods run without a
// live Postgres. sqlc.New accepts any DBTX; the store test lives in-package so it can build
// &PostgresJobStore{q: sqlc.New(fake)} directly. It records the SQL + args each method
// receives (so a test can assert params are bound, never interpolated) and replays scripted
// rows / errors for the success and failure branches.
type fakeDocDBTX struct {
	rowVal      *fakeDocRow
	rows        *fakeDocRows
	queryErr    error
	queryRowSQL string
	querySQL    string
	queryArgs   []any
}

func (f *fakeDocDBTX) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("fakeDocDBTX: Exec not used by document store")
}

func (f *fakeDocDBTX) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.querySQL = sql
	f.queryArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows, nil
}

func (f *fakeDocDBTX) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queryRowSQL = sql
	f.queryArgs = args
	return f.rowVal
}

// fakeDocRow is a scripted pgx.Row. Scan copies the pre-set positional values into the
// caller's &field destinations via reflection (sqlc passes &field pointers), or returns scanErr.
type fakeDocRow struct {
	values  []any
	scanErr error
}

func (r *fakeDocRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return assignDocDest(dest, r.values)
}

// fakeDocRows is a scripted pgx.Rows over a slice of positional value-sets, honouring the
// sqlc read loop: Next advances, Scan copies the current set, Err is checked after the loop.
type fakeDocRows struct {
	rows    [][]any
	idx     int
	rowsErr error
	closed  bool
}

func (r *fakeDocRows) Close()                                       { r.closed = true }
func (r *fakeDocRows) Err() error                                   { return r.rowsErr }
func (r *fakeDocRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeDocRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeDocRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeDocRows) RawValues() [][]byte                          { return nil }
func (r *fakeDocRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeDocRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeDocRows) Scan(dest ...any) error {
	return assignDocDest(dest, r.rows[r.idx-1])
}

// assignDocDest reflectively writes each scripted value into the matching *T destination,
// the positional contract pgx.Rows.Scan honours.
func assignDocDest(dest, values []any) error {
	if len(dest) != len(values) {
		return errors.New("fakeDocScan: dest/value arity mismatch")
	}
	for i, d := range dest {
		rv := reflect.ValueOf(d)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return errors.New("fakeDocScan: destination is not a non-nil pointer")
		}
		rv.Elem().Set(reflect.ValueOf(values[i]))
	}
	return nil
}
