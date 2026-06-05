//go:build db_integration

// Integration test for the snippet TTL sweep (D-16): SweepExpiredSnippets archives a
// snippet past the TTL (de-materialize + active->archived + a D-29 `auto` audit row)
// and KEEPS a fresh one. It needs Postgres migrated through 0010 (the audit table +
// the auto-archive coherence row). Run via:
//
//	go test -tags db_integration -race -run TestSkillTTLSweep ./internal/skills -count=1
package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSkillTTLSweep proves the sweep archives a stale snippet and keeps a fresh one,
// de-materializing the stale one from the export dir and writing the D-29 `auto` audit
// row (gate_recommended=false, gate_taken=true).
func TestSkillTTLSweep(t *testing.T) {
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

	suffix := uuid.Must(uuid.NewV7()).String()[:8]
	stale := "stale" + suffix
	fresh := "fresh" + suffix
	now := time.Now().UTC()
	ttl := 90 * 24 * time.Hour

	// Save + activate both snippets (materializes them into the export dir).
	for _, name := range []string{stale, fresh} {
		if _, err := w.SaveSnippet(ctx, name, "python", "print('"+name+"')\n", Frontmatter{Description: "d"}, AuditActor{ActorID: "cli"}); err != nil {
			t.Fatalf("SaveSnippet %q: %v", name, err)
		}
		if err := w.Activate(ctx, name, ApprovalCLI, "", AuditActor{ActorID: "cli"}); err != nil {
			t.Fatalf("Activate %q: %v", name, err)
		}
	}

	// Make the stale one last-used past the TTL; the fresh one used just now.
	if err := w.StampUsage(stale, now.Add(-ttl-time.Hour)); err != nil {
		t.Fatalf("StampUsage stale: %v", err)
	}
	if err := w.StampUsage(fresh, now); err != nil {
		t.Fatalf("StampUsage fresh: %v", err)
	}

	res, err := w.SweepExpiredSnippets(ctx, ttl, now, AuditActor{ActorID: "auto"})
	if err != nil {
		t.Fatalf("SweepExpiredSnippets: %v", err)
	}

	if !containsName(res.Archived, stale) {
		t.Fatalf("archived = %v, want it to include the stale snippet %q", res.Archived, stale)
	}
	if containsName(res.Archived, fresh) {
		t.Fatalf("archived = %v, must NOT include the fresh snippet %q", res.Archived, fresh)
	}
	if !containsName(res.Kept, fresh) {
		t.Fatalf("kept = %v, want it to include the fresh snippet %q", res.Kept, fresh)
	}

	// The stale snippet is de-materialized from the export dir; the fresh one remains.
	if _, statErr := os.Stat(filepath.Join(export, stale)); !os.IsNotExist(statErr) {
		t.Fatalf("stale snippet still materialized in export dir (want removed): %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(export, fresh)); statErr != nil {
		t.Fatalf("fresh snippet missing from export dir (want kept): %v", statErr)
	}

	// The sweep wrote the D-29 auto audit row for the stale archive.
	rows, err := NewAuditStore(pool).List(ctx, AuditFilter{SkillName: stale})
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	foundAuto := false
	for _, r := range rows {
		if r.Action == AuditAutoArchive && r.ApprovalSource == ApprovalAuto && !r.GateRecommended && r.GateTaken {
			foundAuto = true
		}
	}
	if !foundAuto {
		t.Fatalf("no D-29 auto_archive audit row for %q (want auto/!gate_recommended/gate_taken)", stale)
	}
}

func containsName(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
