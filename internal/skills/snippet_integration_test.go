//go:build db_integration

// Live by-path snippet exec E2E (SC#4): save -> materialize into the host export dir ->
// exec BY PATH via the host interpreter (os/exec) -> output captured -> usage stamped. It
// exercises the FULL 7e snippet runtime against the live stack on the HOST (the host
// terminal is the execution surface; no container), not a fake.
//
// Since amendment #97 this is the layer's acceptance test: a saved snippet is runnable
// with no step in between.
//
// Requires the DB stack up AND the export dir set, plus python3 on PATH (present on the
// CI ubuntu runner):
//
//	export AURA_SKILL_EXPORT_DIR=<host dir>
//	export POSTGRES_PASSWORD/AURA_DB_URL/AURA_DB_MIGRATE_URL  # migrated through 0010
//	go test -tags db_integration -race -run TestSnippetExec ./internal/skills/ -v
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when a required var is unset, so a
// skipped tier can never report falsely green in the pipeline.
package skills

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSnippetExec is the SC#4 by-path exec proof: the snippet is saved (which
// materializes it into the host export dir), resolved to its host path, and run BY PATH
// (python3 <exportDir>/<name>/<name>.py) on the host. The marker stdout proves the file
// is visible + executable; the usage stamp proves the deterministic operator stamp (D-19).
func TestSnippetExec(t *testing.T) {
	exportDir := envOrSkip(t, "AURA_SKILL_EXPORT_DIR")

	pool := migratedPool(t)
	ctx := context.Background()

	// A unique name keeps parallel/repeat runs from colliding on the shared export dir.
	name := "calc" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String()[:8], "-", "")
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         pool,
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    exportDir,
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    nil,
		BodyCapBytes: 32768,
	})
	t.Cleanup(func() {
		// Best-effort: remove the snippet from the shared export dir so a later run is clean.
		_ = Dematerialize(name, exportDir)
	})

	const marker = "snippet-e2e-ok 42"
	code := "print('" + marker + "')\n"
	res, err := w.SaveSnippet(ctx, name, "python", code, Frontmatter{Description: "adds"}, AuditActor{ActorID: "cli"})
	if err != nil {
		t.Fatalf("SaveSnippet: %v", err)
	}
	if res.Status != StatusActive {
		t.Fatalf("SaveSnippet status = %q, want %q", res.Status, StatusActive)
	}

	// NO activation step: the save materialized into the host export dir. This is the
	// exact failure amendment #97 was raised from — the agent saved a snippet, was told it
	// was pending, and could not run the file it had just written.
	use, err := w.UseSnippet(name)
	if err != nil {
		t.Fatalf("UseSnippet: %v", err)
	}
	if use.HostPath == "" {
		t.Fatalf("UseSnippet HostPath is empty, want a materialized host path")
	}

	// Run BY PATH on the host (interpreter + path, never the exec bit).
	cmd := exec.CommandContext(ctx, use.Interpreter, use.HostPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("by-path exec: %v, stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), marker) {
		t.Fatalf("by-path exec stdout = %q, want marker %q", stdout.String(), marker)
	}

	// Stamp usage (the deterministic operator stamp, D-19) and assert it bumped.
	if err := w.StampUsage(name, time.Now()); err != nil {
		t.Fatalf("StampUsage: %v", err)
	}
	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.UseCount != 1 || u.LastUsedAt.IsZero() {
		t.Fatalf("usage sidecar after stamp = %+v, want use_count=1 + last_used_at set", u)
	}
}
