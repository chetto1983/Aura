//go:build db_integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration0026LocalAdminCapsRoundTrip proves migration 0026 seeds `local`'s
// three EXPLICIT admin capabilities idempotently and that its down migration
// removes exactly those three rows while leaving the system-managed `*` wildcard
// (seeded by 0004) intact.
func TestMigration0026LocalAdminCapsRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_migrate0026_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema: %v", err)
	}

	migrateURL := dsn("aura_migrate", freshDB)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("full Migrate up on fresh db: %v", err)
	}
	if headVersion := currentMigrationVersion(t, ctx, freshAdmin); headVersion < 26 {
		t.Fatalf("full Migrate up reached version %d, want at least 26", headVersion)
	}

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	const localID = "00000000-0000-0000-0000-000000000001"
	explicit := []string{"governance.write", "identity.create", "agent.run"}

	// After HEAD: local holds the seeded `*` plus the three explicit admin caps.
	caps := localCapabilitySet0026(t, ctx, app, localID)
	requireCap0026(t, caps, "*", true)
	for _, c := range explicit {
		requireCap0026(t, caps, c, true)
	}

	// Step DOWN 0026: the three explicit caps are removed; `*` survives.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps(-1) down 0026: %v", err)
	}
	caps = localCapabilitySet0026(t, ctx, app, localID)
	requireCap0026(t, caps, "*", true)
	for _, c := range explicit {
		requireCap0026(t, caps, c, false)
	}

	// Step UP 0026 again: the three explicit caps are restored idempotently.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0026: %v", err)
	}
	caps = localCapabilitySet0026(t, ctx, app, localID)
	requireCap0026(t, caps, "*", true)
	for _, c := range explicit {
		requireCap0026(t, caps, c, true)
	}

	// The seed is reversible with no lingering pending migration.
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0026 reversible), got %d", n)
	}
}

func localCapabilitySet0026(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID string) map[string]bool {
	t.Helper()
	id := pgtype.UUID{Bytes: uuid.MustParse(identityID), Valid: true}
	rows, err := pool.Query(ctx, `SELECT capability FROM aura.capability_grants WHERE identity_id = $1`, id)
	if err != nil {
		t.Fatalf("query local capability_grants: %v", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			t.Fatalf("scan capability: %v", err)
		}
		set[capability] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate capabilities: %v", err)
	}
	return set
}

func requireCap0026(t *testing.T, set map[string]bool, capability string, want bool) {
	t.Helper()
	if set[capability] != want {
		t.Fatalf("capability %q present=%v, want present=%v (set=%v)", capability, set[capability], want, set)
	}
}
