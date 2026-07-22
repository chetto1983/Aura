//go:build db_integration

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/idempotency"
)

const (
	dbResetCrashHelperEnv  = "AURA_TEST_DB_RESET_CRASH_HELPER"
	dbResetCrashMigrateEnv = "AURA_TEST_DB_RESET_CRASH_MIGRATE_URL"
	dbResetCrashKeyEnv     = "AURA_TEST_DB_RESET_CRASH_KEY"
)

func TestDBResetSameKeyReplaysWithoutDestroyingPostResetSentinel(t *testing.T) {
	if os.Getenv(dbResetCrashHelperEnv) == "1" {
		runDBResetCrashAfterAcquireHelper(t)
		return
	}
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

	const crashedOperationKey = "reset-crash-becomes-indeterminate"
	helper := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDBResetSameKeyReplaysWithoutDestroyingPostResetSentinel$")
	helper.Env = append(withoutEnvKey(os.Environ(), dbResetCrashHelperEnv),
		dbResetCrashHelperEnv+"=1",
		dbResetCrashMigrateEnv+"="+migrateURL,
		dbResetCrashKeyEnv+"="+crashedOperationKey,
	)
	helperStdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatalf("crash helper stdout: %v", err)
	}
	var helperStderr bytes.Buffer
	helper.Stderr = &helperStderr
	if err := helper.Start(); err != nil {
		t.Fatalf("start crash helper: %v", err)
	}
	ready, readErr := bufio.NewReader(helperStdout).ReadString('\n')
	if readErr != nil || strings.TrimSpace(ready) != "maintenance-acquired" {
		_ = helper.Process.Kill()
		_, _ = helper.Process.Wait()
		t.Fatalf("crash helper readiness=%q err=%v stderr=%q", ready, readErr, helperStderr.String())
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill reset parent after durable acquisition: %v", err)
	}
	if err := helper.Wait(); err == nil {
		t.Fatal("crash helper exited successfully instead of being terminated")
	}

	var crashedState string
	var retryAfter time.Time
	if err := migratePool.QueryRow(ctx, `
SELECT state, retry_after FROM public.aura_maintenance_operations
WHERE operation_scope = 'cli.command' AND operation_key = $1`, crashedOperationKey).Scan(&crashedState, &retryAfter); err != nil {
		t.Fatalf("read crashed reset receipt: %v", err)
	}
	if crashedState != "in_progress" || retryAfter.IsZero() {
		t.Fatalf("crashed reset receipt state/retry_after=%q/%v", crashedState, retryAfter)
	}
	if _, err := migratePool.Exec(ctx, `
UPDATE public.aura_maintenance_operations
SET retry_after = now() - interval '1 second'
WHERE operation_scope = 'cli.command' AND operation_key = $1`, crashedOperationKey); err != nil {
		t.Fatalf("advance crashed receipt beyond retry_after: %v", err)
	}

	crashRetry := exec.CommandContext(ctx, binary, "db", "reset", "--yes", "--operation-key", crashedOperationKey)
	crashRetry.Env = env
	crashRetryOutput, err := crashRetry.CombinedOutput()
	if err == nil {
		t.Fatalf("expired crashed reset unexpectedly executed: %s", crashRetryOutput)
	}
	if !strings.Contains(string(crashRetryOutput), "operation outcome is indeterminate") || strings.Contains(string(crashRetryOutput), "ok: schema reset") {
		t.Fatalf("expired crashed reset output=%q", crashRetryOutput)
	}
	if err := migratePool.QueryRow(ctx, `SELECT value FROM aura.reset_replay_sentinel`).Scan(&sentinel); err != nil || sentinel != "survives" {
		t.Fatalf("crashed-operation retry executed reset; sentinel=%q err=%v", sentinel, err)
	}
	var indeterminateAt time.Time
	if err := migratePool.QueryRow(ctx, `
SELECT state, updated_at FROM public.aura_maintenance_operations
WHERE operation_scope = 'cli.command' AND operation_key = $1`, crashedOperationKey).Scan(&crashedState, &indeterminateAt); err != nil {
		t.Fatalf("read reconciled crash receipt: %v", err)
	}
	if crashedState != "indeterminate" {
		t.Fatalf("reconciled crash state=%q, want indeterminate", crashedState)
	}

	terminalRetry := exec.CommandContext(ctx, binary, "db", "reset", "--yes", "--operation-key", crashedOperationKey)
	terminalRetry.Env = env
	terminalOutput, err := terminalRetry.CombinedOutput()
	if err == nil || !strings.Contains(string(terminalOutput), "operation outcome is indeterminate") {
		t.Fatalf("terminal retry err=%v output=%q", err, terminalOutput)
	}
	var terminalState string
	var terminalUpdatedAt time.Time
	if err := migratePool.QueryRow(ctx, `
SELECT state, updated_at FROM public.aura_maintenance_operations
WHERE operation_scope = 'cli.command' AND operation_key = $1`, crashedOperationKey).Scan(&terminalState, &terminalUpdatedAt); err != nil {
		t.Fatalf("read terminal crash receipt: %v", err)
	}
	if terminalState != "indeterminate" || !terminalUpdatedAt.Equal(indeterminateAt) {
		t.Fatalf("terminal receipt mutated state/time=%q/%v, want indeterminate/%v", terminalState, terminalUpdatedAt, indeterminateAt)
	}
	if err := migratePool.QueryRow(ctx, `SELECT value FROM aura.reset_replay_sentinel`).Scan(&sentinel); err != nil || sentinel != "survives" {
		t.Fatalf("terminal retry executed reset; sentinel=%q err=%v", sentinel, err)
	}
}

func runDBResetCrashAfterAcquireHelper(t *testing.T) {
	t.Helper()
	migrateURL := os.Getenv(dbResetCrashMigrateEnv)
	operationKey := os.Getenv(dbResetCrashKeyEnv)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prepared, _, err := prepareCLIIdempotency(ctx, []string{"db", "reset", "--yes", "--operation-key", operationKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	operation, ok := idempotency.OperationFromContext(prepared)
	if !ok {
		t.Fatal("prepared reset operation is missing")
	}
	registry, err := idempotency.OpenMaintenanceRegistry(ctx, migrateURL)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	decision, err := registry.Begin(ctx, idempotency.BeginRequest{Operation: operation.Key, Fingerprint: operation.Fingerprint})
	if err != nil || decision.Decision != idempotency.DecisionAcquired {
		t.Fatalf("acquire crash receipt decision=%+v err=%v", decision, err)
	}
	_, _ = fmt.Fprintln(os.Stdout, "maintenance-acquired")
	select {}
}
