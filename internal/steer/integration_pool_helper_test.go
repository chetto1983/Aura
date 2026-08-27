//go:build db_integration

package steer

// The disposable-database helper every db_integration test in this package builds on.
// Copies internal/documents/integration_pool_helper_test.go's pipelineDisposablePool
// VERBATIM in shape (51-PATTERNS.md's own Test Analogs instruction): a fresh
// aura_steer_<uuid> database per test run, migrated by aura_migrate, opened for the
// test as aura_app — the CLAUDE.md "db_integration must run as aura_app" rule, since
// aura is superuser+BYPASSRLS and would silently mask an identity-scoping bug.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func steerDisposablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	password := steerEnvOrSkip(t, "POSTGRES_PASSWORD")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	dsn := func(role, database string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, password, host, port, database)
	}
	if err := db.EnsureRoles(ctx, dsn("aura", "aura"), password); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	root, err := db.Open(ctx, &db.Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open root database: %v", err)
	}
	database := "aura_steer_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		root.Close()
		t.Fatalf("create disposable steer database: %v", err)
	}
	var admin, pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if admin != nil {
			admin.Close()
		}
		_, _ = root.Exec(context.Background(), "DROP DATABASE "+database+" WITH (FORCE)")
		root.Close()
	})
	if _, err = root.Exec(ctx, "GRANT CREATE ON DATABASE "+database+" TO aura_migrate"); err != nil {
		t.Fatalf("grant disposable steer database: %v", err)
	}
	admin, err = db.Open(ctx, &db.Config{URL: dsn("aura", database)})
	if err != nil {
		t.Fatalf("open disposable steer database: %v", err)
	}
	if _, err = admin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant disposable public schema: %v", err)
	}
	// admin has no further use past this GRANT (db.Migrate below opens its own
	// connection); close it now rather than leaving it for t.Cleanup, which fires after
	// every defer in the calling test — including a goleak.VerifyNone defer, which would
	// otherwise still observe this pool's live backgroundHealthCheck goroutine.
	admin.Close()
	admin = nil
	if _, err = db.Migrate(ctx, dsn("aura_migrate", database)); err != nil {
		t.Fatalf("migrate disposable steer database: %v", err)
	}
	pool, err = db.Open(ctx, &db.Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatalf("open disposable app database: %v", err)
	}
	return pool
}

func steerEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("steer db_integration requires %s under CI", key)
		}
		t.Skipf("steer db_integration requires %s", key)
	}
	return value
}

// seedIdentityAndConversation inserts one identity + one conversation it owns, both
// visible to aura.conversation_owner() (migration 0103) and to steer_queue's own
// SECURITY DEFINER lookup. Runs on a root-privileged (aura role, bypasses RLS)
// connection so the seed itself never depends on the identity-scoping machinery under
// test.
func seedIdentityAndConversation(t *testing.T, pool *pgxpool.Pool) (identityID, conversationID string) {
	t.Helper()
	ctx := context.Background()
	identityID = uuid.NewString()
	conversationID = uuid.NewString()
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
		identityID, "steer-test-"+identityID[:8],
	); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	// aura.conversations is fail-closed RLS (migration 0089): the insert must carry
	// app.current_identity, unlike aura.identities above (control-plane, no RLS).
	if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO aura.conversations (id, identity_id, model, metadata) VALUES ($1, $2, 'test-model', '{}'::jsonb)",
			conversationID, identityID,
		)
		return err
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	return identityID, conversationID
}
