//go:build db_integration

// Integration tests for internal/askuser. Requires a running Postgres with the
// Phase-4 migrations applied through 0005 (0005 promotes paused_states.conversation_id
// to uuid + FK and adds resumed_answer):
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via (FIFO determinism wants -count=10):
//
//	go test -tags db_integration -race ./internal/askuser -count=10
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset.
package askuser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isPauseNotFound(err error) bool { return errors.Is(err, ErrPauseNotFound) }

func pgTimestamp(ts time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: ts, Valid: true}
}

// localID is the seeded `local` identity (migration 0004) used as the FK parent
// for the throwaway conversations these tests create.
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

// newConversation inserts a throwaway conversation (FK parent for paused_states)
// and registers cascade cleanup. Returns its UUID.
func newConversation(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.conversations (id, identity_id, model) VALUES ($1, $2, 'test-model')",
		id, localID); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.conversations WHERE id = $1", id)
	})
	return id
}

func insertPause(t *testing.T, s *Store, convID, kind, question string, priority int) string {
	t.Helper()
	token := uuid.Must(uuid.NewV7()).String()
	if err := s.Insert(context.Background(), InsertParams{
		Token:          token,
		ConversationID: convID,
		Kind:           kind,
		Question:       question,
		Priority:       priority,
		ToolCallID:     "call_" + token[:8],
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return token
}

func countPending(t *testing.T, pool *pgxpool.Pool, convID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.paused_states WHERE conversation_id = $1 AND resumed_at IS NULL", convID,
	).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	return n
}

// TestListPending_TotalOrderViaTokenTiebreaker: 3 equal-priority rows inserted in
// ONE tx share created_at = now() (the transaction timestamp), so created_at ASC
// ties and the token ASC tiebreaker decides the order (RESEARCH Pitfall 4). The
// rows are inserted in a deliberately non-sorted token order (333, 111, 222) so a
// missing tiebreaker (insertion-order leak) is caught. Run under -count=10.
func TestListPending_TotalOrderViaTokenTiebreaker(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	tokens := []string{
		"33333333-3333-3333-3333-333333333333",
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	}
	// Insert all three in a single transaction so created_at = now() is identical
	// across the rows (now() is the transaction start time in Postgres). Only then
	// does the token ASC tiebreaker actually decide the order.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, tok := range tokens {
		if _, err := tx.Exec(ctx,
			`INSERT INTO aura.paused_states (token, conversation_id, kind, question, priority, tool_call_id)
			 VALUES ($1, $2, 'approval', 'q', 5, 'tc')`, tok, convID); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("tx insert %s: %v", tok, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	pending, err := s.ListPending(ctx, convID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("want 3 pending, got %d", len(pending))
	}
	want := []string{tokens[1], tokens[2], tokens[0]} // 111 < 222 < 333 (token ASC)
	for i, p := range pending {
		if p.Token != want[i] {
			t.Errorf("position %d: want token %s, got %s (token-ASC tiebreaker broken)", i, want[i], p.Token)
		}
	}
}

// TestListPending_PriorityDominatesThenCreatedAt asserts priority DESC wins over
// the token tiebreaker.
func TestListPending_PriorityDominatesThenCreatedAt(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	lowTok := insertPause(t, s, convID, "approval", "low", 1)
	highTok := insertPause(t, s, convID, "approval", "high", 90)

	pending, err := s.ListPending(ctx, convID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 || pending[0].Token != highTok || pending[1].Token != lowTok {
		t.Fatalf("priority DESC order broken: got %+v (want high=%s first)", pending, highTok)
	}
}

// TestCrashRecovery_FreshStoreSeesPriorRows: rows inserted by one Store instance
// are returned by a fresh New(pool) — durability lives in Postgres, not memory.
func TestCrashRecovery_FreshStoreSeesPriorRows(t *testing.T) {
	pool := migratedPool(t)
	convID := newConversation(t, pool)

	s1 := New(pool)
	tok := insertPause(t, s1, convID, "clarification", "still here?", 0)

	s2 := New(pool) // simulates a process restart
	pending, err := s2.ListPending(context.Background(), convID)
	if err != nil {
		t.Fatalf("ListPending (fresh store): %v", err)
	}
	if len(pending) != 1 || pending[0].Token != tok {
		t.Fatalf("fresh Store must see the persisted pause %s, got %+v", tok, pending)
	}
}

// TestMarkResumed_InvalidTokenRejected: an unknown / already-resumed token returns
// ErrPauseNotFound (a clear error, never a silent no-op).
func TestMarkResumed_InvalidTokenRejected(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()

	unknown := uuid.Must(uuid.NewV7()).String()
	if err := s.MarkResumed(ctx, unknown, ResumeAnswer{Action: ActionAccept, Content: "x"}); !isPauseNotFound(err) {
		t.Errorf("MarkResumed(unknown): want ErrPauseNotFound, got %v", err)
	}

	convID := newConversation(t, pool)
	tok := insertPause(t, s, convID, "approval", "q", 0)
	if err := s.MarkResumed(ctx, tok, ResumeAnswer{Action: ActionAccept, Content: "yes"}); err != nil {
		t.Fatalf("first MarkResumed: %v", err)
	}
	// Second resume of the same token is now a no-op row → ErrPauseNotFound.
	if err := s.MarkResumed(ctx, tok, ResumeAnswer{Action: ActionAccept, Content: "again"}); !isPauseNotFound(err) {
		t.Errorf("MarkResumed(already-resumed): want ErrPauseNotFound, got %v", err)
	}
}

// TestMarkResumedBatch_ResolvesMany resolves several tokens atomically and leaves
// zero pending.
func TestMarkResumedBatch_ResolvesMany(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	t1 := insertPause(t, s, convID, "approval", "a", 0)
	t2 := insertPause(t, s, convID, "clarification", "b", 0)

	if err := s.MarkResumedBatch(ctx, map[string]ResumeAnswer{
		t1: {Action: ActionAccept, Content: "yes"},
		t2: {Action: ActionDecline, Content: ""},
	}); err != nil {
		t.Fatalf("MarkResumedBatch: %v", err)
	}
	if n := countPending(t, pool, convID); n != 0 {
		t.Errorf("after batch resume: want 0 pending, got %d", n)
	}
}

// TestMarkResumedBatch_UnknownTokenRollsBack: if any token is unknown the whole
// batch rolls back (no partial resolution).
func TestMarkResumedBatch_UnknownTokenRollsBack(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	good := insertPause(t, s, convID, "approval", "a", 0)
	unknown := uuid.Must(uuid.NewV7()).String()

	err := s.MarkResumedBatch(ctx, map[string]ResumeAnswer{
		good:    {Action: ActionAccept, Content: "yes"},
		unknown: {Action: ActionAccept, Content: "no-such"},
	})
	if !isPauseNotFound(err) {
		t.Fatalf("batch with an unknown token: want ErrPauseNotFound, got %v", err)
	}
	// The good token must still be pending (rollback).
	if n := countPending(t, pool, convID); n != 1 {
		t.Errorf("rollback must leave the good token pending, got %d pending", n)
	}
}

// TestAutoResolveForConversation: Loop.Stop leaves zero pending for the conversation.
func TestAutoResolveForConversation(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	insertPause(t, s, convID, "approval", "a", 0)
	insertPause(t, s, convID, "choice", "b", 50)

	if err := s.AutoResolveForConversation(ctx, convID); err != nil {
		t.Fatalf("AutoResolveForConversation: %v", err)
	}
	if n := countPending(t, pool, convID); n != 0 {
		t.Errorf("after auto-resolve: want 0 pending, got %d", n)
	}

	// The auto-resolved rows carry the cancel marker (AM-02).
	var answer string
	if err := pool.QueryRow(ctx,
		"SELECT resumed_answer->>'action' FROM aura.paused_states WHERE conversation_id = $1 LIMIT 1", convID,
	).Scan(&answer); err != nil {
		t.Fatalf("read resumed_answer: %v", err)
	}
	if answer != ActionCancel {
		t.Errorf("auto-resolve action: want %q, got %q", ActionCancel, answer)
	}
}

// TestGetByToken_RoundTrip + not-found classification.
func TestGetByToken_RoundTrip(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	tok := insertPause(t, s, convID, "choice", "pick one", 7)
	got, err := s.GetByToken(ctx, tok)
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if got.Token != tok || got.ConversationID != convID || got.Kind != "choice" || got.Priority != 7 {
		t.Errorf("GetByToken round-trip mismatch: %+v", got)
	}

	if _, err := s.GetByToken(ctx, uuid.Must(uuid.NewV7()).String()); !isPauseNotFound(err) {
		t.Errorf("GetByToken(absent): want ErrPauseNotFound, got %v", err)
	}
}

// TestStoreMethods_DBErrorWrapping drives the DB-touching methods against a
// canceled context so each "%w" wrap branch is exercised (mirrors identity).
func TestStoreMethods_DBErrorWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	valid := uuid.Must(uuid.NewV7()).String()
	if err := s.Insert(canceled, InsertParams{Token: valid, ConversationID: convID, Kind: "approval", Question: "q", ToolCallID: "tc"}); err == nil {
		t.Error("Insert(canceled): want error, got nil")
	}
	if _, err := s.GetByToken(canceled, valid); err == nil {
		t.Error("GetByToken(canceled): want error, got nil")
	}
	if _, err := s.ListPending(canceled, convID); err == nil {
		t.Error("ListPending(canceled): want error, got nil")
	}
	if err := s.MarkResumed(canceled, valid, ResumeAnswer{Action: ActionAccept}); err == nil {
		t.Error("MarkResumed(canceled): want error, got nil")
	}
	if err := s.MarkResumedBatch(canceled, map[string]ResumeAnswer{valid: {Action: ActionAccept}}); err == nil {
		t.Error("MarkResumedBatch(canceled): want error, got nil")
	}
	if err := s.AutoResolveForConversation(canceled, convID); err == nil {
		t.Error("AutoResolveForConversation(canceled): want error, got nil")
	}
}

// TestCleanupResumedOlderThan deletes resolved rows older than the cutoff, leaving
// pending rows untouched.
func TestCleanupResumedOlderThan(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := context.Background()

	resolved := insertPause(t, s, convID, "approval", "old", 0)
	if err := s.MarkResumed(ctx, resolved, ResumeAnswer{Action: ActionAccept, Content: "ok"}); err != nil {
		t.Fatalf("MarkResumed: %v", err)
	}
	pendingTok := insertPause(t, s, convID, "approval", "keep", 0)

	future := pgTimestamp(time.Now().Add(time.Hour))
	if err := s.CleanupResumedOlderThan(ctx, future); err != nil {
		t.Fatalf("CleanupResumedOlderThan: %v", err)
	}

	var resolvedCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM aura.paused_states WHERE token = $1", resolved,
	).Scan(&resolvedCount); err != nil {
		t.Fatalf("count resolved: %v", err)
	}
	if resolvedCount != 0 {
		t.Errorf("resolved row must be cleaned up, got %d", resolvedCount)
	}
	// The pending row survives.
	if _, err := s.GetByToken(ctx, pendingTok); err != nil {
		t.Errorf("pending row must survive cleanup: %v", err)
	}
}
