//go:build db_integration

// Mutation-hardening tests for writer_activate.go (Phase 18 Gate-3 spot-check). These
// pin the lifecycle behaviours go-mutesting found unguarded:
//
//   - Delete actually removes BOTH the active dir and the export subtree (a no-op'd
//     RemoveAll leaves the skill loadable, or leaves it in the sandbox's /skills mount
//     after the ledger says it is gone);
//   - SetAlways actually persists the flipped always flag to the active SKILL.md (a
//     skipped assignment silently no-ops the operator's `aura skills always` command);
//   - every lifecycle audit wrap (activate/archive/restore/delete/set-always) surfaces
//     a db failure as a prefixed "<action> ...: audit:" error instead of swallowing it;
//   - the restore guards (SanitizeName wrap + "archive dir not configured") carry their
//     diagnostic message, not a downstream substitute.
//
// They need Postgres migrated through 0010 (the audit table). Run via:
//
//	go test -tags db_integration -race -run 'TestDelete|TestSetAlways|TestLifecycleAudit|TestRestore' ./internal/skills -count=1
package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
	"github.com/google/uuid"
)

// TestWriteMutationTakesEffectForEveryActor is amendment #97's core claim: the actor no
// longer selects a code path. A model-authored skill, a model-authored always:true skill
// (which enters every future turn's messages[1] block) and an operator-authored one all
// land active AND materialized into the export dir the sandbox mounts, and each records
// its own actor in the ledger — the only place that distinction now survives.
func TestWriteMutationTakesEffectForEveryActor(t *testing.T) {
	w, root := newMutationWriter(t)
	ctx := context.Background()
	store := NewAuditStore(w.pool)

	cases := []struct {
		label  string
		always bool
		actor  string
	}{
		{label: "model", actor: ActorModel},
		{label: "always", always: true, actor: ActorModel},
		{label: "operator", actor: "cli"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			name := c.label + uuid.Must(uuid.NewV7()).String()[:8]
			status, err := w.WriteMutation(ctx, scoring.SkillCreate,
				Frontmatter{Name: name, Description: "d", Type: TypeInstruction, Always: c.always}, "# body",
				AuditActor{ActorID: c.actor})
			if err != nil {
				t.Fatalf("WriteMutation: %v", err)
			}
			if status != StatusActive {
				t.Fatalf("status = %q, want %q", status, StatusActive)
			}
			for _, dir := range []string{"active", "export"} {
				if _, statErr := os.Stat(filepath.Join(root, dir, name, "SKILL.md")); statErr != nil {
					t.Fatalf("skill must land in %s/: %v", dir, statErr)
				}
			}
			rows, lerr := store.List(ctx, AuditFilter{SkillName: name})
			if lerr != nil {
				t.Fatalf("List: %v", lerr)
			}
			if len(rows) != 1 {
				t.Fatalf("audit rows = %d, want exactly 1", len(rows))
			}
			if rows[0].ActorID != c.actor {
				t.Errorf("audit actor_id = %q, want %q — the ledger is the only record of who acted", rows[0].ActorID, c.actor)
			}
		})
	}
}

// newMutationWriter builds a fully-configured Writer (all dirs under t.TempDir) over a
// migrated pool, returning the writer + its root so a test can inspect on-disk state.
func newMutationWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         migratedPool(t),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	return w, root
}

// seedActiveSkill writes active/<name>/SKILL.md directly (an instruction skill with the
// given always flag), so a test can exercise Delete/SetAlways against a real active dir
// without going through the pending->activate dance.
func seedActiveSkill(t *testing.T, root, name string, always bool) {
	t.Helper()
	dir := filepath.Join(root, "active", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("seed active dir: %v", err)
	}
	fm := Frontmatter{Name: name, Description: "d", Type: TypeInstruction, Always: always}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), skillFileBytes(fm, "body"), 0o600); err != nil {
		t.Fatalf("seed active SKILL.md: %v", err)
	}
}

// TestDeleteRemovesActiveAndExport proves Delete removes BOTH halves of a live skill:
// the active dir the loader scans and the export subtree the sandbox mounts read-only. A
// skipped RemoveAll leaves the skill loadable; a skipped Dematerialize leaves it runnable
// inside the box after the ledger says it is gone. Kills writer_activate.go's active-dir
// removal and its Dematerialize call.
func TestDeleteRemovesActiveAndExport(t *testing.T) {
	w, root := newMutationWriter(t)
	name := "del" + uuid.Must(uuid.NewV7()).String()[:8]

	seedActiveSkill(t, root, name, false)
	if err := Materialize(name, filepath.Join(root, "active", name), filepath.Join(root, "export")); err != nil {
		t.Fatalf("seed export copy: %v", err)
	}

	if _, err := w.Delete(context.Background(), name, AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	for _, dir := range []string{"active", "export"} {
		if _, statErr := os.Stat(filepath.Join(root, dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("Delete must remove %s/%s (stat err=%v)", dir, name, statErr)
		}
	}
}

// TestSetAlwaysPersistsFlag proves SetAlways actually flips the always flag on the active
// SKILL.md (reading it back shows always=true). Kills writer_activate.go.32: the
// `fm.Always = always` assignment skipped, which rewrites the SAME frontmatter and
// silently no-ops the operator's command.
func TestSetAlwaysPersistsFlag(t *testing.T) {
	w, root := newMutationWriter(t)
	name := "sa" + uuid.Must(uuid.NewV7()).String()[:8]

	seedActiveSkill(t, root, name, false) // always starts false
	if err := w.SetAlways(context.Background(), name, true, AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("SetAlways: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "active", name, "SKILL.md"))
	if err != nil {
		t.Fatalf("read active SKILL.md after SetAlways: %v", err)
	}
	fm, _, perr := parseFrontmatter(raw)
	if perr != nil {
		t.Fatalf("parse rewritten SKILL.md: %v", perr)
	}
	if !fm.Always {
		t.Fatalf("SetAlways(true) must persist always=true to the active SKILL.md, got always=%v\n%s", fm.Always, raw)
	}
	if !strings.Contains(string(raw), "always: true") {
		t.Errorf("rewritten SKILL.md should carry the always: true line:\n%s", raw)
	}
}

// TestLifecycleAuditFailureSurfaces proves every lifecycle audit wrap surfaces a db
// failure as a prefixed "<action> ...: audit:" error rather than swallowing it into a nil
// return. The seam is a CLOSED pool: the FS work (promote/materialize/remove/write) all
// run before the audit tx, then db.WithTx fails at Begin on the closed pool, so the audit
// wrap is the ONLY thing standing between the failure and a falsely-successful return.
// Kills writer_activate.go.3 (activate audit), .9 (archive audit), .15 (restore audit),
// .20 (delete audit), .26 (set-always audit).
func TestLifecycleAuditFailureSurfaces(t *testing.T) {
	t.Run("delete", func(t *testing.T) {
		w, root := newMutationWriter(t)
		name := "delaud" + uuid.Must(uuid.NewV7()).String()[:8]
		seedActiveSkill(t, root, name, false)
		w.pool.Close() // make the audit tx fail at Begin
		_, err := w.Delete(context.Background(), name, AuditActor{ActorID: "cli"})
		assertAuditWrap(t, err, "delete")
	})

	t.Run("set always", func(t *testing.T) {
		w, root := newMutationWriter(t)
		name := "saaud" + uuid.Must(uuid.NewV7()).String()[:8]
		seedActiveSkill(t, root, name, false)
		w.pool.Close()
		err := w.SetAlways(context.Background(), name, true, AuditActor{ActorID: "cli"})
		assertAuditWrap(t, err, "set always")
	})

	t.Run("write mutation", func(t *testing.T) {
		w, _ := newMutationWriter(t)
		name := "wraud" + uuid.Must(uuid.NewV7()).String()[:8]
		w.pool.Close()
		_, err := w.WriteMutation(context.Background(), scoring.SkillCreate,
			Frontmatter{Name: name, Description: "d", Type: TypeInstruction}, "body",
			AuditActor{ActorID: "cli"})
		assertAuditWrap(t, err, "write mutation")
	})

	t.Run("archive", func(t *testing.T) {
		w, root := newMutationWriter(t)
		name := "arcaud" + uuid.Must(uuid.NewV7()).String()[:8]
		seedActiveSkill(t, root, name, false)
		w.pool.Close()
		err := w.Archive(context.Background(), name, ApprovalCLI, AuditActor{ActorID: "cli"})
		assertAuditWrap(t, err, "archive")
	})

	t.Run("restore", func(t *testing.T) {
		w, root := newMutationWriter(t)
		name := "rstaud" + uuid.Must(uuid.NewV7()).String()[:8]
		// Seed an archived skill so promote+materialize+usage-status succeed pre-audit.
		archDir := filepath.Join(root, "archived", name)
		if err := os.MkdirAll(archDir, 0o750); err != nil {
			t.Fatalf("seed archived: %v", err)
		}
		fm := Frontmatter{Name: name, Description: "d", Type: TypeInstruction}
		if err := os.WriteFile(filepath.Join(archDir, "SKILL.md"), skillFileBytes(fm, "body"), 0o600); err != nil {
			t.Fatalf("seed archived SKILL.md: %v", err)
		}
		w.pool.Close()
		err := w.Restore(context.Background(), name, ApprovalCLI, AuditActor{ActorID: "cli"})
		assertAuditWrap(t, err, "restore")
	})
}

// assertAuditWrap asserts err is non-nil and carries the "<action>" prefix + the "audit:"
// segment — the operator diagnostic that says WHICH lifecycle step's audit failed.
func assertAuditWrap(t *testing.T, err error, action string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s with a failing audit tx: want an error, got nil (the audit wrap was swallowed)", action)
	}
	if !strings.Contains(err.Error(), action) {
		t.Errorf("%s audit error = %q, want it prefixed with %q", action, err.Error(), action)
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Errorf("%s audit error = %q, want it to name the audit step", action, err.Error())
	}
}

// TestRestoreArchiveDirUnsetMessage tightens the unset-archive-dir guard: Restore on a
// Writer with no archiveDir returns the SPECIFIC "archive dir not configured" diagnostic,
// not whatever downstream error a fall-through would hit. Kills writer_activate.go.11: the
// guard's early-return no-op'd, which would otherwise fall through to a join-on-"" path.
func TestRestoreArchiveDirUnsetMessage(t *testing.T) {
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:      migratedPool(t),
		ActiveDir: filepath.Join(root, "active"),
		ExportDir: filepath.Join(root, "export"),
		// ArchiveDir deliberately unset.
		BodyCapBytes: 32768,
	})

	err := w.Restore(context.Background(), "anything", ApprovalCLI, AuditActor{ActorID: "cli"})
	if err == nil {
		t.Fatal("Restore with no archiveDir: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "archive dir not configured") {
		t.Errorf("Restore unset-archive error = %q, want the 'archive dir not configured' diagnostic", err.Error())
	}
}

// TestRestoreRejectsBadNameMessage proves Restore's SanitizeName guard wraps the
// ErrInvalidName sentinel (the D-30 chokepoint signal) when a traversal name is rejected,
// AND carries the "restore" prefix. Asserting errors.Is(ErrInvalidName) is what kills the
// mutant: with the SanitizeName-error wrap no-op'd, the traversal name falls through to a
// downstream promoteDir failure — a plain path error that does NOT wrap ErrInvalidName —
// so the chokepoint signal disappears even though a later "restore" prefix remains.
func TestRestoreRejectsBadNameMessage(t *testing.T) {
	w, _ := newMutationWriter(t)
	err := w.Restore(context.Background(), "../escape", ApprovalCLI, AuditActor{ActorID: "cli"})
	if err == nil {
		t.Fatal("Restore with a traversal name: want an error, got nil")
	}
	if !errors.Is(err, ErrInvalidName) {
		t.Errorf("Restore bad-name error = %v, want it to wrap ErrInvalidName (the D-30 chokepoint), not a downstream path error", err)
	}
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("Restore bad-name error = %q, want it prefixed with 'restore'", err.Error())
	}
}
