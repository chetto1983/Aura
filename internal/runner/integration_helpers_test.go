//go:build db_integration

// Integration harness for internal/runner (LOOP-02/03/04 cross-store tx atomicity).
// Requires a running Postgres with the Phase-4+ migrations applied and the seeded `local`
// identity (0004). Run via:
//
//	go test -tags db_integration -race ./internal/runner/ -count=1
//	# or: bash scripts/run_runner_integration.sh -run 'Resume|Pause|Multipause'
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the composed DSN is unset. The
// PoolResumeCommitter's cross-store tx cannot be unit-faked (db.WithTx begins on a
// concrete pool), so atomic single/batch resume + pause-exposure atomicity live here.
package runner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localIdentityID is the seeded `local`/system identity (0004) every test conversation
// hangs off (FK: conversations.identity_id → identities.id).
const localIdentityID = "00000000-0000-0000-0000-000000000001"

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via scripts/run_runner_integration.sh)", key)
	}
	return v
}

// migratedRunnerPool opens the aura_app pool after EnsureRoles + Migrate, re-seeds the
// local identity (idempotent — a parallel session can wipe it), and inits the tiktoken
// encoder the real LoadManagedHistory needs.
func migratedRunnerPool(t *testing.T) *pgxpool.Pool {
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
	seedLocalIdentity(t, pool)
	if err := conversations.InitEncoder(); err != nil {
		t.Fatalf("InitEncoder: %v", err)
	}
	return pool
}

// seedLocalIdentity re-seeds the local identity + wildcard grant (idempotent). Migration
// 0004 seeds these, but a parallel session intermittently wipes them; re-seeding keeps the
// FK-dependent conversation/pause writes robust (an FK 23503 is an env fault, not a code
// defect).
func seedLocalIdentity(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, 'local', 'system') ON CONFLICT DO NOTHING`,
		localIdentityID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1, '*') ON CONFLICT DO NOTHING`,
		localIdentityID); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
}

// newIntegrationRunner builds a Runner over the REAL conversations/askuser Stores + the
// pool-owning PoolResumeCommitter, with a scripted fake client (the tx atomicity, not the
// LLM, is under test). Identity/cache-metric/tool-invocation deps stay in-memory fakes.
func newIntegrationRunner(t *testing.T, pool *pgxpool.Pool, client llm.Client) (*Runner, *conversations.Store, *askuser.Store) {
	t.Helper()
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	pauseStore := askuser.New(pool)
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	r := New(Deps{
		Conv:            convStore,
		Pause:           pauseStore,
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
		ResumeCommitter: NewPoolResumeCommitter(pool, convStore, pauseStore),
	})
	return r, convStore, pauseStore
}

// newIntegrationConversation creates a conversation off the seeded local identity and
// registers a cleanup that removes its pauses + turns + row (order-safe regardless of FK
// cascade).
func newIntegrationConversation(t *testing.T, pool *pgxpool.Pool, convStore *conversations.Store) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := convStore.Create(context.Background(), conversations.CreateParams{
		ID: id, IdentityID: localIdentityID, Model: "test-model",
	}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "DELETE FROM aura.paused_states WHERE conversation_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM aura.conversation_turns WHERE conversation_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM aura.conversations WHERE id=$1", id)
	})
	return id
}

// resumedAt reads a pause row's resumed_at (nil == still pending). The idempotency key is
// the WHERE resumed_at IS NULL conditional update (D-06), so this is the ground-truth
// "claimed?" probe for the atomicity assertions.
func resumedAt(t *testing.T, pool *pgxpool.Pool, token string) *time.Time {
	t.Helper()
	var ts *time.Time
	if err := pool.QueryRow(context.Background(),
		"SELECT resumed_at FROM aura.paused_states WHERE token=$1", token).Scan(&ts); err != nil {
		t.Fatalf("query resumed_at for %s: %v", token, err)
	}
	return ts
}

// countPauseRows counts paused_states rows for a conversation (the pause-exposure probe).
func countPauseRows(t *testing.T, pool *pgxpool.Pool, convID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.paused_states WHERE conversation_id=$1", convID).Scan(&n); err != nil {
		t.Fatalf("count paused_states: %v", err)
	}
	return n
}

// countPersistedToolTurns counts RoleTool rows actually PERSISTED in conversation_turns
// (DB ground truth). This differs from countToolAnswers over a rehydrated LoadHistory: the
// real store synthesizes a "previous result unknown after crash recovery" placeholder
// RoleTool for any assistant tool_call still lacking a durable answer (a pending pause),
// so LoadHistory shows a tool turn the DB does not store. Atomicity assertions (a
// rolled-back resume persisted no answer) must probe the persisted rows, not the
// synthesized view.
func countPersistedToolTurns(t *testing.T, pool *pgxpool.Pool, convID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.conversation_turns WHERE conversation_id=$1 AND role='tool'", convID).Scan(&n); err != nil {
		t.Fatalf("count persisted tool turns: %v", err)
	}
	return n
}

// countToolAnswers counts RoleTool answer turns in a rehydrated history (exactly one per
// resolved pause; a double-answer or orphan would move this off N).
func countToolAnswers(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == llm.RoleTool {
			n++
		}
	}
	return n
}

// historyHasToolContentMsgs reports whether any RoleTool turn's content contains needle.
func historyHasToolContentMsgs(msgs []llm.Message, needle string) bool {
	for _, m := range msgs {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}
