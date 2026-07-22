//go:build db_integration

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
)

func TestDBMigrateCLIWorksBeforeRuntimeRegistrySchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
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

	binary := filepath.Join(t.TempDir(), "aura")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build aura CLI: %v\n%s", err, output)
	}

	head, err := db.MigrationHead()
	if err != nil {
		t.Fatal(err)
	}
	for _, startVersion := range []int64{0, 42, 45} {
		startVersion := startVersion
		t.Run(fmt.Sprintf("from_%d", startVersion), func(t *testing.T) {
			database := fmt.Sprintf("aura_cli_migrate_%d_%d", startVersion, time.Now().UnixNano())
			if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteTestIdentifier(database)); err != nil {
				t.Fatalf("create disposable database: %v", err)
			}
			t.Cleanup(func() {
				_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+quoteTestIdentifier(database)+` WITH (FORCE)`)
			})

			bootstrapURL := migrationTestDSN("aura", password, host, port, database)
			migrateURL := migrationTestDSN("aura_migrate", password, host, port, database)
			appURL := migrationTestDSN("aura_app", password, host, port, database)
			if startVersion != 0 {
				if err := db.EnsureRoles(ctx, bootstrapURL, password); err != nil {
					t.Fatalf("ensure roles: %v", err)
				}
				if _, err := db.Migrate(ctx, migrateURL); err != nil {
					t.Fatalf("prepare head schema: %v", err)
				}
				if startVersion > head {
					t.Fatalf("test start version %d exceeds embedded head %d", startVersion, head)
				}
				if err := db.MigrateSteps(ctx, migrateURL, int(startVersion-head)); err != nil {
					t.Fatalf("step schema to %d: %v", startVersion, err)
				}
			}

			env := withoutEnvKey(os.Environ(), cliIdempotencyChildEnv)
			env = append(env,
				"POSTGRES_USER=aura",
				"POSTGRES_PASSWORD="+password,
				"POSTGRES_HOST="+host,
				"POSTGRES_PORT="+port,
				"POSTGRES_DB="+database,
				"AURA_DB_BOOTSTRAP_URL="+bootstrapURL,
				"AURA_DB_MIGRATE_URL="+migrateURL,
				"AURA_DB_URL="+appURL,
			)
			operationKey := "migrate-from-" + strconv.FormatInt(startVersion, 10)
			for attempt := 1; attempt <= 2; attempt++ {
				command := exec.CommandContext(ctx, binary, "db", "migrate", "--operation-key", operationKey)
				command.Env = env
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("migration attempt %d from %d: %v\n%s", attempt, startVersion, err, output)
				}
				if strings.Contains(string(output), "operation registry unavailable") {
					t.Fatalf("migration attempt %d touched runtime registry: %s", attempt, output)
				}
				if attempt == 2 && !strings.Contains(string(output), "no pending migrations") {
					t.Fatalf("migration retry output = %q, want tracker-backed no-op", output)
				}
			}
			if err := db.CheckMigrationHead(ctx, migrateURL); err != nil {
				t.Fatalf("schema after CLI migration: %v", err)
			}
		})
	}
}

func cliMigrationEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value != "" {
		return value
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("integration test requires %s under CI", key)
	}
	t.Skipf("integration test requires %s", key)
	return ""
}

func envDefaultForTest(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func migrationTestDSN(role, password, host, port, database string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, password, host, port, database)
}

func quoteTestIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func withoutEnvKey(env []string, key string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
