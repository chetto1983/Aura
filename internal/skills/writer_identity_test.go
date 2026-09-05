package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writer_identity_test.go pins WHOSE roots a write lands in (amendment #214). Everything here
// is filesystem-only: the catalog leg needs Postgres and lives in the db_integration tier.

// newLayoutWriter builds a deployment-global Writer that CAN scope, plus the three bases, so
// a test can assert both the unscoped behaviour and the derived one against real paths.
func newLayoutWriter(t *testing.T) (*Writer, Layout) {
	t.Helper()
	root := t.TempDir()
	layout := Layout{
		Global:     filepath.Join(root, "active"),
		Identities: filepath.Join(root, "identities"),
		Export:     filepath.Join(root, "export"),
	}
	w := NewWriter(WriterConfig{
		ActiveDir:    layout.Global,
		ExportDir:    layout.Export,
		ArchiveDir:   filepath.Join(layout.Global, StageArchived),
		Layout:       layout,
		BodyCapBytes: 32768,
	})
	return w, layout
}

const writerTestIdentity = "44444444-4444-4444-8444-444444444444"

// TestWriterForScopesEveryRoot is the core of the slice on the write side: the derived Writer
// writes into the identity's own active root, exports into the identity's own export, and
// archives beneath its own root — not one of them left pointing at the shared library.
func TestWriterForScopesEveryRoot(t *testing.T) {
	t.Parallel()
	w, layout := newLayoutWriter(t)

	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if scoped == w {
		t.Fatal("For returned the global writer for a real identity — every write would land in the shared root")
	}
	wantActive := filepath.Join(layout.Identities, writerTestIdentity)
	if scoped.ActiveDir() != wantActive {
		t.Errorf("active root = %q, want %q", scoped.ActiveDir(), wantActive)
	}
	// Under the identity's own root, never under the deployment export: MaterializeIn tars
	// the deployment export into every box and walks the tree, so a nested identity export
	// rides into everybody else's box.
	if want := filepath.Join(wantActive, ".export"); scoped.exportDir != want {
		t.Errorf("export root = %q, want %q", scoped.exportDir, want)
	}
	if rel, err := filepath.Rel(layout.Export, scoped.exportDir); err == nil && !strings.HasPrefix(rel, "..") {
		t.Errorf("export root %q lives under the deployment export %q", scoped.exportDir, layout.Export)
	}
	if want := filepath.Join(wantActive, StageArchived); scoped.archiveDir != want {
		t.Errorf("archive root = %q, want %q", scoped.archiveDir, want)
	}
	if scoped.Owner() != writerTestIdentity {
		t.Errorf("owner = %q, want %q", scoped.Owner(), writerTestIdentity)
	}
	// The global writer is untouched — For derives, it does not mutate.
	if w.ActiveDir() != layout.Global || w.Owner() != "" {
		t.Fatalf("For mutated the receiver: active=%q owner=%q", w.ActiveDir(), w.Owner())
	}
}

// TestWriterForIsANoOpWithoutAnIdentityOrABase pins the two ways a deployment stays exactly
// as it was before #214: nobody asked for a scope, or per-identity skills are switched off.
func TestWriterForIsANoOpWithoutAnIdentityOrABase(t *testing.T) {
	t.Parallel()
	w, _ := newLayoutWriter(t)

	for _, identity := range []string{"", "   "} {
		got, err := w.For(identity)
		if err != nil {
			t.Fatalf("For(%q): %v", identity, err)
		}
		if got != w {
			t.Errorf("For(%q) derived a writer — an unscoped caller must write the deployment library", identity)
		}
	}

	// Per-identity skills switched off: no Identities base, so there is no own root to
	// write into and the identity writes the deployment library, as it did before.
	off, _ := newLayoutWriter(t)
	off.layout.Identities = ""
	got, err := off.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For with no identity base: %v", err)
	}
	if got != off {
		t.Fatal("with no identity base configured, For must leave the writer alone")
	}
}

// TestWriterForRefusesAnIdentityThatCannotNameADirectory proves the traversal guard is on the
// derivation, not on each write: a crafted identity is refused once, here, instead of
// producing a Writer aimed outside the base.
func TestWriterForRefusesAnIdentityThatCannotNameADirectory(t *testing.T) {
	t.Parallel()
	w, _ := newLayoutWriter(t)
	for _, bad := range []string{"../escape", "a/b", ".."} {
		if got, err := w.For(bad); err == nil {
			t.Errorf("For(%q) = %v, want an error", bad, got.ActiveDir())
		} else if !strings.Contains(err.Error(), "skills writer for") {
			t.Errorf("For(%q) error %q does not name the failing operation", bad, err)
		}
	}
}

// TestScopedWriterReadsOnlyItsOwnRoot is the read-side consequence of the derivation, and it
// is the property the whole slice exists for: a skill sitting in one identity's root is
// invisible to the other's writer.
func TestScopedWriterReadsOnlyItsOwnRoot(t *testing.T) {
	t.Parallel()
	w, layout := newLayoutWriter(t)
	const other = "55555555-5555-4555-8555-555555555555"

	mine, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	theirs, err := w.For(other)
	if err != nil {
		t.Fatalf("For(other): %v", err)
	}

	skillDir := filepath.Join(layout.Identities, writerTestIdentity, "private-skill")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: private-skill\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	if !mine.ActiveExists("private-skill") {
		t.Fatal("the owner's writer cannot see the owner's own skill")
	}
	if theirs.ActiveExists("private-skill") {
		t.Fatal("another identity's writer sees a skill it does not own — this is the boundary the slice exists for")
	}
	if w.ActiveExists("private-skill") {
		t.Fatal("the deployment writer sees a personal skill — the identity root is inside the shared namespace again")
	}
}

// TestCatalogOwnerAcceptsOnlyARealIdentity pins why a label-shaped id gets directories but no
// catalog row: aura.skill_catalog keys on aura.identities.id, a uuid, and the RLS predicate
// casts it — a label would fail at write time instead of at derivation.
func TestCatalogOwnerAcceptsOnlyARealIdentity(t *testing.T) {
	t.Parallel()
	if got := catalogOwner(writerTestIdentity); got != writerTestIdentity {
		t.Errorf("catalogOwner(uuid) = %q, want the uuid back", got)
	}
	for _, label := range []string{"local", "cli", "", "operator-1"} {
		if got := catalogOwner(label); got != "" {
			t.Errorf("catalogOwner(%q) = %q, want empty — a label names no identity row", label, got)
		}
	}
}

// TestCatalogOpsAreNilWithoutAnOwner proves the global Writer runs exactly the statements it
// ran before this slice: no owner, no catalog operation to fold into the audit transaction.
func TestCatalogOpsAreNilWithoutAnOwner(t *testing.T) {
	t.Parallel()
	w, _ := newLayoutWriter(t)
	if w.catalogUpsertOp("skill", "d", false, "hash") != nil {
		t.Error("the deployment writer must record no catalog row")
	}
	if w.catalogDeleteOp("skill") != nil {
		t.Error("the deployment writer must delete no catalog row")
	}

	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if scoped.catalogUpsertOp("skill", "d", false, "hash") == nil {
		t.Error("a scoped writer must record the ownership of what it writes")
	}
	if scoped.catalogDeleteOp("skill") == nil {
		t.Error("a scoped writer must collect the ownership of what it deletes")
	}
}

// TestScopedWriteStagesInTheIdentityRoot proves the staging directory follows the derivation
// too. The write fails at the audit transaction (this writer has no pool) — which is the
// point: by then the staging has already happened, and it must have happened under the
// identity's root and nowhere near the shared one.
func TestScopedWriteStagesInTheIdentityRoot(t *testing.T) {
	t.Parallel()
	w, layout := newLayoutWriter(t)
	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	fill := writeFilesInto(map[string][]byte{"SKILL.md": []byte("body")})
	err = func() (err error) {
		// A nil pool panics inside db.WithTx; the staging under test has already run.
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("audit unavailable")
			}
		}()
		return scoped.writeActive(t.Context(), "staged-skill", fill, AuditInsert{}, nil)
	}()
	if err == nil {
		t.Fatal("a poolless write must fail at the ledger, not succeed silently")
	}
	if _, statErr := os.Stat(filepath.Join(layout.Identities, writerTestIdentity, stagingDirName)); statErr != nil {
		t.Fatalf("staging did not happen under the identity root: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(layout.Global, stagingDirName)); statErr == nil {
		t.Fatal("staging landed in the deployment root — the derived writer is not scoped")
	}
}

// TestDeleteRefusesASkillThisRootDoesNotHold is the lie the scoped Writer made reachable.
// os.RemoveAll succeeds on an absent path, so before this a delete aimed at the wrong root
// removed nothing, wrote a delete row into the append-only ledger, and answered "removed" —
// while the skill was still sitting in the other root and still in every agent's context.
// Since #214 that is not hypothetical: the cockpit lists the deployment library and its
// delete button addresses the actor's own root.
func TestDeleteRefusesASkillThisRootDoesNotHold(t *testing.T) {
	t.Parallel()
	w, layout := newLayoutWriter(t)

	// A real skill, in the DEPLOYMENT root — exactly what the cockpit board lists.
	skillDir := filepath.Join(layout.Global, "house-skill")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: house-skill\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	// No pool is needed: the refusal must come BEFORE the audit transaction, or the ledger
	// has already been told about a deletion that did not happen.
	if _, err := scoped.Delete(t.Context(), "house-skill", AuditActor{}); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("delete of a skill in another root = %v, want ErrUnknownSkill", err)
	}
	if _, statErr := os.Stat(skillDir); statErr != nil {
		t.Fatalf("the skill was removed from the root the caller does not address: %v", statErr)
	}
	if _, err := w.For(""); err != nil {
		t.Fatalf("For(\"\"): %v", err)
	}
	// The deployment Writer, which DOES hold it, gets past the existence check and fails at
	// the ledger instead — so the guard is about the root, not about refusing every delete.
	err = func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("audit unavailable")
			}
		}()
		_, derr := w.Delete(t.Context(), "house-skill", AuditActor{})
		return derr
	}()
	if errors.Is(err, ErrUnknownSkill) {
		t.Fatal("the writer that owns the root refused its own skill")
	}
}
