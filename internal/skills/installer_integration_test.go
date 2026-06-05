//go:build db_integration

// Integration test for the full Installer.Install pipeline (clone → stage → pending
// → audit). Requires Postgres migrated through 0010 (same env as the audit-store
// integration tests) AND a git binary. Run:
//
//	go test -tags db_integration -race -run TestInstallFullPipeline ./internal/skills -count=1
package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestInstallFullPipeline proves SC#1 end-to-end: a native clone of a local file://
// skill repo lands the skill in pending/ and writes ONE install audit row (the D-29
// pending tuple: NULL source/token, gate_recommended, !gate_taken) carrying the
// canonical content hash — and NEVER self-activates (no active dir, no approved row).
func TestInstallFullPipeline(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	pool := migratedPool(t)
	ctx := context.Background()

	root := t.TempDir()
	pendingDir := filepath.Join(root, "pending")
	w := NewWriter(WriterConfig{
		Pool:         pool,
		PendingDir:   pendingDir,
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	inst := NewInstaller(InstallerConfig{Writer: w, Blocklist: []string{"<|im_start|>"}, BodyCapBytes: 32768})

	name := "ipipe-" + uuid.Must(uuid.NewV7()).String()[:8]
	repoURL := installRepoFixture(t, gitBin, name)

	res, err := inst.Install(ctx, repoURL, InstallOpts{Actor: AuditActor{ActorID: "model"}})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Status != StatusPendingApproval {
		t.Errorf("install status: want %q (never self-activates), got %q", StatusPendingApproval, res.Status)
	}
	if !res.AlwaysStripped {
		t.Errorf("always:true fixture should report AlwaysStripped")
	}
	if !strings.HasPrefix(res.ContentHash, "sha256:") {
		t.Errorf("content hash missing sha256: prefix: %q", res.ContentHash)
	}

	// The skill landed in pending/, NOT active/ (no self-activation, D-03/T-11-06-E2).
	if _, err := os.Stat(filepath.Join(pendingDir, name, "SKILL.md")); err != nil {
		t.Errorf("skill not in pending dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "active", name)); !os.IsNotExist(err) {
		t.Errorf("skill self-activated into active dir (must not): %v", err)
	}

	// Exactly one install audit row, the pending tuple, with the canonical hash.
	rows, err := NewAuditStore(pool).List(ctx, AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 install audit row, got %d", len(rows))
	}
	r := rows[0]
	if r.Action != AuditInstall {
		t.Errorf("audit action: want %q, got %q", AuditInstall, r.Action)
	}
	if r.ApprovalSource != ApprovalNone || r.GateTaken {
		t.Errorf("install audit should be the pending tuple (NULL source, !gate_taken); got src=%q gate_taken=%v", r.ApprovalSource, r.GateTaken)
	}
	if r.ContentHash != res.ContentHash {
		t.Errorf("audit content_hash %q != install result hash %q", r.ContentHash, res.ContentHash)
	}
}

// installRepoFixture builds a local file:// repo with an always:true skill so the
// pipeline test exercises the D-10 strip + the canonical hash + the audit row.
func installRepoFixture(t *testing.T, gitBin, name string) string {
	t.Helper()
	repo := t.TempDir()
	runGitIT(t, gitBin, repo, "init", "-q")
	skillDir := filepath.Join(repo, "skills", name)
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skill := "---\nname: " + name + "\ndescription: pipeline skill\ntype: instruction\nalways: true\n---\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	runGitIT(t, gitBin, repo, "add", "-A")
	runGitIT(t, gitBin, repo, "commit", "-q", "-m", "skill")
	return "file://" + filepath.ToSlash(repo)
}

func runGitIT(t *testing.T, gitBin, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
