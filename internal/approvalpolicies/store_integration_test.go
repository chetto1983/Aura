//go:build db_integration

// Live-Postgres test for the per-identity tool policy store. It proves what only a live
// engine can: the upsert replaces rather than accumulates; one identity cannot read another's
// policy and a connection with no principal sees none, enforced by the two RLS policies of
// migration 0140, not by a WHERE clause; and deleting the identity cascades its rows away.
//
//	go test -tags db_integration -run TestPolicies ./internal/approvalpolicies/
//
// A skipped tier must never pass as green (t.Fatal under $CI, per CLAUDE.md).
package approvalpolicies

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("approvalpolicies integration requires %s under CI — a skipped tier must not pass as green", key)
		}
		t.Skipf("approvalpolicies integration requires %s", key)
	}
	return v
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
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

func seedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING`,
		id, "policies-test-"+id[:8]); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_, _ = pool.Exec(cctx, `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

func TestPoliciesSetReplacesAndClearReports(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()

	if err := store.Set(ctx, id, "calendar", "send_email", PolicyAsk, "alice"); err != nil {
		t.Fatalf("Set ask: %v", err)
	}
	if err := store.Set(ctx, id, "shell_exec", "", PolicyDeny, ""); err != nil {
		t.Fatalf("Set deny: %v", err)
	}
	if err := store.Set(ctx, id, "calendar", "send_email", PolicyDeny, "bob"); err != nil {
		t.Fatalf("Set deny over ask: %v", err)
	}

	policy, ok, err := store.Get(ctx, id, "calendar", "send_email")
	if err != nil || !ok || policy != PolicyDeny {
		t.Fatalf("Get after upsert = %q, %v, %v; want deny", policy, ok, err)
	}
	if _, ok, err := store.Get(ctx, id, "calendar", "delete_event"); err != nil || ok {
		t.Fatalf("a sibling verb must have no policy: %v, %v", ok, err)
	}

	rows, err := store.List(ctx, id)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 || rows[0].Subject() != "calendar send_email" || rows[1].Subject() != "shell_exec" {
		t.Fatalf("List = %+v; want the two rows ordered by tool", rows)
	}
	if rows[0].SetBy != "bob" || rows[0].Policy != PolicyDeny {
		t.Errorf("the upsert must replace policy and attribution: %+v", rows[0])
	}

	removed, err := store.Clear(ctx, id, "shell_exec", "")
	if err != nil || !removed {
		t.Fatalf("Clear = %v, %v; want removed", removed, err)
	}
	removed, err = store.Clear(ctx, id, "shell_exec", "")
	if err != nil || removed {
		t.Fatalf("second Clear = %v, %v; want nothing removed", removed, err)
	}
}

func TestPoliciesAreIdentityScoped(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	owner := seedIdentity(t, pool)
	other := seedIdentity(t, pool)
	ctx := context.Background()
	if err := store.Set(ctx, owner, "shell_exec", "", PolicyDeny, ""); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, ok, err := store.Get(ctx, other, "shell_exec", ""); err != nil || ok {
		t.Fatalf("another identity reads the owner's policy: %v, %v", ok, err)
	}
	if rows, err := store.List(ctx, other); err != nil || len(rows) != 0 {
		t.Fatalf("another identity lists the owner's policies: %+v, %v", rows, err)
	}
	if removed, err := store.Clear(ctx, other, "shell_exec", ""); err != nil || removed {
		t.Fatalf("another identity cleared the owner's policy: %v, %v", removed, err)
	}
	if _, ok, err := store.Get(ctx, owner, "shell_exec", ""); err != nil || !ok {
		t.Fatalf("the owner's policy is gone: %v, %v", ok, err)
	}
}

func TestPoliciesAreInvisibleWithoutAnIdentity(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	owner := seedIdentity(t, pool)
	ctx := context.Background()
	if err := store.Set(ctx, owner, "shell_exec", "", PolicyDeny, ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	var visible int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aura.gateway_tool_policies`).Scan(&visible); err != nil {
		t.Fatalf("count with no principal: %v", err)
	}
	if visible != 0 {
		t.Fatalf("a connection with no app.current_identity sees %d policies, want 0", visible)
	}
}

func TestPoliciesFollowTheIdentityOut(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	owner := seedIdentity(t, pool)
	ctx := context.Background()
	if err := store.Set(ctx, owner, "shell_exec", "", PolicyAsk, ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, owner); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	rows, err := store.List(ctx, owner)
	if err != nil {
		t.Fatalf("List after cascade: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("policies survived their identity: %+v", rows)
	}
}
