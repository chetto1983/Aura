//go:build db_integration

package conversations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

func mkConvDir(t *testing.T, runDir, id string) string {
	t.Helper()
	dir := filepath.Join(runDir, "conversations", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1.content"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return dir
}

// TestScanOrphans_RemovesOrphanKeepsLive: a conversations/<orphan> dir with no DB
// row is removed; a dir WITH a live conversations row survives.
func TestScanOrphans_RemovesOrphanKeepsLive(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	s := New(pool, Config{RunDir: runDir, TurnCapBytes: 65536})
	ctx := ownerCtx()

	liveID := newConversation(t, s)
	liveDir := mkConvDir(t, runDir, liveID)

	orphanID := "00000000-0000-0000-0000-0000000000ff" // valid uuid, no DB row
	orphanDir := mkConvDir(t, runDir, orphanID)

	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: runDir}); err != nil {
		t.Fatalf("ScanOrphans: %v", err)
	}

	if _, err := os.Stat(liveDir); err != nil {
		t.Errorf("live conversation dir must survive: %v", err)
	}
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Errorf("orphan dir must be removed, stat err=%v", err)
	}
}

// TestScanOrphans_SymlinkNotFollowed: a symlink under conversations/ pointing at an
// EXTERNAL dir is unlinked but never traversed — the external target survives.
func TestScanOrphans_SymlinkNotFollowed(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	ctx := ownerCtx()

	// An external dir with a sentinel file that MUST survive.
	external := t.TempDir()
	sentinel := filepath.Join(external, "DO_NOT_DELETE")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	convRoot := filepath.Join(runDir, "conversations")
	if err := os.MkdirAll(convRoot, 0o755); err != nil {
		t.Fatalf("mkdir convRoot: %v", err)
	}
	link := filepath.Join(convRoot, "evil-symlink")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: runDir}); err != nil {
		t.Fatalf("ScanOrphans: %v", err)
	}

	// The external target + its sentinel MUST still exist (RemoveAll never followed
	// the symlink). The symlink entry itself may be unlinked.
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("EoP: external target must survive the symlink-guarded scan: %v", err)
	}
}

// TestScanOrphans_SizeWarnDoesNotPurge: a run dir over the threshold logs a WARN but
// the content is NOT purged (audit-only).
func TestScanOrphans_SizeWarnDoesNotPurge(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	s := New(pool, Config{RunDir: runDir, TurnCapBytes: 65536})
	ctx := ownerCtx()

	liveID := newConversation(t, s)
	liveDir := mkConvDir(t, runDir, liveID)
	// Make the live dir's sidecar large enough to exceed a tiny threshold.
	big := strings.Repeat("x", 4096)
	if err := os.WriteFile(filepath.Join(liveDir, "1.content"), []byte(big), 0o644); err != nil {
		t.Fatalf("write big sidecar: %v", err)
	}

	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: runDir, WarnThresholdBytes: 1}); err != nil {
		t.Fatalf("ScanOrphans: %v", err)
	}
	// Audit-only: the live dir + its content survive despite the size WARN.
	if _, err := os.Stat(filepath.Join(liveDir, "1.content")); err != nil {
		t.Errorf("size WARN must NOT purge (audit-only): %v", err)
	}
}

// TestScanOrphans_SweepsTmp: a tmp/ entry older than the TTL is swept; a fresh one
// survives.
func TestScanOrphans_SweepsTmp(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	ctx := ownerCtx()

	tmpRoot := filepath.Join(runDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	old := filepath.Join(tmpRoot, "old.scratch")
	fresh := filepath.Join(tmpRoot, "fresh.scratch")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %q: %v", p, err)
		}
	}
	// Backdate the old entry past the 24h TTL.
	stale := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: runDir}); err != nil {
		t.Fatalf("ScanOrphans: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("stale tmp entry must be swept, stat err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh tmp entry must survive: %v", err)
	}
}

// TestScanOrphans_ReconcilesCrashOrphanContentSidecars: inside a LIVE conversation
// dir the boot scan reconciles ONLY an unreferenced+aged <seq>.content (a crash
// between the sidecar write and the DB commit). A referenced .content (even aged), an
// unreferenced+young .content, a co-located <spillID>.result tool sidecar, and a
// symlink at a .content leaf all survive (D-09 / LOOP-09 / F-040). The referenced set
// comes from the real ListSpilledSeqsForConversation query, not a directory heuristic.
func TestScanOrphans_ReconcilesCrashOrphanContentSidecars(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	s := New(pool, Config{RunDir: runDir, TurnCapBytes: 64}) // tiny cap → force a spill
	ctx := ownerCtx()

	convID := newConversation(t, s)
	dir := filepath.Join(runDir, "conversations", convID)

	// A genuinely referenced spilled turn: committed row + <seq>.content on disk.
	if err := s.AppendTurn(ctx, AppendTurnParams{
		ConversationID: convID, Seq: 1, Role: llm.RoleAssistant, Content: strings.Repeat("z", 200),
	}); err != nil {
		t.Fatalf("AppendTurn spill: %v", err)
	}
	referenced := filepath.Join(dir, "1.content")
	if _, err := os.Stat(referenced); err != nil {
		t.Fatalf("referenced sidecar must exist post-append: %v", err)
	}

	aged := time.Now().Add(-sidecarOrphanGrace - time.Hour)
	// Referenced but AGED — referencing must beat age.
	if err := os.Chtimes(referenced, aged, aged); err != nil {
		t.Fatalf("chtimes referenced: %v", err)
	}

	orphanAged := filepath.Join(dir, "100.content") // unreferenced + aged → removed
	if err := os.WriteFile(orphanAged, []byte("crash orphan"), 0o600); err != nil {
		t.Fatalf("write orphanAged: %v", err)
	}
	if err := os.Chtimes(orphanAged, aged, aged); err != nil {
		t.Fatalf("chtimes orphanAged: %v", err)
	}

	orphanYoung := filepath.Join(dir, "101.content") // unreferenced + young → survives (grace)
	if err := os.WriteFile(orphanYoung, []byte("in flight"), 0o600); err != nil {
		t.Fatalf("write orphanYoung: %v", err)
	}

	toolResult := filepath.Join(dir, "toolcall-xyz.result") // .result → survives (strict .content)
	if err := os.WriteFile(toolResult, []byte("tool output"), 0o600); err != nil {
		t.Fatalf("write toolResult: %v", err)
	}
	if err := os.Chtimes(toolResult, aged, aged); err != nil { // aged .result still survives
		t.Fatalf("chtimes toolResult: %v", err)
	}

	// A symlink named like a .content leaf, target OUTSIDE runDir → never followed/removed.
	external := t.TempDir()
	sentinel := filepath.Join(external, "SENTINEL")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	symlink := filepath.Join(dir, "200.content")
	symlinkMade := true
	if err := os.Symlink(sentinel, symlink); err != nil {
		symlinkMade = false
		t.Logf("symlinks unavailable, skipping symlink assertion: %v", err)
	}

	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: runDir}); err != nil {
		t.Fatalf("ScanOrphans: %v", err)
	}

	if _, err := os.Stat(orphanAged); !os.IsNotExist(err) {
		t.Errorf("unreferenced+aged .content must be reconciled away, stat err=%v", err)
	}
	if _, err := os.Stat(referenced); err != nil {
		t.Errorf("referenced .content must survive regardless of age: %v", err)
	}
	if _, err := os.Stat(orphanYoung); err != nil {
		t.Errorf("unreferenced+young .content must survive (grace): %v", err)
	}
	if _, err := os.Stat(toolResult); err != nil {
		t.Errorf(".result tool sidecar must survive the strict .content match: %v", err)
	}
	if symlinkMade {
		if _, err := os.Lstat(symlink); err != nil {
			t.Errorf("symlink leaf must survive (never removed): %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Errorf("symlink target must survive (never followed): %v", err)
		}
	}
}

// TestScanOrphans_NoRunDirIsNoop: an empty RunDir or a missing conversations dir is
// a clean no-op.
func TestScanOrphans_NoRunDirIsNoop(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: ""}); err != nil {
		t.Errorf("empty run dir must be a no-op, got %v", err)
	}
	if err := ScanOrphans(ctx, pool, ScanParams{RunDir: t.TempDir()}); err != nil {
		t.Errorf("missing conversations dir must be a no-op, got %v", err)
	}
}
