package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMaterializeCopiesActiveFiles proves a plain active skill dir is copied into
// exportDir/<name> verbatim (the /skills mount source, D-17).
func TestMaterializeCopiesActiveFiles(t *testing.T) {
	skillDir := t.TempDir()
	exportDir := t.TempDir()
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: x\n---\nbody")
	writeFile(t, filepath.Join(skillDir, "scripts", "run.py"), "print(1)")

	if err := Materialize("x", skillDir, exportDir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := readFile(t, filepath.Join(exportDir, "x", "SKILL.md")); got != "---\nname: x\n---\nbody" {
		t.Errorf("SKILL.md not copied: %q", got)
	}
	if got := readFile(t, filepath.Join(exportDir, "x", "scripts", "run.py")); got != "print(1)" {
		t.Errorf("nested script not copied: %q", got)
	}
}

// TestMaterializeStripsSymlink proves a symlink in the skill dir does NOT appear in
// the export dir (Pitfall 4 / T-11-04-T1) — neither the link nor its target.
func TestMaterializeStripsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Symlink needs elevation/developer-mode on Windows; the WSL/CI tier is the
		// authoritative run for this assertion.
		t.Skip("symlink creation requires privilege on Windows; asserted in the WSL/CI tier")
	}
	skillDir := t.TempDir()
	exportDir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, secret, "TOP SECRET")
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: x\n---\nbody")
	if err := os.Symlink(secret, filepath.Join(skillDir, "leak.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := Materialize("x", skillDir, exportDir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(exportDir, "x", "leak.txt")); !os.IsNotExist(err) {
		t.Errorf("symlink leak.txt survived materialization (err=%v) — symlink strip failed", err)
	}
	// The legitimate file is still there.
	if got := readFile(t, filepath.Join(exportDir, "x", "SKILL.md")); got == "" {
		t.Error("legitimate SKILL.md missing after symlink-stripped materialize")
	}
}

// TestDematerializeRemovesSubtree proves Dematerialize clears the export subtree and
// is a no-op on an absent one.
func TestDematerializeRemovesSubtree(t *testing.T) {
	exportDir := t.TempDir()
	writeFile(t, filepath.Join(exportDir, "x", "SKILL.md"), "data")

	if err := Dematerialize("x", exportDir); err != nil {
		t.Fatalf("Dematerialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exportDir, "x")); !os.IsNotExist(err) {
		t.Errorf("export subtree survived dematerialize (err=%v)", err)
	}
	// Idempotent: a second call on the now-absent subtree is a no-op.
	if err := Dematerialize("x", exportDir); err != nil {
		t.Errorf("Dematerialize on absent subtree: want nil, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(b)
}
