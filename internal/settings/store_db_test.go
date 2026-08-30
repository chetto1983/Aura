//go:build db_integration

// Integration tests for the aura.settings Store. Requires a running Postgres with the
// migrations applied (AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD in env;
// `scripts/coverage_docker.sh` provisions a disposable database for exactly this tier).
//
//	go test -tags db_integration -race ./internal/settings
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset.
package settings

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// The keys below are allowlisted (AllowedKeys) so is_secret comes from the allowlist
// exactly as the API path relies on; every test deletes what it wrote.
const (
	stepsKey  = "AURA_LOOP_MAX_STEPS"
	wallKey   = "AURA_LOOP_MAX_WALLCLOCK_SEC"
	secretKey = "OPENROUTER_API_KEY"
)

func cleanupKeys(t *testing.T, s *Store, keys ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, key := range keys {
			_ = s.Delete(context.Background(), key)
		}
	})
}

func valueOf(rows []sqlc.AuraSettings, key string) (string, bool) {
	for _, row := range rows {
		if row.Key == key {
			return row.Value, true
		}
	}
	return "", false
}

func TestStoreUpsertListReplaceDelete(t *testing.T) {
	ctx := context.Background()
	s := NewStore(migratedPool(t))
	cleanupKeys(t, s, stepsKey, wallKey, secretKey)

	row, err := s.Upsert(ctx, stepsKey, "7", "tester")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if row.Key != stepsKey || row.Value != "7" || row.IsSecret || !row.UpdatedBy.Valid || row.UpdatedBy.String != "tester" {
		t.Fatalf("upserted row = %+v", row)
	}
	secret, err := s.Upsert(ctx, secretKey, "sk-test", "")
	if err != nil {
		t.Fatalf("Upsert secret: %v", err)
	}
	if !secret.IsSecret || secret.UpdatedBy.Valid {
		t.Fatalf("secret row = %+v, want is_secret from the allowlist and no updated_by", secret)
	}

	rows, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !sort.SliceIsSorted(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key }) {
		t.Fatal("List is not ordered by key")
	}
	if v, ok := valueOf(rows, stepsKey); !ok || v != "7" {
		t.Fatalf("List %s = (%q,%v), want 7", stepsKey, v, ok)
	}

	replaced, err := s.ReplaceMany(ctx, map[string]string{wallKey: "120", stepsKey: "9"}, []string{secretKey}, "batch")
	if err != nil {
		t.Fatalf("ReplaceMany: %v", err)
	}
	if len(replaced) != 2 || replaced[0].Key != stepsKey || replaced[0].Value != "9" || replaced[1].Key != wallKey || replaced[1].Value != "120" {
		t.Fatalf("ReplaceMany rows = %+v, want key-sorted steps=9, wallclock=120", replaced)
	}
	rows, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List after replace: %v", err)
	}
	if _, ok := valueOf(rows, secretKey); ok {
		t.Fatal("ReplaceMany did not delete the secret row in the same transaction")
	}
	if v, _ := valueOf(rows, stepsKey); v != "9" {
		t.Fatalf("steps after replace = %q, want 9", v)
	}

	if err := s.Delete(ctx, stepsKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, stepsKey); err != nil {
		t.Fatalf("Delete of an absent key must be a no-op, got %v", err)
	}
	rows, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if _, ok := valueOf(rows, stepsKey); ok {
		t.Fatal("deleted row still listed")
	}
}

// Every write goes through the advisory-locked transaction; a context that is already
// done must fail closed at Begin, never write.
func TestStoreWritesFailClosedOnDoneContext(t *testing.T) {
	s := NewStore(migratedPool(t))
	cleanupKeys(t, s, stepsKey)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Upsert(ctx, stepsKey, "1", ""); err == nil {
		t.Fatal("Upsert on a done context succeeded")
	}
	if _, err := s.ReplaceMany(ctx, map[string]string{stepsKey: "1"}, nil, ""); err == nil {
		t.Fatal("ReplaceMany on a done context succeeded")
	}
	if err := s.Delete(ctx, stepsKey); err == nil {
		t.Fatal("Delete on a done context succeeded")
	}
	rows, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := valueOf(rows, stepsKey); ok {
		t.Fatal("a failed write left a row behind")
	}
}
