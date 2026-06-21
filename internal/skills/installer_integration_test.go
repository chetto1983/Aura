//go:build db_integration

// Integration test for the skills install transport (plan 29-03 task 1). It requires
// the migrations applied through 0010 (see audit_store_integration_test.go header for
// the env + invocation). No-skip-as-green: the shared envOrSkip t.Fatals under $CI, so
// a skipped tier can never report falsely green. This is the Task-1 live-PG backstop —
// a real Install through WriteInstallPending appends EXACTLY ONE skill_audit install row
// (newest-first), the staged tree lands in pending/, and the active loader never sees it.
package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// fakeAddIntegration is the db_integration analog of fakeAdd: it stages a SKILL.md tree
// under <dir>/.claude/skills/<name>/ exactly as `npx skills add` would, so the transport
// runs end to end (strip→stage→parse→hash→validate→WriteInstallPending) WITHOUT a real
// network call, while the hand-off DOES exercise the live db.WithTx audit INSERT.
func fakeAddIntegration(name, body string) CommandRunner {
	return func(_ context.Context, dir, _ string, _ ...string) (string, error) {
		skillDir := filepath.Join(dir, ".claude", "skills", name)
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			return "", err
		}
		md := "---\nname: " + name + "\ndescription: live install fixture\ntype: instruction\n---\n" + body
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o600); err != nil {
			return "", err
		}
		return "Installed 1 skill → .claude/skills/" + name + "\n", nil
	}
}

// TestInstallerAuditAppendOnly proves the Task-1 no-skip-as-green backstop: a real
// Install through WriteInstallPending appends exactly ONE skill_audit install row
// (newest-first, the D-29 pending tuple: NULL source/token, gate_recommended=true,
// gate_taken=false), the staged tree lands in pending/<name>/, and the active loader
// does NOT see the pending skill (it scans active/ only).
func TestInstallerAuditAppendOnly(t *testing.T) {
	pool := migratedPool(t)
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         pool,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	store := NewAuditStore(pool)

	name := "ins-" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String()[:8], "-", "")
	inst := NewInstaller(InstallerConfig{
		Writer:       w,
		Run:          fakeAddIntegration(name, "a clean live install body"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})

	info, err := inst.Install(t.Context(), "owner/"+name, AuditActor{ActorID: "operator", IdentityID: "local"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if info.Status != StatusPendingApproval {
		t.Fatalf("Install status = %q, want %q (pending, never self-activated)", info.Status, StatusPendingApproval)
	}
	if info.RiskTier != "risky" {
		t.Errorf("install must be RISKY, got %q", info.RiskTier)
	}
	if info.ContentHash == "" {
		t.Error("install must carry a canonical content hash")
	}

	// The staged tree landed in pending/<name>/.
	if _, serr := os.Stat(filepath.Join(root, "pending", name, "SKILL.md")); serr != nil {
		t.Errorf("staged skill missing from pending/: %v", serr)
	}

	// The active loader does NOT see the pending skill (it scans active/ only).
	loader := NewLoader(Config{Roots: []string{filepath.Join(root, "active")}, Blocklist: []string{"<|im_start|>"}})
	if _, ok := loader.Get(name); ok {
		t.Fatalf("pending skill %q must NOT be visible to the active loader", name)
	}

	// Exactly ONE install audit row, newest-first, with the D-29 pending tuple.
	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	installRows := 0
	for _, r := range rows {
		if r.Action == AuditInstall {
			installRows++
			if r.ApprovalSource != ApprovalNone || r.PausedStateToken != "" {
				t.Errorf("install row tuple = (%q,%q), want NULL source+token (pending)", r.ApprovalSource, r.PausedStateToken)
			}
			if !r.GateRecommended || r.GateTaken {
				t.Errorf("install row gate = (rec=%v,taken=%v), want (true,false)", r.GateRecommended, r.GateTaken)
			}
			if r.ContentHash != info.ContentHash {
				t.Errorf("install row hash = %q, want %q", r.ContentHash, info.ContentHash)
			}
		}
	}
	if installRows != 1 {
		t.Fatalf("install audit rows = %d, want exactly 1", installRows)
	}
	// Newest-first ordering: the install row is the most recent for this skill.
	if len(rows) > 0 && rows[0].Action != AuditInstall {
		t.Errorf("newest-first: rows[0].Action = %q, want install", rows[0].Action)
	}
}
