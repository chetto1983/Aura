//go:build db_integration

package documents

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pipelineDisposablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	password := pipelineEnvOrSkip(t, "POSTGRES_PASSWORD")
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
	database := "aura_pipeline_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		root.Close()
		t.Fatalf("create disposable pipeline database: %v", err)
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
		t.Fatalf("grant disposable pipeline database: %v", err)
	}
	admin, err = db.Open(ctx, &db.Config{URL: dsn("aura", database)})
	if err != nil {
		t.Fatalf("open disposable pipeline database: %v", err)
	}
	if _, err = admin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant disposable public schema: %v", err)
	}
	if _, err = db.Migrate(ctx, dsn("aura_migrate", database)); err != nil {
		t.Fatalf("migrate disposable pipeline database: %v", err)
	}
	pool, err = db.Open(ctx, &db.Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatalf("open disposable app database: %v", err)
	}
	return pool
}

func pipelineEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("pipeline integration requires %s under CI", key)
		}
		t.Skipf("pipeline integration requires %s", key)
	}
	return value
}
