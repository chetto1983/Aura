//go:build sandbox_integration && db_integration

// Live workspace symlink-escape cascade tier (ROADMAP CAP-02 criterion 2). It plants
// a REAL symlink to /etc inside a live session container's bind-mounted /workspace,
// then runs the HOST-side WorkspaceManager.PurgeConversationDir cascade and asserts:
//
//	(1) the host /etc tree is UNTOUCHED (the os.Root no-follow walk unlinks the LINK,
//	    never traverses into the symlink target — D-13/D-14, T-08-05-EOP-SYMLINK), and
//	(2) the planted symlink is removed (the cascade actually cleaned the workspace).
//
// This is the live counterpart to the unit-tier TestWorkspace_SymlinkEscapeCascade
// (workspace_test.go): the unit test plants the symlink with os.Symlink on the host;
// this test plants it from INSIDE the container as the attacker would, proving the
// host cascade is safe against a symlink the sidecar (untrusted) created.
//
// Requires the live stack (BOTH tags) + AURA_RUN_DIR pointing at the host run dir the
// container bind-mounts (08-08 wiring). No-skip-as-green: runDirOrSkip t.Fatals under
// $CI when AURA_RUN_DIR is unset.
package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// runDirOrSkip resolves the host run dir whose conversations/<id>/workspace subtree the
// session container bind-mounts. It MUST agree with the SessionManager's mount source so
// the host cascade operates on the very dir the container wrote into.
func runDirOrSkip(t *testing.T) string {
	t.Helper()
	v := os.Getenv("AURA_RUN_DIR")
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("live cascade tier requires AURA_RUN_DIR (the host workspace mount root), unset under CI — " +
				"a skipped integration test must not pass as green; wire it in sandbox.yml")
		}
		t.Skip("live cascade tier requires AURA_RUN_DIR + a live Docker daemon")
	}
	return v
}

// TestWorkspace_SymlinkEscapeCascade_Live is ROADMAP CAP-02 criterion 2, live. It:
//  1. acquires a live session container for a conversation,
//  2. execs `ln -s /etc /workspace/escape` INSIDE the container (the attacker plants a
//     symlink in the sidecar-writable workspace),
//  3. runs the HOST-side PurgeConversationDir cascade over <runDir>/conversations/<id>,
//  4. asserts host /etc still has its files (the cascade did NOT follow the link) and the
//     planted symlink is gone.
//
// The host-only counterpart (symlink planted with os.Symlink) is the unit-tier
// TestWorkspace_SymlinkEscapeCascade in workspace_test.go.
func TestWorkspace_SymlinkEscapeCascade_Live(t *testing.T) {
	pool := migratedPool(t)
	runner := integrationRunner(t)
	runDir := runDirOrSkip(t)
	m, _ := liveManager(t, pool, 1800*time.Second, time.Hour, "", "", nil)

	convID := newConversationRow(t, pool).String()
	ctx := ctxT(t)

	sess, err := m.Acquire(ctx, convID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Plant the escaping symlink from inside the container (attacker's position).
	plant, err := runner.RunShellSession(ctx, convID, "ln -s /etc /workspace/escape && ls -la /workspace/escape", 20)
	if err != nil {
		m.Release(sess)
		t.Fatalf("plant symlink: %v", err)
	}
	if plant.ExitCode != 0 {
		m.Release(sess)
		t.Fatalf("ln -s in container failed: exit %d stderr=%q", plant.ExitCode, plant.Stderr)
	}
	m.Release(sess)

	// Sentinel: a real host /etc entry that MUST survive the cascade. /etc/hostname
	// exists on every Linux host the sidecar image targets.
	const sentinel = "/etc/hostname"
	before, err := os.Stat(sentinel)
	if err != nil {
		t.Fatalf("host sentinel %s must exist before cascade: %v", sentinel, err)
	}

	// Run the HOST cascade on the conversation dir (the os.Root no-follow purge).
	wm := NewWorkspaceManager(runDir, 0)
	if err := wm.PurgeConversationDir(convID); err != nil {
		t.Fatalf("PurgeConversationDir: %v", err)
	}

	// (1) Host /etc untouched: the sentinel still exists with the same identity.
	after, err := os.Stat(sentinel)
	if err != nil {
		t.Fatalf("FATAL: host %s gone after cascade — symlink was followed (escape!): %v", sentinel, err)
	}
	if !os.SameFile(before, after) {
		t.Fatalf("FATAL: host %s changed identity after cascade — symlink target was mutated", sentinel)
	}

	// (2) The planted symlink (and the whole conversation dir) is gone.
	escape := filepath.Join(runDir, "conversations", convID, "workspace", "escape")
	if _, err := os.Lstat(escape); !os.IsNotExist(err) {
		t.Fatalf("planted symlink must be removed by the cascade, Lstat err=%v", err)
	}
	convDir := filepath.Join(runDir, "conversations", convID)
	if _, err := os.Stat(convDir); !os.IsNotExist(err) {
		t.Fatalf("conversation dir must be removed by the cascade, Stat err=%v", err)
	}
}
