//go:build db_integration

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
)

// TestCLISmoke drives the built binary through every row of cliSmokeRows, in order, against a
// database of its own (C1 of docs/superpowers/verification/2026-10-31-cli-audit.md: the
// operator's verbs ran in no test, and `aura memory remember` failed for two months).
func TestCLISmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	smoke := newCLISmoke(t, ctx)
	for _, row := range cliSmokeRows {
		t.Run(row.name, func(t *testing.T) { smoke.run(t, ctx, row) })
	}
}

type cliSmoke struct {
	binary string
	env    []string
	probe  *pgxpool.Pool
	vars   map[string]string
}

// newCLISmoke builds the binary, creates a database for this run and the environment that points
// the binary at it. The environment is the operator's: a strict profile, a configured local
// model route that is never called, and a home of its own, since `config set` writes there.
func newCLISmoke(t *testing.T, ctx context.Context) *cliSmoke {
	t.Helper()
	password := cliMigrationEnvOrSkip(t, "POSTGRES_PASSWORD")
	host := envDefaultForTest("PGHOST", "127.0.0.1")
	port := envDefaultForTest("PGPORT", "5432")
	admin, err := db.Open(ctx, &db.Config{URL: migrationTestDSN("aura", password, host, port, envDefaultForTest("POSTGRES_DB", "aura"))})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	database := fmt.Sprintf("aura_cli_smoke_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteTestIdentifier(database)); err != nil {
		t.Fatalf("create the run's database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+quoteTestIdentifier(database)+` WITH (FORCE)`)
	})

	dir := t.TempDir()
	binary := filepath.Join(dir, "aura")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build aura CLI: %v\n%s", err, output)
	}
	bootstrapURL := migrationTestDSN("aura", password, host, port, database)
	env := append(withoutEnvKey(os.Environ(), cliIdempotencyChildEnv),
		"HOME="+filepath.Join(dir, "home"),
		"POSTGRES_USER=aura", "POSTGRES_PASSWORD="+password, "POSTGRES_HOST="+host, "POSTGRES_PORT="+port,
		"POSTGRES_DB="+database,
		"AURA_DB_BOOTSTRAP_URL="+bootstrapURL,
		"AURA_DB_MIGRATE_URL="+migrationTestDSN("aura_migrate", password, host, port, database),
		"AURA_DB_URL="+migrationTestDSN("aura_app", password, host, port, database),
		"AURA_PROFILE=single_user_hardened",
		"AURA_RUN_DIR="+filepath.Join(dir, "run"),
		"AURA_SKILLS_DIR="+filepath.Join(dir, "skills"), "AURA_SKILLS_IDENTITY_DIR="+filepath.Join(dir, "skills-identities"),
		"AURA_LLM_PROVIDER=llamacpp", "AURA_LLM_BASE_URL=http://127.0.0.1:9/v1", "AURA_LLM_MODEL=cli-smoke",
		"AURA_OBJECTSTORE_BACKEND=filesystem-dev",
		"AURA_OBJECTSTORE_ACCESS_KEY=GKclismoke000000000000000ab",
		"AURA_OBJECTSTORE_SECRET_KEY=cli-smoke-not-a-sample-secret-000000000000000000000000000000000000",
	)
	probe, err := db.Open(ctx, &db.Config{URL: bootstrapURL})
	if err != nil {
		t.Fatalf("open the run's database: %v", err)
	}
	t.Cleanup(probe.Close)
	return &cliSmoke{binary: binary, env: env, probe: probe, vars: map[string]string{}}
}

func (s *cliSmoke) run(t *testing.T, ctx context.Context, row cliSmokeRow) {
	t.Helper()
	argv := make([]string, len(row.argv))
	for i, arg := range row.argv {
		argv[i] = s.expand(t, arg)
	}
	rowCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(rowCtx, s.binary, argv...)
	cmd.Env = s.env
	cmd.Stdin = strings.NewReader("") // every verb runs unattended; chat new reads EOF and leaves
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exit := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		exit = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("aura %s: %v", strings.Join(argv, " "), err)
	}
	report := fmt.Sprintf("aura %s\nexit %d\n--- stdout\n%s\n--- stderr\n%s", strings.Join(argv, " "), exit, stdout.String(), stderr.String())
	if row.exit != anyExit && exit != row.exit {
		t.Fatalf("exit %d, want %d\n%s", exit, row.exit, report)
	}
	if want := s.expand(t, row.stdout); !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout lacks %q\n%s", want, report)
	}
	if want := s.expand(t, row.stderr); !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr lacks %q\n%s", want, report)
	}
	if row.after != "" {
		var held bool
		if err := s.probe.QueryRow(ctx, s.expand(t, row.after)).Scan(&held); err != nil || !held {
			t.Fatalf("the row's effect is missing (%v): %s\n%s", err, row.after, report)
		}
	}
	for name, query := range row.capture {
		var value string
		if err := s.probe.QueryRow(ctx, query).Scan(&value); err != nil {
			t.Fatalf("capture %s: %v\n%s", name, err, report)
		}
		s.vars[name] = value
	}
}

// expand replaces "{name}" with a value an earlier row captured. A placeholder nothing captured
// fails the row, so a skipped capture cannot turn into a literal "{task}" argument.
func (s *cliSmoke) expand(t *testing.T, text string) string {
	t.Helper()
	for name, value := range s.vars {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	if start := strings.Index(text, "{"); start >= 0 && !strings.HasPrefix(text[start:], `{"`) {
		if end := strings.Index(text[start:], "}"); end > 0 {
			t.Fatalf("placeholder %s was never captured", text[start:start+end+1])
		}
	}
	return text
}
