//go:build db_integration

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
)

func TestDBResetSameKeyReplaysWithoutDestroyingPostResetSentinel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	password := cliMigrationEnvOrSkip(t, "POSTGRES_PASSWORD")
	host := envDefaultForTest("PGHOST", "127.0.0.1")
	port := envDefaultForTest("PGPORT", "5432")
	adminDatabase := envDefaultForTest("POSTGRES_DB", "aura")
	adminURL := migrationTestDSN("aura", password, host, port, adminDatabase)
	admin, err := db.Open(ctx, &db.Config{URL: adminURL})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()

	database := fmt.Sprintf("aura_cli_reset_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteTestIdentifier(database)); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+quoteTestIdentifier(database)+` WITH (FORCE)`)
	})

	bootstrapURL := migrationTestDSN("aura", password, host, port, database)
	migrateURL := migrationTestDSN("aura_migrate", password, host, port, database)
	appURL := migrationTestDSN("aura_app", password, host, port, database)
	if err := db.EnsureRoles(ctx, bootstrapURL, password); err != nil {
		t.Fatalf("ensure roles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate disposable database: %v", err)
	}

	binary := filepath.Join(t.TempDir(), "aura")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build aura CLI: %v\n%s", err, output)
	}
	env := withoutEnvKey(os.Environ(), cliIdempotencyChildEnv)
	env = append(env,
		"POSTGRES_USER=aura", "POSTGRES_PASSWORD="+password,
		"POSTGRES_HOST="+host, "POSTGRES_PORT="+port, "POSTGRES_DB="+database,
		"AURA_DB_BOOTSTRAP_URL="+bootstrapURL, "AURA_DB_MIGRATE_URL="+migrateURL,
		"AURA_DB_URL="+appURL, "AURA_RESET_YES=1",
	)

	const operationKey = "reset-replay-preserves-sentinel"
	first := exec.CommandContext(ctx, binary, "db", "reset", "--yes", "--operation-key", operationKey)
	first.Env = env
	firstOutput, err := first.CombinedOutput()
	if err != nil {
		t.Fatalf("first reset: %v\n%s", err, firstOutput)
	}
	if !strings.Contains(string(firstOutput), "ok: schema reset") {
		t.Fatalf("first reset output = %q", firstOutput)
	}

	migratePool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migrate pool: %v", err)
	}
	defer migratePool.Close()
	if _, err := migratePool.Exec(ctx, `CREATE TABLE aura.reset_replay_sentinel (value text PRIMARY KEY)`); err != nil {
		t.Fatalf("create post-reset sentinel: %v", err)
	}
	if _, err := migratePool.Exec(ctx, `INSERT INTO aura.reset_replay_sentinel (value) VALUES ('survives')`); err != nil {
		t.Fatalf("insert post-reset sentinel: %v", err)
	}

	retry := exec.CommandContext(ctx, binary, "db", "reset", "--yes", "--operation-key", operationKey)
	retry.Env = env
	retryOutput, err := retry.CombinedOutput()
	if err != nil {
		t.Fatalf("same-key reset retry: %v\n%s", err, retryOutput)
	}
	if string(retryOutput) != string(firstOutput) {
		t.Fatalf("retry output = %q, want replay %q", retryOutput, firstOutput)
	}
	var sentinel string
	if err := migratePool.QueryRow(ctx, `SELECT value FROM aura.reset_replay_sentinel`).Scan(&sentinel); err != nil {
		t.Fatalf("post-reset sentinel was destroyed by retry: %v", err)
	}
	if sentinel != "survives" {
		t.Fatalf("sentinel = %q, want survives", sentinel)
	}
	var durableState string
	if err := migratePool.QueryRow(ctx, `
SELECT state FROM public.aura_maintenance_operations
WHERE operation_scope = 'cli.command' AND operation_key = $1`, operationKey).Scan(&durableState); err != nil {
		t.Fatalf("read durable reset receipt: %v", err)
	}
	if durableState != "completed" {
		t.Fatalf("durable reset state = %q, want completed", durableState)
	}
}
