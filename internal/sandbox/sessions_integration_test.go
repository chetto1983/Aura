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
