// Unit tier for the WorkspaceManager os.Root walkers. Shares the package-level
// goleak TestMain from docker_test.go (//go:build !sandbox_integration) — no
// TestMain here. These tests plant attacker-shaped symlinks in a temp tree and
// prove the cascade unlinks the LINK, never the target (ROADMAP criterion 2,
// T-08-05-EOP-SYMLINK), and that walkSize never follows a symlinked dir.
package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// mustSymlink creates a symlink or skips the test when the platform forbids it
// (Windows without SeCreateSymbolicLinkPrivilege). The deployment + CI target is
// Linux where symlinks always work, so this skip never fires under $CI — guarded
// so a skipped symlink-escape gate cannot pass falsely green on the CI Linux host.
func mustSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("symlink %q -> %q failed under CI (the symlink-escape gate MUST run on Linux): %v",
				newname, oldname, err)
		}
		t.Skipf("symlink unsupported on this host (need privilege on Windows); the escape gate runs on Linux/CI: %v", err)
	}
}

func TestWorkspace_SymlinkEscapeCascade(t *testing.T) {
	runDir := t.TempDir()
	// Stand-in for /etc: a host tree OUTSIDE the conversation dir that MUST survive.
	hostSecret := t.TempDir()
	secretFile := filepath.Join(hostSecret, "passwd")
	if err := os.WriteFile(secretFile, []byte("root:x:0:0"), 0o600); err != nil {
		t.Fatalf("seed host secret: %v", err)
	}

	wm := NewWorkspaceManager(runDir, 100<<20)
	convID := "11111111-1111-1111-1111-111111111111"
	ws, err := wm.EnsureDir(convID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "data.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed workspace file: %v", err)
	}
	// Attacker plants a symlink pointing OUT of the workspace at the host secret dir.
	escape := filepath.Join(ws, "escape")
	mustSymlink(t, hostSecret, escape)

	if err := wm.PurgeConversationDir(convID); err != nil {
		t.Fatalf("PurgeConversationDir: %v", err)
	}

	// The host secret (symlink TARGET) must be UNTOUCHED — the cascade unlinked the
	// link, never followed it.
	if _, err := os.Stat(secretFile); err != nil {
		t.Fatalf("host secret was removed through the symlink (D-14 violation): %v", err)
	}
	// The conversation dir (and the escape symlink entry within it) must be gone.
	convDir := filepath.Join(runDir, "conversations", convID)
	if _, err := os.Lstat(convDir); !os.IsNotExist(err) {
		t.Fatalf("conversation dir survived purge: err=%v", err)
	}
}

func TestWorkspace_QuotaWalkSize(t *testing.T) {
	runDir := t.TempDir()
	outside := t.TempDir()
	// A big file OUTSIDE the workspace that a symlinked dir would erroneously sum.
	if err := os.WriteFile(filepath.Join(outside, "huge.bin"), make([]byte, 4096), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	wm := NewWorkspaceManager(runDir, 100<<20)
	convID := "22222222-2222-2222-2222-222222222222"
	ws, err := wm.EnsureDir(convID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	// 10 + 20 = 30 bytes of real regular files.
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), make([]byte, 10), 0o600); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o700); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "b.txt"), make([]byte, 20), 0o600); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	// A symlinked dir pointing outside must NOT be traversed/summed.
	mustSymlink(t, outside, filepath.Join(ws, "linkdir"))

	size, err := wm.WalkSize(convID)
	if err != nil {
		t.Fatalf("WalkSize: %v", err)
	}
	if size != 30 {
		t.Fatalf("WalkSize = %d, want 30 (regular files only, no symlink traversal)", size)
	}
}

func TestWorkspace_QuotaExceeded(t *testing.T) {
	runDir := t.TempDir()
	wm := NewWorkspaceManager(runDir, 16) // 16-byte quota
	convID := "33333333-3333-3333-3333-333333333333"
	ws, err := wm.EnsureDir(convID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "big.txt"), make([]byte, 32), 0o600); err != nil {
		t.Fatalf("seed big: %v", err)
	}
	if err := wm.CheckQuota(convID); err == nil {
		t.Fatalf("CheckQuota over a 16-byte quota with a 32-byte file: want ErrWorkspaceQuotaExceeded, got nil")
	}
}
