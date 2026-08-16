//go:build db_integration

// Integration tests for the skilladapters Writer success paths — the audit-tx
// branches that require a live Postgres pool (db.WithTx → pool.Begin). These cover
// the three success-return statements unit tests cannot reach with a nil pool:
// SaveSnippet's active status, Restore's active status, and ArchiveSnippet's
// "archived" status. The Loader-adapter and Writer-adapter PRE-tx branches are
// covered by the unit tier (skilladapters_loader_test.go / skilladapters_writer_test.go).
//
// Requires a running Postgres migrated through 0010 (the skill_audit table):
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration ./internal/skilladapters -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when a DSN is unset, so a skipped
// tier can never report falsely-green.
package skilladapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// envOrSkip mirrors internal/skills: skip locally, fail-loud under CI so a missing
// DSN never reports a falsely-green integration job.
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

// bootstrapURL composes the superuser DSN EnsureRoles needs from the password.
func bootstrapURL(t *testing.T, pwd string) string {
	t.Helper()
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
}

// migratedPool brings up an aura_app pool against a Postgres migrated through 0010.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	if err := db.EnsureRoles(ctx, bootstrapURL(t, pwd), pwd); err != nil {
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

// liveWriterAdapter builds a Writer adapter over a real *skills.Writer with the live
// pool, rooted under a temp dir so each test owns its own pending/active/export/archive.
func liveWriterAdapter(t *testing.T, pool *pgxpool.Pool) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	live := skills.NewWriter(skills.WriterConfig{
		Pool:         pool,
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		BodyCapBytes: 32768,
	})
	return NewWriter(live, nil), root
}

// uniqueName yields a per-test skill name within the [a-z0-9-] grammar.
func uniqueName(prefix string) string {
	return prefix + uuid.Must(uuid.NewV7()).String()[:8]
}

// TestWriteMutationCreateLandsUsable proves the model-path create materializes in-box
// and returns the active status through the adapter (the db.WithTx audit-tx success
// branch).
func TestWriteMutationCreateLandsUsable(t *testing.T) {
	pool := migratedPool(t)
	a, root := liveWriterAdapter(t, pool)
	name := uniqueName("create-")

	status, err := a.WriteMutation(context.Background(), "create", name, "an in-box skill", "Do the thing.", false)
	if err != nil {
		t.Fatalf("WriteMutation create: %v", err)
	}
	if status != skills.StatusActive {
		t.Fatalf("WriteMutation create status = %q, want %q", status, skills.StatusActive)
	}
	// The write materializes the skill into the export dir.
	if _, statErr := os.Stat(filepath.Join(root, "export", name, "SKILL.md")); statErr != nil {
		t.Errorf("after in-box create the export SKILL.md is missing: %v", statErr)
	}
}

// TestSaveSnippetLandsUsable proves SaveSnippet validates, writes, records the audit
// tuple and returns the active status through the adapter. The export copy is the
// assertion that matters: it is the file UseSnippet hands the model, so a snippet that
// saved without it is one the model is told to run and cannot.
func TestSaveSnippetLandsUsable(t *testing.T) {
	pool := migratedPool(t)
	a, root := liveWriterAdapter(t, pool)
	name := uniqueName("snip-")

	status, err := a.SaveSnippet(context.Background(), name, "python", "print('"+name+"')\n", "a saved snippet", true, false)
	if err != nil {
		t.Fatalf("SaveSnippet: %v", err)
	}
	if status != skills.StatusActive {
		t.Fatalf("SaveSnippet status = %q, want %q", status, skills.StatusActive)
	}
	for _, dir := range []string{"active", "export"} {
		if _, statErr := os.Stat(filepath.Join(root, dir, name, name+".py")); statErr != nil {
			t.Errorf("after SaveSnippet the %s code file is missing: %v", dir, statErr)
		}
	}
}

// TestArchiveThenRestoreRoundTrips proves the adapter's ArchiveSnippet and Restore
// success branches: a saved+activated snippet archives (returns "archived",
// de-materialized) then restores (returns the active status, re-materialized).
func TestArchiveThenRestoreRoundTrips(t *testing.T) {
	pool := migratedPool(t)
	a, root := liveWriterAdapter(t, pool)
	ctx := context.Background()
	name := uniqueName("life-")

	// Save the snippet: it lives under active/ + export/ the moment this returns.
	if _, err := a.SaveSnippet(ctx, name, "python", "print('"+name+"')\n", "lifecycle snippet", false, false); err != nil {
		t.Fatalf("SaveSnippet: %v", err)
	}

	// Archive through the adapter: returns "archived" and de-materializes the export dir.
	archStatus, err := a.ArchiveSnippet(ctx, name)
	if err != nil {
		t.Fatalf("ArchiveSnippet: %v", err)
	}
	if archStatus != "archived" {
		t.Fatalf("ArchiveSnippet status = %q, want %q", archStatus, "archived")
	}
	if _, statErr := os.Stat(filepath.Join(root, "export", name)); !os.IsNotExist(statErr) {
		t.Errorf("after archive the export materialization should be gone (stat err=%v)", statErr)
	}

	// Restore through the adapter: returns the active status and re-materializes.
	restStatus, err := a.Restore(ctx, name)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restStatus != skills.StatusActive {
		t.Fatalf("Restore status = %q, want %q", restStatus, skills.StatusActive)
	}
	if _, statErr := os.Stat(filepath.Join(root, "export", name, name+".py")); statErr != nil {
		t.Errorf("after restore the export code file should be back: %v", statErr)
	}
}
