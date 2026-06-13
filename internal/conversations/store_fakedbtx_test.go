// Unit tier (no build tag): the non-transactional Store read/write methods driven
// through an in-memory sqlc.DBTX fake, so the projection + error-classification
// branches that otherwise only run under db_integration also run with `go test`
// (no live Postgres). The fake is local to this file (modeled on
// internal/cachemetrics/fakedbtx_test.go): sqlc.New accepts any DBTX, and the test
// is in-package so it can build Store{q: sqlc.New(fake)} directly.
//
// The atomic AppendTurn family is intentionally NOT here: it wraps db.WithTx, which
// calls (*pgxpool.Pool).Begin on a concrete pool — there is no DBTX seam to fake.
// Those paths are covered under db_integration in store_test.go.
package conversations

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
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

// fakeDBTX is an in-memory sqlc.DBTX. It scripts ONE result-set per kind (QueryRow,
// Query, Exec) — enough for the single-statement Store methods under test. QueryRow
// returns queryRowVal; Query returns queryRows (or queryErr); Exec returns execErr.
type fakeDBTX struct {
	execErr     error
	queryErr    error
	queryRows   *fakeRows
	queryRowVal *fakeRow
}

func (f *fakeDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.execErr
}

func (f *fakeDBTX) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRows, nil
}

func (f *fakeDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	if f.queryRowVal == nil {
		return &fakeRow{scanErr: pgx.ErrNoRows}
	}
	return f.queryRowVal
}

// fakeRow is a scripted pgx.Row: Scan copies the pre-set positional values into the
// caller's &dest pointers, or returns scanErr.
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

// fakeRows is a scripted pgx.Rows over positional value-sets, honoring the sqlc read
// loop (Next advances, Scan copies, Err checked after).
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

// fakeStore builds a Store whose query surface is the fake (pool stays nil — the
// non-tx methods never touch it).
func fakeStore(t *testing.T, fake *fakeDBTX) *Store {
	t.Helper()
	return &Store{q: sqlc.New(fake), runDir: t.TempDir(), turnCapBytes: 65536}
}

func mustUUID(t *testing.T) (string, pgtype.UUID) {
	t.Helper()
	u := uuid.Must(uuid.NewV7())
	return u.String(), pgtype.UUID{Bytes: u, Valid: true}
}

// conversationRowValues returns the 12 positional Scan values GetConversation /
// CreateConversation expect, matching AuraConversations field order.
func conversationRowValues(id, identity pgtype.UUID, status, model string, titleSet bool, title string) []any {
	cost := pgtype.Numeric{Int: big.NewInt(1234), Exp: -4, Valid: true} // 0.1234
	created := pgtype.Timestamptz{Time: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC), Valid: true}
	return []any{
		id, // ID
		pgtype.Text{String: title, Valid: titleSet}, // Title
		identity,     // IdentityID
		created,      // CreatedAt
		created,      // LastActiveAt
		status,       // Status
		model,        // Model
		int64(10),    // TotalInputTokens
		int64(20),    // TotalOutputTokens
		int64(5),     // TotalCachedTokens
		cost,         // TotalCostUsd
		[]byte(`{}`), // Metadata
	}
}

// TestCreate_FakeSuccess proves Create projects a returned row onto Conversation,
// including the numeric cost and title-set boundary conversion.
func TestCreate_FakeSuccess(t *testing.T) {
	t.Parallel()
	id, idU := mustUUID(t)
	_, identU := mustUUID(t)
	fake := &fakeDBTX{queryRowVal: &fakeRow{
		values: conversationRowValues(idU, identU, StatusActive, "deepseek", true, "My chat"),
	}}
	s := fakeStore(t, fake)

	got, err := s.Create(context.Background(), CreateParams{ID: id, IdentityID: uuid.Must(uuid.NewV7()).String(), Model: "deepseek"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID != id || got.Status != StatusActive || got.Model != "deepseek" {
		t.Errorf("projection wrong: %+v", got)
	}
	if !got.TitleSet || got.Title != "My chat" {
		t.Errorf("title projection wrong: set=%v title=%q", got.TitleSet, got.Title)
	}
	if got.TotalInputTokens != 10 || got.TotalOutputTokens != 20 || got.TotalCachedTokens != 5 {
		t.Errorf("token aggregates wrong: %+v", got)
	}
	if got.TotalCostUSD < 0.1233 || got.TotalCostUSD > 0.1235 {
		t.Errorf("cost projection wrong: %v", got.TotalCostUSD)
	}
	if got.CreatedAt != "2026-05-30T12:00:00Z" {
		t.Errorf("created_at projection wrong: %q", got.CreatedAt)
	}
}

// TestCreate_FakeDBError exercises the Create "%w" wrap on a failing QueryRow scan.
func TestCreate_FakeDBError(t *testing.T) {
	t.Parallel()
	boom := errors.New("insert failed")
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: boom}}
	s := fakeStore(t, fake)
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := s.Create(context.Background(), CreateParams{ID: id, IdentityID: id, Model: "m"}); !errors.Is(err, boom) {
		t.Errorf("Create DB error must propagate: %v", err)
	}
}

// TestGet_FakeSuccess proves a found row projects; a NULL title renders untitled.
func TestGet_FakeSuccess(t *testing.T) {
	t.Parallel()
	id, idU := mustUUID(t)
	_, identU := mustUUID(t)
	fake := &fakeDBTX{queryRowVal: &fakeRow{
		values: conversationRowValues(idU, identU, StatusArchived, "m", false, ""),
	}}
	s := fakeStore(t, fake)
	got, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusArchived || got.TitleSet {
		t.Errorf("projection wrong: %+v", got)
	}
	if dt := got.DisplayTitle(); dt != "(untitled 2026-05-30T12:00:00Z)" {
		t.Errorf("null title must render untitled: %q", dt)
	}
}

// TestGet_FakeNotFound maps pgx.ErrNoRows to the ErrConversationNotFound sentinel.
func TestGet_FakeNotFound(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: pgx.ErrNoRows}}
	s := fakeStore(t, fake)
	_, err := s.Get(context.Background(), uuid.Must(uuid.NewV7()).String())
	if !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("want ErrConversationNotFound, got %v", err)
	}
}

// TestGet_FakeOtherDBError keeps a non-ErrNoRows failure as a wrapped error (not the
// not-found sentinel).
func TestGet_FakeOtherDBError(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection reset")
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: boom}}
	s := fakeStore(t, fake)
	_, err := s.Get(context.Background(), uuid.Must(uuid.NewV7()).String())
	if errors.Is(err, ErrConversationNotFound) {
		t.Errorf("non-ErrNoRows must NOT map to not-found: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("other DB error must propagate: %v", err)
	}
}

// TestList_FakeSuccess projects multiple rows in order.
func TestList_FakeSuccess(t *testing.T) {
	t.Parallel()
	_, a := mustUUID(t)
	_, b := mustUUID(t)
	_, ident := mustUUID(t)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		conversationRowValues(a, ident, StatusActive, "m", true, "first"),
		conversationRowValues(b, ident, StatusArchived, "m", true, "second"),
	}}}
	s := fakeStore(t, fake)
	got, err := s.List(context.Background(), true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].Title != "first" || got[1].Title != "second" {
		t.Errorf("List projection wrong: %+v", got)
	}
}

// TestList_FakeQueryError wraps a Query failure.
func TestList_FakeQueryError(t *testing.T) {
	t.Parallel()
	boom := errors.New("query failed")
	s := fakeStore(t, &fakeDBTX{queryErr: boom})
	if _, err := s.List(context.Background(), false); !errors.Is(err, boom) {
		t.Errorf("List error must propagate: %v", err)
	}
}

// TestList_FakeRowsErr wraps a post-iteration rows.Err().
func TestList_FakeRowsErr(t *testing.T) {
	t.Parallel()
	boom := errors.New("rows broke mid-scan")
	s := fakeStore(t, &fakeDBTX{queryRows: &fakeRows{rowsErr: boom}})
	if _, err := s.List(context.Background(), false); !errors.Is(err, boom) {
		t.Errorf("List rows.Err must propagate: %v", err)
	}
}

// TestUpdateStatus_Fake covers the valid path, the invalid-status reject (no DB
// round-trip), and the DB error wrap.
func TestUpdateStatus_Fake(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()

	s := fakeStore(t, &fakeDBTX{})
	if err := s.UpdateStatus(context.Background(), id, StatusArchived); err != nil {
		t.Errorf("UpdateStatus(valid): %v", err)
	}

	// Invalid status is rejected before the DB round-trip.
	if err := s.UpdateStatus(context.Background(), id, "garbage"); err == nil {
		t.Error("UpdateStatus(invalid): want error")
	}

	boom := errors.New("update failed")
	sErr := fakeStore(t, &fakeDBTX{execErr: boom})
	if err := sErr.UpdateStatus(context.Background(), id, StatusDeleted); !errors.Is(err, boom) {
		t.Errorf("UpdateStatus DB error must propagate: %v", err)
	}
}

// TestRenameAndSetTitle_Fake cover the two Exec-backed title mutators (success +
// DB-error wrap).
func TestRenameAndSetTitle_Fake(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()

	ok := fakeStore(t, &fakeDBTX{})
	if err := ok.Rename(context.Background(), id, "new title"); err != nil {
		t.Errorf("Rename: %v", err)
	}
	if err := ok.SetTitleIfNull(context.Background(), id, "auto title"); err != nil {
		t.Errorf("SetTitleIfNull: %v", err)
	}

	boom := errors.New("write failed")
	bad := fakeStore(t, &fakeDBTX{execErr: boom})
	if err := bad.Rename(context.Background(), id, "x"); !errors.Is(err, boom) {
		t.Errorf("Rename DB error must propagate: %v", err)
	}
	if err := bad.SetTitleIfNull(context.Background(), id, "x"); !errors.Is(err, boom) {
		t.Errorf("SetTitleIfNull DB error must propagate: %v", err)
	}
}

// TestCountTurns_Fake covers the count projection + DB error wrap.
func TestCountTurns_Fake(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()

	fake := &fakeDBTX{queryRowVal: &fakeRow{values: []any{int64(7)}}}
	s := fakeStore(t, fake)
	if n, err := s.CountTurns(context.Background(), id); err != nil || n != 7 {
		t.Errorf("CountTurns: n=%d err=%v (want 7)", n, err)
	}

	boom := errors.New("count failed")
	sErr := fakeStore(t, &fakeDBTX{queryRowVal: &fakeRow{scanErr: boom}})
	if _, err := sErr.CountTurns(context.Background(), id); !errors.Is(err, boom) {
		t.Errorf("CountTurns DB error must propagate: %v", err)
	}
}

// turnRowValues returns the 11 positional Scan values ListTurnsBySeq expects,
// matching AuraConversationTurns field order.
func turnRowValues(conv pgtype.UUID, seq int32, role, content, sidecar, toolCallID string, toolCalls []byte) []any {
	created := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	return []any{
		conv, // ConversationID
		seq,  // Seq
		role, // Role
		pgtype.Text{String: content, Valid: content != ""},       // Content
		pgtype.Text{String: sidecar, Valid: sidecar != ""},       // ContentSidecarPath
		pgtype.Text{String: toolCallID, Valid: toolCallID != ""}, // ToolCallID
		toolCalls, // ToolCalls
		created,   // CreatedAt
		int32(3),  // InputTokens
		int32(4),  // OutputTokens
		int32(1),  // CachedTokens
	}
}

// TestLoadHistory_FakeInlineTurns reconstructs llm.Messages from inline turn rows.
func TestLoadHistory_FakeInlineTurns(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	calls := []byte(`[{"id":"call_1","type":"function","function":{"name":"text_response","arguments":"{}"}}]`)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "system", "you are aura", "", "", nil),
		turnRowValues(conv, 2, "user", "hi", "", "", nil),
		turnRowValues(conv, 3, "assistant", "", "", "", calls),
		turnRowValues(conv, 4, "tool", "ok", "", "call_1", nil),
	}}}
	s := fakeStore(t, fake)
	msgs, err := s.LoadHistory(context.Background(), uuid.Must(uuid.NewV7()).String())
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	// repairToolMessagePairs preserves the valid assistant->tool group.
	if len(msgs) != 4 {
		t.Fatalf("history length = %d, want 4: %+v", len(msgs), msgs)
	}
	if msgs[2].Role != "assistant" || len(msgs[2].ToolCalls) != 1 || msgs[2].ToolCalls[0].ID != "call_1" {
		t.Errorf("assistant tool_calls projection wrong: %+v", msgs[2])
	}
	if msgs[3].Role != "tool" || msgs[3].ToolCallID != "call_1" || msgs[3].Content != "ok" {
		t.Errorf("tool turn projection wrong: %+v", msgs[3])
	}
}

// TestLoadHistory_FakeSidecarRehydrate proves loadTurns reads spilled content from
// disk when a row carries a content_sidecar_path.
func TestLoadHistory_FakeSidecarRehydrate(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "3.content")
	if err := os.WriteFile(sidecar, []byte("spilled body"), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "user", "small", "", "", nil),
		turnRowValues(conv, 3, "assistant", "", sidecar, "", nil),
	}}}
	s := &Store{q: sqlc.New(fake), runDir: dir, turnCapBytes: 65536}
	msgs, err := s.LoadHistory(context.Background(), uuid.Must(uuid.NewV7()).String())
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Content != "spilled body" {
		t.Errorf("sidecar content must be rehydrated: %+v", msgs)
	}
}

// TestLoadHistory_FakeSidecarMissing surfaces the read error when a row points at a
// sidecar that is gone.
func TestLoadHistory_FakeSidecarMissing(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	missing := filepath.Join(t.TempDir(), "999.content")
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "assistant", "", missing, "", nil),
	}}}
	s := &Store{q: sqlc.New(fake), runDir: t.TempDir(), turnCapBytes: 65536}
	if _, err := s.LoadHistory(context.Background(), uuid.Must(uuid.NewV7()).String()); err == nil {
		t.Error("LoadHistory(missing sidecar): want read error")
	}
}

// TestLoadHistory_FakeMalformedToolCalls surfaces the per-turn decode error.
func TestLoadHistory_FakeMalformedToolCalls(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "assistant", "", "", "", []byte("{not json")),
	}}}
	s := fakeStore(t, fake)
	if _, err := s.LoadHistory(context.Background(), uuid.Must(uuid.NewV7()).String()); err == nil {
		t.Error("LoadHistory(malformed tool_calls): want decode error")
	}
}

// TestLoadHistory_FakeQueryError wraps the ListTurnsBySeq query failure.
func TestLoadHistory_FakeQueryError(t *testing.T) {
	t.Parallel()
	boom := errors.New("list turns failed")
	s := fakeStore(t, &fakeDBTX{queryErr: boom})
	if _, err := s.LoadHistory(context.Background(), uuid.Must(uuid.NewV7()).String()); !errors.Is(err, boom) {
		t.Errorf("LoadHistory query error must propagate: %v", err)
	}
}

// TestLoadHistory_FakePartiallyRecoversToolGroup proves the crash-recovery path: an
// assistant turn with TWO tool calls but only ONE persisted tool result keeps the
// matching result and synthesizes a recovery marker for the missing one. This drives
// appendRecoveredToolResultGroup (the partial group) AND toolCallHasID (matching the
// present result against the assistant's call ids).
func TestLoadHistory_FakePartiallyRecoversToolGroup(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	calls := []byte(`[` +
		`{"id":"call_present","type":"function","function":{"name":"shell_exec","arguments":"{}"}},` +
		`{"id":"call_missing","type":"function","function":{"name":"fs_read","arguments":"{}"}}` +
		`]`)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "user", "start", "", "", nil),
		turnRowValues(conv, 2, "assistant", "", "", "", calls),
		turnRowValues(conv, 3, "tool", "the present result", "", "call_present", nil),
		turnRowValues(conv, 4, "user", "after crash", "", "", nil),
	}}}
	s := fakeStore(t, fake)
	msgs, err := s.LoadHistory(context.Background(), uuid.Must(uuid.NewV7()).String())
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	var sawPresent, sawRecovery bool
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_present" && m.Content == "the present result" {
			sawPresent = true
		}
		if m.Role == "tool" && m.ToolCallID == "call_missing" &&
			strings.Contains(m.Content, "previous result unknown") {
			sawRecovery = true
		}
	}
	if !sawPresent {
		t.Errorf("present tool result must be preserved: %+v", msgs)
	}
	if !sawRecovery {
		t.Errorf("missing tool call must get a recovery marker on reload: %+v", msgs)
	}
}

// TestLoadManagedHistory_FakeAppliesLadder proves the managed-history entry point
// loads turns via the fake, runs the offline encoder, and applies the L1/L2/L2.5
// ladder (here a small history stays under the hard cap → returned as-is, no rot row).
func TestLoadManagedHistory_FakeAppliesLadder(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "system", "you are aura", "", "", nil),
		turnRowValues(conv, 2, "user", "hello", "", "", nil),
		turnRowValues(conv, 3, "assistant", "hi there", "", "", nil),
	}}}
	s := fakeStore(t, fake)
	msgs, err := s.LoadManagedHistory(context.Background(), uuid.Must(uuid.NewV7()).String(),
		ContextConfig{ContextWindow: 200000, MaxOutputTokens: 4096, ToolEvictAfterTurns: 10})
	if err != nil {
		t.Fatalf("LoadManagedHistory: %v", err)
	}
	if len(msgs) != 3 || msgs[0].Role != "system" || msgs[2].Content != "hi there" {
		t.Errorf("managed history projection wrong: %+v", msgs)
	}
}

// TestLoadManagedHistory_FakeLoadError surfaces the loadTurns query failure before any
// ladder work.
func TestLoadManagedHistory_FakeLoadError(t *testing.T) {
	t.Parallel()
	boom := errors.New("load turns failed")
	s := fakeStore(t, &fakeDBTX{queryErr: boom})
	if _, err := s.LoadManagedHistory(context.Background(), uuid.Must(uuid.NewV7()).String(),
		ContextConfig{ContextWindow: 1000, MaxOutputTokens: 1}); !errors.Is(err, boom) {
		t.Errorf("LoadManagedHistory load error must propagate: %v", err)
	}
}
