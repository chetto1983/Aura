//go:build db_integration

package adaptive

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// shippedMigrationSteps is the number of migrations on disk — the step count that lands a
// disposable database on TODAY's schema. Fixtures that mean "the current schema" must derive
// it: a literal is a number somebody has to remember to bump with every migration, and the
// one nobody bumped (0074 shipped, this fixture stayed at 73) produced a green local run and
// a red CI, with the production query reading a column its own test schema did not have.
func shippedMigrationSteps(t *testing.T) int {
	t.Helper()
	head, err := db.MigrationHead()
	if err != nil {
		t.Fatalf("read embedded migration head: %v", err)
	}
	return int(head)
}

func schema2MigrationDatabase(
	t *testing.T,
	ctx context.Context,
	database string,
	steps int,
) (*pgxpool.Pool, string) {
	t.Helper()
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("integration test requires POSTGRES_PASSWORD under CI")
		}
		t.Skip("integration test requires POSTGRES_PASSWORD")
	}
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	dsn := func(role, database string) string {
		return fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			role, password, host, port, database,
		)
	}
	if err := db.EnsureRoles(ctx, dsn("aura", "aura"), password); err != nil {
		t.Fatal(err)
	}
	admin, err := db.Open(ctx, &db.Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+database+" TO aura_migrate"); err != nil {
		t.Fatal(err)
	}
	freshAdmin, err := db.Open(ctx, &db.Config{URL: dsn("aura", database)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		freshAdmin.Close()
		t.Fatal(err)
	}
	freshAdmin.Close()

	migrateURL := dsn("aura_migrate", database)
	if err := db.MigrateSteps(ctx, migrateURL, steps); err != nil {
		t.Fatalf("install migrations through %04d: %v", steps, err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assertSchema2MigrationVersion(t, ctx, migrateURL, steps)
	return pool, migrateURL
}

func assertSchema2MigrationVersion(
	t *testing.T,
	ctx context.Context,
	migrateURL string,
	want int,
) {
	t.Helper()
	version, dirty := schema2MigrationState(t, ctx, migrateURL)
	if version != want || dirty {
		t.Fatalf("schema migration state = version %d dirty %t, want %d clean", version, dirty, want)
	}
}

func schema2MigrationState(
	t *testing.T,
	ctx context.Context,
	migrateURL string,
) (int, bool) {
	t.Helper()
	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	return version, dirty
}
