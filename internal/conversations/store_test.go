//go:build db_integration

// Integration tests for internal/conversations. Requires a running Postgres with
// the Phase-4 migrations applied through 0006:
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/conversations -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset.
package conversations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const localID = "00000000-0000-0000-0000-000000000001"

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

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := envOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)

	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
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

func newStore(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	runDir := t.TempDir()
	return New(pool, Config{RunDir: runDir, TurnCapBytes: 65536})
}

func newConversation(t *testing.T, s *Store) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := s.Create(context.Background(), CreateParams{
		ID: id, IdentityID: localID, Model: "test-model",
	}); err != nil {
		t.Fatalf("Create conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM aura.conversations WHERE id = $1", id)
	})
	return id
}

func countTurns(t *testing.T, pool *pgxpool.Pool, convID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.conversation_turns WHERE conversation_id = $1", convID,
	).Scan(&n); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	return n
}

// TestLoadHistory_ByteIdenticalAfterRestart persists 3 turns, opens a FRESH Store
// (process-restart analog), and asserts two LoadHistory calls return byte-identical
// slices (Req#8 / SC-2 deterministic resume).
func TestLoadHistory_ByteIdenticalAfterRestart(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "hello"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "hi there",
			InputTokens: 10, OutputTokens: 5, CachedTokens: 2, CostUSD: 0.0010},
	}
	for _, tp := range turns {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}

	s2 := New(pool, Config{RunDir: s.runDir, TurnCapBytes: s.turnCapBytes}) // restart
	h1, err := s2.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory#1: %v", err)
	}
	h2, err := s2.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory#2: %v", err)
	}
	if len(h1) != 3 {
		t.Fatalf("want 3 turns, got %d", len(h1))
	}
	if fmt.Sprintf("%#v", h1) != fmt.Sprintf("%#v", h2) {
		t.Errorf("LoadHistory not byte-identical across calls:\n#1 %#v\n#2 %#v", h1, h2)
	}
	if h1[0].Role != llm.RoleSystem || h1[2].Content != "hi there" {
		t.Errorf("history reconstruction wrong: %+v", h1)
	}
}

// TestAppendTurn_AtomicRollback proves SC-2: a failure injected between the turn
// INSERT and the aggregates UPDATE (same statements AppendTurn runs, inside one
// db.WithTx) rolls back, leaving NO partial turn.
func TestAppendTurn_AtomicRollback(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	before := countTurns(t, pool, convID)

	id, _ := db.ParseUUID("id", convID)
	injected := errors.New("injected failure between INSERT and UPDATE")
	err := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
		if ierr := q.InsertConversationTurn(ctx, sqlc.InsertConversationTurnParams{
			ConversationID: id, Seq: 99, Role: llm.RoleUser,
			Content: pgtype.Text{String: "doomed turn", Valid: true},
		}); ierr != nil {
			return ierr
		}
		// Fail here — AFTER the INSERT, BEFORE the aggregates UPDATE.
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("want injected error, got %v", err)
	}
	if after := countTurns(t, pool, convID); after != before {
		t.Errorf("SC-2 violated: turn count %d -> %d (partial turn survived a rollback)", before, after)
	}
}

// TestAppendTurn_SidecarSpill: content > cap spills to a sidecar file with
// content=NULL + content_sidecar_path set, and LoadHistory rehydrates the full
// bytes from disk.
func TestAppendTurn_SidecarSpill(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	s := New(pool, Config{RunDir: runDir, TurnCapBytes: 64}) // tiny cap to force a spill
	convID := newConversation(t, s)
	ctx := context.Background()

	big := strings.Repeat("x", 200)
	if err := s.AppendTurn(ctx, AppendTurnParams{
		ConversationID: convID, Seq: 1, Role: llm.RoleAssistant, Content: big,
	}); err != nil {
		t.Fatalf("AppendTurn (spill): %v", err)
	}

	var content, path *string
	if err := pool.QueryRow(ctx,
		"SELECT content, content_sidecar_path FROM aura.conversation_turns WHERE conversation_id=$1 AND seq=1",
		convID,
	).Scan(&content, &path); err != nil {
		t.Fatalf("read turn: %v", err)
	}
	if content != nil {
		t.Errorf("spilled turn must have content NULL, got %q", *content)
	}
	if path == nil {
		t.Fatal("spilled turn must have content_sidecar_path set")
	}
	wantPath := filepath.Join(runDir, "conversations", convID, "1.content")
	if *path != wantPath {
		t.Errorf("sidecar path: want %q, got %q", wantPath, *path)
	}
	if _, err := os.Stat(*path); err != nil {
		t.Errorf("sidecar file must exist: %v", err)
	}

	h, err := s.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h) != 1 || h[0].Content != big {
		t.Errorf("LoadHistory must rehydrate spilled content (len=%d)", len(h[0].Content))
	}
}

// TestAppendTurn_AggregatesSumTurnColumns: after a multi-turn run the conversation
// aggregates equal the sum of the per-turn token columns and total_cost_usd > 0.
func TestAppendTurn_AggregatesSumTurnColumns(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	deltas := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: "a", InputTokens: 100, OutputTokens: 0, CachedTokens: 10, CostUSD: 0.0050},
		{ConversationID: convID, Seq: 2, Role: llm.RoleAssistant, Content: "b", InputTokens: 50, OutputTokens: 200, CachedTokens: 5, CostUSD: 0.0100},
	}
	for _, d := range deltas {
		if err := s.AppendTurn(ctx, d); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", d.Seq, err)
		}
	}

	c, err := s.Get(ctx, convID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c.TotalInputTokens != 150 || c.TotalOutputTokens != 200 || c.TotalCachedTokens != 15 {
		t.Errorf("token aggregates wrong: %+v", c)
	}
	if c.TotalCostUSD <= 0 {
		t.Errorf("total_cost_usd must be > 0, got %v", c.TotalCostUSD)
	}
	// 0.0050 + 0.0100 = 0.0150 at numeric(10,4).
	if diff := c.TotalCostUSD - 0.0150; diff > 5e-5 || diff < -5e-5 {
		t.Errorf("total_cost_usd: want ~0.0150, got %v", c.TotalCostUSD)
	}
}

// TestSearchConversationTurns_OrderedBySimilarity asserts the locked FTS query
// returns matches ordered by similarity DESC from the pg_trgm GIN index.
func TestSearchConversationTurns_OrderedBySimilarity(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	rows := []string{
		"the quick brown fox jumps",
		"quick brown foxes are quick",
		"completely unrelated content about databases",
	}
	for i, content := range rows {
		if err := s.AppendTurn(ctx, AppendTurnParams{
			ConversationID: convID, Seq: i + 1, Role: llm.RoleUser, Content: content,
		}); err != nil {
			t.Fatalf("AppendTurn: %v", err)
		}
	}

	res, err := s.SearchConversationTurns(ctx, "quick brown fox", 10)
	if err != nil {
		t.Fatalf("SearchConversationTurns: %v", err)
	}
	if len(res) < 2 {
		t.Fatalf("want >= 2 matches, got %d", len(res))
	}
	for i := 1; i < len(res); i++ {
		if res[i-1].Similarity < res[i].Similarity {
			t.Errorf("results not ordered by similarity DESC: %v before %v", res[i-1].Similarity, res[i].Similarity)
		}
	}
}

func TestSearchConversationTurnsExcludesDeletedConversations(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	activeID := newConversation(t, s)
	deletedID := newConversation(t, s)
	ctx := context.Background()
	needle := "needle" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")

	for _, convID := range []string{activeID, deletedID} {
		if err := s.AppendTurn(ctx, AppendTurnParams{
			ConversationID: convID,
			Seq:            1,
			Role:           llm.RoleUser,
			Content:        needle + " searchable phrase",
		}); err != nil {
			t.Fatalf("AppendTurn %s: %v", convID, err)
		}
	}
	if err := s.UpdateStatus(ctx, deletedID, StatusDeleted); err != nil {
		t.Fatalf("delete status: %v", err)
	}

	res, err := s.SearchConversationTurns(ctx, needle, 10)
	if err != nil {
		t.Fatalf("SearchConversationTurns: %v", err)
	}
	if !containsSearchResult(res, activeID) {
		t.Fatalf("active conversation result missing: %+v", res)
	}
	if containsSearchResult(res, deletedID) {
		t.Fatalf("deleted conversation leaked into search results: %+v", res)
	}

	raw, err := os.ReadFile(filepath.Join("..", "db", "queries", "conversation_turns.sql"))
	if err != nil {
		t.Fatalf("read locked query file: %v", err)
	}
	locked := "SELECT conversation_id, seq, content, similarity(content, $1) AS sim\n" +
		"FROM aura.conversation_turns\n" +
		"WHERE content % $1\n" +
		"ORDER BY similarity(content, $1) DESC\n" +
		"LIMIT $2;"
	if !strings.Contains(string(raw), locked) {
		t.Fatalf("locked SearchConversationTurns SQL clause changed:\n%s", raw)
	}
}

func containsSearchResult(results []SearchResult, id string) bool {
	for _, r := range results {
		if r.ConversationID == id {
			return true
		}
	}
	return false
}

// TestDelete_CascadesAndRemovesSidecar: Delete cascade-removes turns + paused_states
// and os.RemoveAll's the sidecar dir after commit.
func TestDelete_CascadesAndRemovesSidecar(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	s := New(pool, Config{RunDir: runDir, TurnCapBytes: 16})
	convID := newConversation(t, s)
	ctx := context.Background()

	if err := s.AppendTurn(ctx, AppendTurnParams{
		ConversationID: convID, Seq: 1, Role: llm.RoleAssistant, Content: strings.Repeat("y", 100),
	}); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	// A pending paused_state for the same conversation (cascade target).
	tok := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.paused_states (token, conversation_id, kind, question, priority, tool_call_id)
		 VALUES ($1, $2, 'approval', 'q', 0, 'tc')`, tok, convID); err != nil {
		t.Fatalf("insert paused_state: %v", err)
	}
	dir := filepath.Join(runDir, "conversations", convID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("sidecar dir should exist before delete: %v", err)
	}

	if err := s.Delete(ctx, convID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n := countTurns(t, pool, convID); n != 0 {
		t.Errorf("turns must cascade-delete, got %d", n)
	}
	var pausedCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM aura.paused_states WHERE conversation_id=$1", convID).Scan(&pausedCount); err != nil {
		t.Fatalf("count paused: %v", err)
	}
	if pausedCount != 0 {
		t.Errorf("paused_states must cascade-delete, got %d", pausedCount)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("sidecar dir must be removed after Delete, stat err=%v", err)
	}
}

// TestStatusAndRename exercises archive/unarchive/delete status flips + rename +
// SetTitleIfNull idempotency + List filtering.
func TestStatusAndRename(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	if err := s.SetTitleIfNull(ctx, convID, "first title"); err != nil {
		t.Fatalf("SetTitleIfNull: %v", err)
	}
	// Idempotent: a second SetTitleIfNull does NOT overwrite.
	if err := s.SetTitleIfNull(ctx, convID, "second title"); err != nil {
		t.Fatalf("SetTitleIfNull#2: %v", err)
	}
	c, _ := s.Get(ctx, convID)
	if c.Title != "first title" {
		t.Errorf("SetTitleIfNull must be idempotent, got %q", c.Title)
	}
	// Rename overwrites unconditionally.
	if err := s.Rename(ctx, convID, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	c, _ = s.Get(ctx, convID)
	if c.Title != "renamed" {
		t.Errorf("Rename: got %q", c.Title)
	}

	// Archive hides from the default List, surfaces with includeArchived.
	if err := s.UpdateStatus(ctx, convID, StatusArchived); err != nil {
		t.Fatalf("archive: %v", err)
	}
	active, _ := s.List(ctx, false)
	if containsConv(active, convID) {
		t.Error("archived conversation must not appear in active List")
	}
	all, _ := s.List(ctx, true)
	if !containsConv(all, convID) {
		t.Error("archived conversation must appear in includeArchived List")
	}

	// Invalid status rejected.
	if err := s.UpdateStatus(ctx, convID, "bogus"); err == nil {
		t.Error("invalid status must be rejected")
	}
}

func containsConv(cs []Conversation, id string) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}

// TestGet_NotFound maps a missing conversation to ErrConversationNotFound.
func TestGet_NotFound(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	_, err := s.Get(context.Background(), uuid.Must(uuid.NewV7()).String())
	if !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("want ErrConversationNotFound, got %v", err)
	}
}

// TestCountTurns counts persisted turns for a conversation.
func TestCountTurns(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	if n, err := s.CountTurns(ctx, convID); err != nil || n != 0 {
		t.Fatalf("CountTurns(empty): n=%d err=%v", n, err)
	}
	for seq := 1; seq <= 3; seq++ {
		if err := s.AppendTurn(ctx, AppendTurnParams{
			ConversationID: convID, Seq: seq, Role: llm.RoleUser, Content: "x",
		}); err != nil {
			t.Fatalf("AppendTurn: %v", err)
		}
	}
	if n, err := s.CountTurns(ctx, convID); err != nil || n != 3 {
		t.Errorf("CountTurns: n=%d err=%v (want 3)", n, err)
	}
}

// TestStoreMethods_DBErrorWrapping drives the DB-touching methods against a
// canceled context so each "%w" wrap branch is exercised (mirrors askuser).
func TestStoreMethods_DBErrorWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	newID := uuid.Must(uuid.NewV7()).String()
	if _, err := s.Create(canceled, CreateParams{ID: newID, IdentityID: localID, Model: "m"}); err == nil {
		t.Error("Create(canceled): want error")
	}
	if _, err := s.Get(canceled, convID); err == nil {
		t.Error("Get(canceled): want error")
	}
	if _, err := s.List(canceled, true); err == nil {
		t.Error("List(canceled): want error")
	}
	if err := s.UpdateStatus(canceled, convID, StatusArchived); err == nil {
		t.Error("UpdateStatus(canceled): want error")
	}
	if err := s.Rename(canceled, convID, "x"); err == nil {
		t.Error("Rename(canceled): want error")
	}
	if err := s.SetTitleIfNull(canceled, convID, "x"); err == nil {
		t.Error("SetTitleIfNull(canceled): want error")
	}
	if _, err := s.CountTurns(canceled, convID); err == nil {
		t.Error("CountTurns(canceled): want error")
	}
	if err := s.AppendTurn(canceled, AppendTurnParams{ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: "x"}); err == nil {
		t.Error("AppendTurn(canceled): want error")
	}
	if _, err := s.LoadHistory(canceled, convID); err == nil {
		t.Error("LoadHistory(canceled): want error")
	}
	if _, err := s.SearchConversationTurns(canceled, "q", 5); err == nil {
		t.Error("SearchConversationTurns(canceled): want error")
	}
	if err := s.Delete(canceled, convID); err == nil {
		t.Error("Delete(canceled): want error")
	}
}

// TestStoreMethods_InvalidUUIDWrapping exercises the parseUUID error branches.
func TestStoreMethods_InvalidUUIDWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	ctx := context.Background()
	const bad = "not-a-uuid"

	if _, err := s.Create(ctx, CreateParams{ID: bad, IdentityID: localID, Model: "m"}); err == nil {
		t.Error("Create(bad id): want error")
	}
	if _, err := s.Create(ctx, CreateParams{ID: uuid.Must(uuid.NewV7()).String(), IdentityID: bad, Model: "m"}); err == nil {
		t.Error("Create(bad identity): want error")
	}
	if _, err := s.Get(ctx, bad); err == nil {
		t.Error("Get(bad): want error")
	}
	if err := s.UpdateStatus(ctx, bad, StatusActive); err == nil {
		t.Error("UpdateStatus(bad): want error")
	}
	if err := s.Rename(ctx, bad, "x"); err == nil {
		t.Error("Rename(bad): want error")
	}
	if err := s.SetTitleIfNull(ctx, bad, "x"); err == nil {
		t.Error("SetTitleIfNull(bad): want error")
	}
	if _, err := s.CountTurns(ctx, bad); err == nil {
		t.Error("CountTurns(bad): want error")
	}
	if err := s.AppendTurn(ctx, AppendTurnParams{ConversationID: bad, Seq: 1, Role: llm.RoleUser}); err == nil {
		t.Error("AppendTurn(bad): want error")
	}
	if _, err := s.LoadHistory(ctx, bad); err == nil {
		t.Error("LoadHistory(bad): want error")
	}
	if err := s.Delete(ctx, bad); err == nil {
		t.Error("Delete(bad): want error")
	}
}
