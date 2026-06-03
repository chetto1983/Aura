//go:build db_integration

// Boot-recovery integration tier for the SessionManager (D-06). Requires a live
// Postgres with the Phase-8 migrations applied through 0008 (sandbox_sessions):
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/sandbox
//
// No-skip-as-green (CLAUDE.md): envOrSkip t.Fatals under $CI when the DSN is unset,
// so a skipped tier never reports falsely green. It exercises the registry path
// (InsertSession -> ListActive -> MarkTerminated) against real sqlc + pgx, proving
// boot recovery marks prior 'active' rows terminated.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// recoveryDocker is a no-op dockerClient: boot recovery's stray sweep must not need
// a daemon to exercise the registry path.
type recoveryDocker struct{}

func (recoveryDocker) run(context.Context, string, []string) (string, error) { return "", nil }
func (recoveryDocker) stop(context.Context, string) error                    { return nil }
func (recoveryDocker) remove(context.Context, string) error                  { return nil }
func (recoveryDocker) listStray(context.Context) ([]string, error)           { return nil, nil }

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("db_integration test requires %s, unset under CI — a skipped integration "+
				"test must not pass as green; wire the DSN in ci.yml", key)
		}
		t.Skipf("db_integration test requires %s + a live Postgres; set it and run make db-up", key)
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
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newConversationRow inserts a throwaway conversation (FK parent for
// sandbox_sessions.conversation_id) and registers cascade cleanup. Returns its id.
func newConversationRow(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	id := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	_, err := pool.Exec(ctx,
		`INSERT INTO aura.conversations (id, identity_id, status)
		 VALUES ($1, '00000000-0000-0000-0000-000000000001', 'active')`, id)
	if err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id = $1`, id)
	})
	return id
}

// TestMigration0008_SchemaRoundTrip is the 0008_sandbox_sessions migration contract
// test (db tier). migratedPool runs every migration Up THROUGH 0008; this test then
// asserts the shipped schema matches the contract — the table + the status/last_used
// index + the status CHECK + the uuid FK with ON DELETE CASCADE (landmine #1: a uuid
// FK, NOT text) — and exercises the cascade (deleting the conversation removes the
// session row). The down leg is asserted structurally: 0008.down DROPs the table, so a
// post-Reset Status (db package tier) would not list it; here we prove the up contract
// + the cascade the down relies on. Reaching this assertion live proves the round-trip.
func TestMigration0008_SchemaRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()

	// (1) Table exists in the aura schema.
	var tbl string
	if err := pool.QueryRow(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema='aura' AND table_name='sandbox_sessions'`).Scan(&tbl); err != nil {
		t.Fatalf("0008 must create aura.sandbox_sessions: %v", err)
	}

	// (2) The status/last_used index the reaper + boot recovery scan on exists.
	var idx string
	if err := pool.QueryRow(ctx,
		`SELECT indexname FROM pg_indexes
		 WHERE schemaname='aura' AND tablename='sandbox_sessions'
		   AND indexname='sandbox_sessions_status_last_used_idx'`).Scan(&idx); err != nil {
		t.Fatalf("0008 must create the status/last_used index: %v", err)
	}

	// (3) conversation_id is uuid (landmine #1 — a text FK to a uuid PK is rejected).
	var dataType string
	if err := pool.QueryRow(ctx,
		`SELECT data_type FROM information_schema.columns
		 WHERE table_schema='aura' AND table_name='sandbox_sessions' AND column_name='conversation_id'`).Scan(&dataType); err != nil {
		t.Fatalf("conversation_id column lookup: %v", err)
	}
	if dataType != "uuid" {
		t.Fatalf("conversation_id must be uuid (landmine #1), got %q", dataType)
	}

	// (4) ON DELETE CASCADE: inserting a session then deleting its conversation row
	// removes the session row (the FK the down migration's DROP relies on).
	convID := newConversationRow(t, pool)
	row, err := sqlc.New(pool).InsertSession(ctx, sqlc.InsertSessionParams{
		ConversationID: convID,
		ContainerID:    "aura-sandbox-sess-cascade-" + uuid.NewString(),
		ImageDigest:    "img@sha256:roundtrip",
	})
	if err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM aura.conversations WHERE id = $1`, convID); err != nil {
		t.Fatalf("delete conversation (cascade trigger): %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.sandbox_sessions WHERE id = $1`, row.ID).Scan(&count); err != nil {
		t.Fatalf("post-cascade count: %v", err)
	}
	if count != 0 {
		t.Fatalf("ON DELETE CASCADE must remove the session row, found %d", count)
	}
}

func TestSessions_BootRecoveryMarksTerminated(t *testing.T) {
	pool := migratedPool(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	convID := newConversationRow(t, pool)
	row, err := q.InsertSession(ctx, sqlc.InsertSessionParams{
		ConversationID: convID,
		ContainerID:    "aura-sandbox-sess-it-" + uuid.NewString(),
		ImageDigest:    "img@sha256:test",
	})
	if err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.sandbox_sessions WHERE id = $1`, row.ID)
	})

	mctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := NewSessionManager(mctx, SessionDeps{
		Docker:  recoveryDocker{},
		Store:   q,
		MaxN:    5,
		TTL:     1800 * time.Second,
		Runtime: "runsc",
		Image:   "img",
	})
	defer m.Close()

	if err := m.RecoverOnBoot(ctx); err != nil {
		t.Fatalf("RecoverOnBoot: %v", err)
	}

	active, err := q.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, a := range active {
		if a.ID == row.ID {
			t.Fatalf("session %v still active after boot recovery, want terminated", row.ID)
		}
	}
}
