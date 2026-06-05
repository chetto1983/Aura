package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCanonicalHashOrderIndependent proves the canonical hash is a pure function of
// the (relPath, content) set — independent of the order in which files were written
// or the FS walk yields them. Two dirs with the same files in different creation
// order hash identically.
func TestCanonicalHashOrderIndependent(t *testing.T) {
	files := map[string]string{
		"SKILL.md":     "---\nname: x\n---\nbody\n",
		"a/one.md":     "one",
		"b/two.md":     "two",
		"z/last.txt":   "last",
		"ref/notes.md": "notes",
	}

	dirA := writeTree(t, files, []string{"SKILL.md", "a/one.md", "b/two.md", "z/last.txt", "ref/notes.md"})
	dirB := writeTree(t, files, []string{"ref/notes.md", "z/last.txt", "b/two.md", "a/one.md", "SKILL.md"})

	hA, err := CanonicalHash(dirA)
	if err != nil {
		t.Fatalf("CanonicalHash(dirA): %v", err)
	}
	hB, err := CanonicalHash(dirB)
	if err != nil {
		t.Fatalf("CanonicalHash(dirB): %v", err)
	}
	if hA != hB {
		t.Errorf("hash depends on write order: %q vs %q", hA, hB)
	}
}

// TestCanonicalHashExcludesSymlinks proves a symlink is NOT hashed: two trees that
// differ ONLY by a symlink hash identically (the symlink is skipped, never followed).
func TestCanonicalHashExcludesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows; the WSL/CI run is authoritative")
	}
	base := map[string]string{"SKILL.md": "body", "data.txt": "x"}

	plain := writeTree(t, base, []string{"SKILL.md", "data.txt"})
	withLink := writeTree(t, base, []string{"SKILL.md", "data.txt"})
	if err := os.Symlink("/etc/hosts", filepath.Join(withLink, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	hPlain, err := CanonicalHash(plain)
	if err != nil {
		t.Fatalf("CanonicalHash(plain): %v", err)
	}
	hLink, err := CanonicalHash(withLink)
	if err != nil {
		t.Fatalf("CanonicalHash(withLink): %v", err)
	}
	if hPlain != hLink {
		t.Errorf("symlink changed the hash (not excluded): %q vs %q", hPlain, hLink)
	}
}

// TestCanonicalHashContentSensitive proves a content change DOES change the hash
// (the pin is real): otherwise-identical trees with one differing byte differ.
func TestCanonicalHashContentSensitive(t *testing.T) {
	a := writeTree(t, map[string]string{"SKILL.md": "alpha"}, []string{"SKILL.md"})
	b := writeTree(t, map[string]string{"SKILL.md": "alphb"}, []string{"SKILL.md"})
	hA, _ := CanonicalHash(a)
	hB, _ := CanonicalHash(b)
	if hA == hB {
		t.Errorf("content change did not change the hash: both %q", hA)
	}
}

// writeTree writes files into a fresh temp dir in the given creation order and
// returns the dir. Order controls the FS creation sequence so the order-independence
// test can vary it.
func writeTree(t *testing.T, files map[string]string, order []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range order {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(files[rel]), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}
