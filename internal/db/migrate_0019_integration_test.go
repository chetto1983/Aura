//go:build db_integration

// Integration coverage for migration 0019 (Authula web-auth schema isolation, H1):
// the dedicated `authula` schema, its aura_app CREATE+USAGE grant, the additive
// aura.identity_auth_links join table, and the down/up round-trip. Runs on a throwaway
// database so the shared `aura` DB is untouched, mirroring TestMigrate0017.
//
// Run via: go test -tags db_integration ./internal/db -run TestMigrate0019 -count=1
//
// Asserts:
//   - the `authula` schema exists after migrate up; aura_app holds CREATE on it (so the
//     embedded framework can run its own migrations there)
//   - aura.identity_auth_links exists with the UNIQUE(authula_user_id) constraint and a
//     FK to aura.identities (link upsert + cascade behavior)
//   - aura.* is otherwise untouched (the seeded `local` identity still present)
//   - stepping down through 0019 drops the schema + table; re-up restores them; final
//     Migrate applies zero (fully reversible even as later migrations are added)

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMigrate0019_AuthulaSchemaAndLink(t *testing.T) {
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

	const freshDB = "aura_migrate0019_drill"
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
	headVersion := currentMigrationVersion(t, ctx, freshAdmin)
	if headVersion < 19 {
		t.Fatalf("full Migrate up reached version %d, want at least 19", headVersion)
	}
	stepsToBefore0019 := headVersion - 18

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	// 1. The authula schema exists and aura_app holds CREATE on it.
	var schemaExists bool
	if err := app.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name='authula')`,
	).Scan(&schemaExists); err != nil {
		t.Fatalf("check authula schema: %v", err)
	}
	if !schemaExists {
		t.Fatal("authula schema missing after migrate up")
	}
	var canCreate bool
	if err := app.QueryRow(ctx,
		`SELECT has_schema_privilege('aura_app', 'authula', 'CREATE')`,
	).Scan(&canCreate); err != nil {
		t.Fatalf("check aura_app CREATE on authula: %v", err)
	}
	if !canCreate {
		t.Error("aura_app must hold CREATE on the authula schema (Authula auto-creates its tables there)")
	}

	// 2. aura.identity_auth_links exists; the FK + UNIQUE + cascade behave.
	const seededIdentity = "00000000-0000-0000-0000-000000000001" // 0004 seed
	var localPresent bool
	if err := app.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM aura.identities WHERE id=$1)`, seededIdentity,
	).Scan(&localPresent); err != nil {
		t.Fatalf("check seeded local identity: %v", err)
	}
	if !localPresent {
		t.Fatal("seeded `local` identity missing — 0019 must not disturb aura.*")
	}
	if _, err := app.Exec(ctx,
		`INSERT INTO aura.identity_auth_links (identity_id, authula_user_id) VALUES ($1, 'authula-user-1')`,
		seededIdentity,
	); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	// UNIQUE(authula_user_id): a second row with the same authula user-id must conflict.
	if _, err := app.Exec(ctx,
		`INSERT INTO aura.identity_auth_links (identity_id, authula_user_id) VALUES ($1, 'authula-user-1')`,
		seededIdentity,
	); err == nil {
		t.Error("duplicate authula_user_id should violate the UNIQUE constraint")
	}
	// Resolve round-trips.
	var gotID string
	if err := app.QueryRow(ctx,
		`SELECT identity_id::text FROM aura.identity_auth_links WHERE authula_user_id='authula-user-1'`,
	).Scan(&gotID); err != nil {
		t.Fatalf("resolve link: %v", err)
	}
	if gotID != seededIdentity {
		t.Errorf("linked identity = %q, want %q", gotID, seededIdentity)
	}

	// 3. Down through 0019 drops the schema + table; re-up restores; final
	// Migrate applies zero. The computed step count keeps this drill stable as HEAD
	// advances beyond 0019.
	if err := MigrateSteps(ctx, migrateURL, -stepsToBefore0019); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0019: %v", -stepsToBefore0019, err)
	}
	var schemaStillThere bool
	if err := app.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name='authula')`,
	).Scan(&schemaStillThere); err != nil {
		t.Fatalf("check authula schema after down: %v", err)
	}
	if schemaStillThere {
		t.Error("authula schema still present after down — down did not drop it")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0019: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("post-round-trip Migrate to HEAD: %v", err)
	}
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0019 reversible), got %d", n)
	}
}
