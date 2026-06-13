// Unit tier (no build tag): the Store query methods run against an in-memory fakeDBTX
// (fakedbtx_test.go) so Insert / ListSince / AggregateSince — including their error
// branches and the parameterized-`since` binding — are covered without a live Postgres.
// The real DB round-trip is additionally exercised under db_integration in store_test.go
// (the canonical conversations/identity split).
package cachemetrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// newTestStore wraps an in-memory DBTX in the same sqlc surface New(pool) builds, so the
// query methods see the identical Queries shape they do in production.
func newTestStore(db sqlc.DBTX) *Store {
	return &Store{q: sqlc.New(db)}
}

func sampleInsertParams(t *testing.T) sqlc.InsertCacheMetricParams {
	t.Helper()
	p, err := NewInsertParams(MetricParams{
		ConversationID: uuid.Must(uuid.NewV7()).String(),
		Seq:            1,
		PromptTokens:   1200,
		CachedTokens:   1000,
		CostUSD:        0.0042,
	})
	if err != nil {
		t.Fatalf("NewInsertParams: %v", err)
	}
	return p
}

func TestNew_BuildsStore(t *testing.T) {
	t.Parallel()
	// New is constructed from a nil pool: it must not touch the pool eagerly (sqlc.New
	// only stashes the handle), so the Store is usable for wiring without a connection.
	s := New(nil)
	if s == nil || s.q == nil {
		t.Fatal("New(nil): want a non-nil Store with a wired Queries")
	}
}

func TestInsert_SuccessBindsAllParams(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{execTag: pgconn.NewCommandTag("INSERT 0 1")}
	s := newTestStore(fake)
	p := sampleInsertParams(t)

	if err := s.Insert(context.Background(), p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// The Runner reuses the already-computed params; the seam must pass all five through
	// to the INSERT in order (conversation_id, seq, prompt, cached, cost).
	if len(fake.execArgs) != 5 {
		t.Fatalf("Insert bound %d args, want 5: %#v", len(fake.execArgs), fake.execArgs)
	}
	if got := fake.execArgs[0].(pgtype.UUID); got != p.ConversationID {
		t.Errorf("conversation_id arg = %v, want %v", got, p.ConversationID)
	}
	if got := fake.execArgs[1].(int32); got != p.Seq {
		t.Errorf("seq arg = %d, want %d", got, p.Seq)
	}
	if got := fake.execArgs[2].(int32); got != p.PromptTokens {
		t.Errorf("prompt_tokens arg = %d, want %d", got, p.PromptTokens)
	}
}

func TestInsert_WrapsError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("pg down")
	s := newTestStore(&fakeDBTX{execErr: sentinel})
	err := s.Insert(context.Background(), sampleInsertParams(t))
	if err == nil {
		t.Fatal("Insert with a failing Exec: want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Insert error must wrap the driver error (%%w): got %v", err)
	}
	if got := err.Error(); got == sentinel.Error() {
		t.Errorf("Insert error must add context, got bare driver error %q", got)
	}
}

func newMetricRow(t *testing.T, id string, seq int32, ts time.Time, prompt, cached int32, cost float64) []any {
	t.Helper()
	convID, err := uuidFrom("conversation_id", id)
	if err != nil {
		t.Fatalf("uuidFrom(%q): %v", id, err)
	}
	costNum, err := numericFromFloat(cost)
	if err != nil {
		t.Fatalf("numericFromFloat(%v): %v", cost, err)
	}
	return []any{
		convID,
		seq,
		pgtype.Timestamptz{Time: ts, Valid: true},
		prompt,
		cached,
		costNum,
	}
}

func TestListSince_ProjectsRows(t *testing.T) {
	t.Parallel()
	id1 := uuid.Must(uuid.NewV7()).String()
	id2 := uuid.Must(uuid.NewV7()).String()
	t0 := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		newMetricRow(t, id1, 1, t0, 1000, 800, 0.0125),
		newMetricRow(t, id2, 2, t1, 2000, 1900, 0.2500),
	}}}
	s := newTestStore(fake)

	since := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	got, err := s.ListSince(context.Background(), since)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListSince returned %d rows, want 2", len(got))
	}
	// Boundary conversion: pgtype wrappers -> plain Go domain types.
	if got[0].ConversationID != id1 || got[0].Seq != 1 || got[0].PromptTokens != 1000 ||
		got[0].CachedTokens != 800 {
		t.Errorf("row 0 projection wrong: %+v", got[0])
	}
	if !got[0].TS.Equal(t0) {
		t.Errorf("row 0 ts = %v, want %v", got[0].TS, t0)
	}
	if diff := got[0].CostUSD - 0.0125; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("row 0 cost = %v, want 0.0125", got[0].CostUSD)
	}
	if got[1].ConversationID != id2 || got[1].Seq != 2 {
		t.Errorf("row 1 projection wrong: %+v", got[1])
	}
	if diff := got[1].CostUSD - 0.25; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("row 1 cost = %v, want 0.25", got[1].CostUSD)
	}
	// The window is bound, never interpolated (T-06-02): the since arg reaches the driver.
	if len(fake.queryArgs) != 1 {
		t.Fatalf("ListSince bound %d args, want 1", len(fake.queryArgs))
	}
	bound, ok := fake.queryArgs[0].(pgtype.Timestamptz)
	if !ok || !bound.Time.Equal(since) {
		t.Errorf("since arg = %#v, want bound timestamptz %v", fake.queryArgs[0], since)
	}
	if !fake.queryRows.closed {
		t.Error("ListSince must close the rows (defer rows.Close)")
	}
}

func TestListSince_Empty(t *testing.T) {
	t.Parallel()
	s := newTestStore(&fakeDBTX{queryRows: &fakeRows{rows: nil}})
	got, err := s.ListSince(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ListSince(empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty window: want 0 rows, got %d", len(got))
	}
}

func TestListSince_QueryError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("query boom")
	s := newTestStore(&fakeDBTX{queryErr: sentinel})
	_, err := s.ListSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("ListSince with a failing Query: want error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("ListSince error must wrap the driver error: got %v", err)
	}
}

func TestListSince_ScanError(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()
	sentinel := errors.New("scan boom")
	fake := &fakeDBTX{queryRows: &fakeRows{
		rows:    [][]any{newMetricRow(t, id, 1, time.Now(), 10, 5, 0.01)},
		scanAt:  1,
		scanErr: sentinel,
	}}
	s := newTestStore(fake)
	_, err := s.ListSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("ListSince with a failing row Scan: want error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("ListSince scan error must propagate: got %v", err)
	}
}

func TestListSince_RowsErr(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()
	sentinel := errors.New("rows iteration boom")
	fake := &fakeDBTX{queryRows: &fakeRows{
		rows:    [][]any{newMetricRow(t, id, 1, time.Now(), 10, 5, 0.01)},
		rowsErr: sentinel,
	}}
	s := newTestStore(fake)
	_, err := s.ListSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("ListSince with a non-nil rows.Err(): want error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("ListSince must surface rows.Err(): got %v", err)
	}
}

func aggRow(turns int64, prompt, cached, cost any) *fakeRow {
	return &fakeRow{values: []any{turns, prompt, cached, cost}}
}

func TestAggregateSince_Success(t *testing.T) {
	t.Parallel()
	cost, err := numericFromFloat(1.2345)
	if err != nil {
		t.Fatalf("numericFromFloat: %v", err)
	}
	fake := &fakeDBTX{queryRowVal: aggRow(3, int64(6000), int64(5400), cost)}
	s := newTestStore(fake)

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	agg, err := s.AggregateSince(context.Background(), since)
	if err != nil {
		t.Fatalf("AggregateSince: %v", err)
	}
	if agg.Turns != 3 || agg.TotalPromptTokens != 6000 || agg.TotalCachedTokens != 5400 {
		t.Errorf("aggregate counts wrong: %+v", agg)
	}
	if diff := agg.TotalCostUSD - 1.2345; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("aggregate cost = %v, want 1.2345", agg.TotalCostUSD)
	}
	// since is bound to the windowed query, never interpolated.
	if len(fake.queryArgs) != 1 {
		t.Fatalf("AggregateSince bound %d args, want 1", len(fake.queryArgs))
	}
	if bound, ok := fake.queryArgs[0].(pgtype.Timestamptz); !ok || !bound.Time.Equal(since) {
		t.Errorf("since arg = %#v, want bound timestamptz %v", fake.queryArgs[0], since)
	}
}

// TestAggregateSince_TextShapes proves the coalesce(sum(...)) results decode across the
// driver shapes anyInt64 / anyNumericFloat accept (text sums on some planners).
func TestAggregateSince_TextShapes(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: aggRow(2, "7000", []byte("6800"), "0.5000")}
	s := newTestStore(fake)
	agg, err := s.AggregateSince(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("AggregateSince(text shapes): %v", err)
	}
	if agg.TotalPromptTokens != 7000 || agg.TotalCachedTokens != 6800 {
		t.Errorf("text-shape token sums wrong: %+v", agg)
	}
	if diff := agg.TotalCostUSD - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("text-shape cost = %v, want 0.5", agg.TotalCostUSD)
	}
}

func TestAggregateSince_QueryRowScanError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("no rows")
	s := newTestStore(&fakeDBTX{queryRowVal: &fakeRow{scanErr: sentinel}})
	_, err := s.AggregateSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("AggregateSince with a failing Scan: want error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("AggregateSince must wrap the scan error: got %v", err)
	}
}

func TestAggregateSince_PromptDecodeError(t *testing.T) {
	t.Parallel()
	// An unmodeled prompt-tokens shape must surface as an error, never a fabricated 0
	// (WR-02): a zeroed cache aggregate would lie that the cache did nothing.
	s := newTestStore(&fakeDBTX{queryRowVal: aggRow(1, struct{}{}, int64(0), int64(0))})
	_, err := s.AggregateSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("AggregateSince with an unmodeled prompt shape: want error")
	}
	if got := err.Error(); !strings.Contains(got, "prompt_tokens") {
		t.Errorf("error should name the failing column, got %q", got)
	}
}

func TestAggregateSince_CachedDecodeError(t *testing.T) {
	t.Parallel()
	s := newTestStore(&fakeDBTX{queryRowVal: aggRow(1, int64(10), "not-a-number", int64(0))})
	_, err := s.AggregateSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("AggregateSince with an unparseable cached shape: want error")
	}
	if got := err.Error(); !strings.Contains(got, "cached_tokens") {
		t.Errorf("error should name the failing column, got %q", got)
	}
}

func TestAggregateSince_CostDecodeError(t *testing.T) {
	t.Parallel()
	s := newTestStore(&fakeDBTX{queryRowVal: aggRow(1, int64(10), int64(8), struct{}{})})
	_, err := s.AggregateSince(context.Background(), time.Now())
	if err == nil {
		t.Fatal("AggregateSince with an unmodeled cost shape: want error")
	}
	if got := err.Error(); !strings.Contains(got, "cost_usd") {
		t.Errorf("error should name the failing column, got %q", got)
	}
}
