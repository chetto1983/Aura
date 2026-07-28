//go:build db_integration

// Integration test for the skills install transport (plan 29-03 task 1). It requires
// the migrations applied through 0010 (see audit_store_integration_test.go header for
// the env + invocation). No-skip-as-green: the shared envOrSkip t.Fatals under $CI, so
// a skipped tier can never report falsely green. This is the Task-1 live-PG backstop —
// a real Install through WriteInstall appends EXACTLY ONE skill_audit install row
// (newest-first), the tree lands active + materialized, and the loader sees it.
package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeAddIntegration is the db_integration analog of fakeAdd: it stages a SKILL.md tree
// under <dir>/.claude/skills/<name>/ exactly as `npx skills add` would, so the transport
// runs end to end (strip→stage→parse→hash→validate→WriteInstall) WITHOUT a real network
// call, while the hand-off DOES exercise the live db.WithTx audit INSERT.
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
// Install through WriteInstall appends exactly ONE skill_audit install row (newest-first,
// the actor tuple: 'cli' source, NULL token, gate_recommended=true, gate_taken=true), the
// tree lands in active/<name>/ AND in the export dir the sandbox mounts, and the loader
// SEES it — which after amendment #97 is the whole point: an install that returns is an
// install the model can use on the next turn.
//
// The board-visible Name is asserted here too (it was its own repro file when installs
// still went to a staging tab): a skills.sh install must surface its human frontmatter
// name, never a UUID/identifier.
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
	if info.Status != StatusActive {
		t.Fatalf("Install status = %q, want %q", info.Status, StatusActive)
	}
	if info.Name != name {
		t.Errorf("Install Name = %q, want the human frontmatter name %q (a UUID here is the reported board bug)", info.Name, name)
	}
	if info.Destination != filepath.Join(root, "active", name) {
		t.Errorf("Install Destination = %q, want the active dir", info.Destination)
	}
	if info.RiskTier != "risky" {
		t.Errorf("install must be RISKY, got %q", info.RiskTier)
	}
	if info.ContentHash == "" {
		t.Error("install must carry a canonical content hash")
	}

	// The tree landed active AND materialized into the export dir the sandbox mounts.
	for _, dir := range []string{"active", "export"} {
		if _, serr := os.Stat(filepath.Join(root, dir, name, "SKILL.md")); serr != nil {
			t.Errorf("installed skill missing from %s/: %v", dir, serr)
		}
	}

	// The loader SEES it: the install is usable on the next turn, with its human name.
	loader := NewLoader(Config{Roots: []string{filepath.Join(root, "active")}, Blocklist: []string{"<|im_start|>"}})
	loaded, ok := loader.Get(name)
	if !ok {
		t.Fatalf("installed skill %q must be visible to the loader", name)
	}
	if loaded.Name != name {
		t.Errorf("loaded skill Name = %q, want the human name %q", loaded.Name, name)
	}

	// Exactly ONE install audit row, newest-first, with the actor tuple.
	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	installRows := 0
	for _, r := range rows {
		if r.Action == AuditInstall {
			installRows++
			if r.ApprovalSource != ApprovalCLI || r.PausedStateToken != "" {
				t.Errorf("install row tuple = (%q,%q), want ('cli', NULL token)", r.ApprovalSource, r.PausedStateToken)
			}
			if !r.GateRecommended || !r.GateTaken {
				t.Errorf("install row gate = (rec=%v,taken=%v), want (true,true)", r.GateRecommended, r.GateTaken)
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

// skillAuditSQLState extracts the SQLSTATE from a pg error, "" otherwise.
func skillAuditSQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// TestSkillAuditAppendOnly is the both-ledgers append-only backstop (SPEC Prohibition #4 /
// T-29-05-02): the SKILL side, mirroring internal/mcp/manager TestMCPAuditAppendOnly. It
// drives the real lifecycle (install → archive → restore) over the Writer/Installer —
// each appends EXACTLY ONE row of the expected action newest-first — and then proves
// aura.skill_audit is immutable: UPDATE/DELETE/TRUNCATE as aura_app each raise
// insufficient_privilege (42501), and every row survives. (NOTE: restore audits as the
// EXISTING 'activate' action — there is deliberately no 'restore' constant; see
// writer_activate.go Restore. Since amendment #97 the install activates itself, so there
// is no separate activation step in the lifecycle.)
func TestSkillAuditAppendOnly(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
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

	name := "led-" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String()[:8], "-", "")
	inst := NewInstaller(InstallerConfig{
		Writer:       w,
		Run:          fakeAddIntegration(name, "an append-only lifecycle body"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})

	// install → one install row; the skill is active when this returns.
	if _, err := inst.Install(t.Context(), "owner/"+name, AuditActor{ActorID: "operator", IdentityID: "local"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// archive → one archive row.
	if err := w.Archive(t.Context(), name, ApprovalCLI, AuditActor{ActorID: "cli", IdentityID: "local"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// restore → recorded as an additional 'activate' action (the load-bearing decision).
	if err := w.Restore(t.Context(), name, ApprovalCLI, AuditActor{ActorID: "cli", IdentityID: "local"}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	counts := map[AuditAction]int{}
	for _, r := range rows {
		counts[r.Action]++
	}
	if counts[AuditInstall] != 1 {
		t.Errorf("install rows = %d, want exactly 1", counts[AuditInstall])
	}
	if counts[AuditArchive] != 1 {
		t.Errorf("archive rows = %d, want exactly 1", counts[AuditArchive])
	}
	// restore records as 'activate' (no 'restore' action constant).
	if counts[AuditActivate] != 1 {
		t.Errorf("activate rows (the restore) = %d, want exactly 1", counts[AuditActivate])
	}
	if len(rows) != 3 {
		t.Fatalf("lifecycle rows = %d, want exactly 3 (install+archive+restore)", len(rows))
	}
	// Newest-first: restore (the last action, recorded as activate) leads.
	if rows[0].Action != AuditActivate {
		t.Errorf("newest-first: rows[0].Action = %q, want activate (the restore)", rows[0].Action)
	}

	// --- Append-only: aura_app cannot UPDATE / DELETE / TRUNCATE the skill ledger ---
	if _, err := pool.Exec(t.Context(), "UPDATE aura.skill_audit SET content_hash = 'tampered' WHERE skill_name = $1", name); err == nil {
		t.Error("UPDATE aura.skill_audit as aura_app: want denied, got nil error")
	} else if code := skillAuditSQLState(err); code != "42501" {
		t.Errorf("UPDATE SQLSTATE = %q, want 42501 insufficient_privilege", code)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM aura.skill_audit WHERE skill_name = $1", name); err == nil {
		t.Error("DELETE aura.skill_audit as aura_app: want denied, got nil error")
	} else if code := skillAuditSQLState(err); code != "42501" {
		t.Errorf("DELETE SQLSTATE = %q, want 42501 insufficient_privilege", code)
	}
	if _, err := pool.Exec(t.Context(), "TRUNCATE aura.skill_audit"); err == nil {
		t.Error("TRUNCATE aura.skill_audit as aura_app: want denied, got nil error")
	} else if code := skillAuditSQLState(err); code != "42501" {
		t.Errorf("TRUNCATE SQLSTATE = %q, want 42501 insufficient_privilege", code)
	}

	// All four rows survived every mutation attempt.
	after, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List after mutation attempts: %v", err)
	}
	if len(after) != 3 {
		t.Fatalf("skill_audit rows tampered or removed: want 3 surviving rows, got %d", len(after))
	}
}
