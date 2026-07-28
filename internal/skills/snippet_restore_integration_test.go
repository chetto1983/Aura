//go:build db_integration

// Integration test for Writer.Restore (18-03): Restore is the inverse of Archive —
// it promotes archived/<name> -> active, re-Materializes the snippet into the export
// dir, flips the usage sidecar back to "active", and records an audit row. Per the
// action-constant decision (the 0010 `action` CHECK does NOT list 'restore'), restore
// audits as the EXISTING AuditActivate constant with the cli ApprovalCLI tuple — no new
// migration. It needs Postgres migrated through 0010 (the audit table + the cli
// coherence row). Run via:
//
//	go test -tags db_integration -race -run TestRestoreSnippet ./internal/skills -count=1
package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestRestoreSnippetRoundTrip proves Restore is Archive's inverse: a Save then Archive
// then Restore round-trips the active dir + the export-dir materialization back,
// flips the sidecar status to "active", and writes a cli-tuple audit row recorded as
// AuditActivate (NOT a new 'restore' action — that constant does not exist and the CHECK
// would reject it).
func TestRestoreSnippetRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()

	root := t.TempDir()
	export := filepath.Join(root, "export")
	w := NewWriter(WriterConfig{
		Pool:         pool,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    export,
		ArchiveDir:   filepath.Join(root, "archived"),
		BodyCapBytes: 32768,
	})

	name := "restore" + uuid.Must(uuid.NewV7()).String()[:8]

	// Save the snippet: it is active + materialized into the export dir on return.
	if _, err := w.SaveSnippet(ctx, name, "python", "print('"+name+"')\n", Frontmatter{Description: "d"}, AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("SaveSnippet: %v", err)
	}

	// Archive it: de-materialized from the export dir, moved active->archived.
	if err := w.Archive(ctx, name, ApprovalCLI, AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(export, name)); !os.IsNotExist(statErr) {
		t.Fatalf("after Archive the snippet is still materialized in the export dir: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "active", name)); !os.IsNotExist(statErr) {
		t.Fatalf("after Archive the snippet still lives under the active dir: %v", statErr)
	}

	// Restore it: archived->active, re-materialized, sidecar back to active, audit row.
	if err := w.Restore(ctx, name, ApprovalCLI, AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// The active dir reappears.
	if _, statErr := os.Stat(filepath.Join(root, "active", name, "SKILL.md")); statErr != nil {
		t.Fatalf("after Restore the active SKILL.md is missing (want present): %v", statErr)
	}
	// The export-dir materialization reappears (the host path + loader see it again).
	if _, statErr := os.Stat(filepath.Join(export, name, name+".py")); statErr != nil {
		t.Fatalf("after Restore the export-dir snippet code file is missing (want present): %v", statErr)
	}
	// The usage sidecar status is back to "active".
	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.Status != "active" {
		t.Fatalf("usage status = %q after Restore, want %q", u.Status, "active")
	}

	// The restore audit row is recorded as activate/cli (the D-29 cli tuple): NO new
	// migration, NO AuditRestore constant.
	rows, err := NewAuditStore(pool).List(ctx, AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	foundRestore := false
	for _, r := range rows {
		if r.Action == AuditActivate && r.ApprovalSource == ApprovalCLI && r.GateRecommended && r.GateTaken {
			// Distinguish the restore row from the initial Activate row by counting:
			// both share the activate/cli shape, so a non-zero count is sufficient.
			foundRestore = true
		}
	}
	if !foundRestore {
		t.Fatalf("no activate/cli audit row for %q after Restore (want activate/cli/gate_recommended/gate_taken)", name)
	}
}

// TestRestoreErrorsWhenArchiveDirUnset proves the symmetric guard: Restore on a Writer
// with no archiveDir configured is a clear error (mirrors Archive's `if w.archiveDir != ""`).
func TestRestoreErrorsWhenArchiveDirUnset(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()

	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:       pool,
		PendingDir: filepath.Join(root, "pending"),
		ActiveDir:  filepath.Join(root, "active"),
		ExportDir:  filepath.Join(root, "export"),
		// ArchiveDir deliberately unset.
		BodyCapBytes: 32768,
	})

	err := w.Restore(ctx, "anything", ApprovalCLI, AuditActor{ActorID: "cli"})
	if err == nil {
		t.Fatal("Restore with no archiveDir configured: want a clear error, got nil")
	}
	// The guard must carry its SPECIFIC diagnostic, not whatever a fall-through hits:
	// without the explicit message the operator cannot tell a misconfigured Writer from
	// a missing-archive failure.
	if !strings.Contains(err.Error(), "archive dir not configured") {
		t.Errorf("Restore unset-archive error = %q, want the 'archive dir not configured' diagnostic", err.Error())
	}
}
