package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// writer_archived_exists_test.go pins ArchivedExists, the archive-side twin of ActiveExists.
//
// It exists because Archive and Restore are two halves of one button and, once a caller can
// have TWO writable roots (their own and — with governance.write — the house's), the verb has
// to be resolved against the root that actually HOLDS the name. ActiveExists answers that for
// Archive and Delete; without this, Restore has no way to ask the same question and a house
// skill an operator archived would be visible to nobody and restorable by nobody.

// archivedWriter builds a writer over a temp tree and returns it with its archive dir.
func archivedWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	base := t.TempDir()
	active := filepath.Join(base, "skills")
	archive := filepath.Join(active, StageArchived)
	w := NewWriter(WriterConfig{
		ActiveDir:  active,
		ExportDir:  filepath.Join(base, "export"),
		ArchiveDir: archive,
	})
	return w, archive
}

// seedArchived writes a minimal archived skill tree.
func seedArchived(t *testing.T, archive, name string) {
	t.Helper()
	dir := filepath.Join(archive, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	md := "---\nname: " + name + "\ndescription: d\ntype: instruction\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

// TestArchivedExistsSeesAnArchivedSkill is the positive case: the name is in this writer's
// archive, so Restore aimed here would find it.
func TestArchivedExistsSeesAnArchivedSkill(t *testing.T) {
	t.Parallel()
	w, archive := archivedWriter(t)
	seedArchived(t, archive, "parked")

	if !w.ArchivedExists("parked") {
		t.Fatal("an archived skill must be visible to the writer whose archive holds it")
	}
}

// TestArchivedExistsRefusesWhatIsNotThere covers the three ways a name is not in the archive:
// never archived, still active, and a name the grammar refuses. All three must answer false
// rather than reaching a path join with an unvalidated name.
func TestArchivedExistsRefusesWhatIsNotThere(t *testing.T) {
	t.Parallel()
	w, archive := archivedWriter(t)
	seedArchived(t, archive, "parked")

	for _, name := range []string{"never-archived", "../escape", ""} {
		if w.ArchivedExists(name) {
			t.Fatalf("ArchivedExists(%q) = true, want false", name)
		}
	}
}

// TestArchivedExistsIsFalseWithoutAnArchiveDir pins the unconfigured writer: Restore already
// refuses one, and this must not answer true for a path it would build from an empty root.
func TestArchivedExistsIsFalseWithoutAnArchiveDir(t *testing.T) {
	t.Parallel()
	w := NewWriter(WriterConfig{ActiveDir: t.TempDir()})

	if w.ArchivedExists("anything") {
		t.Fatal("a writer with no archive dir must hold nothing archived")
	}
}
