package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFSDeleter_RemovesDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpha", "SKILL.md"), []byte("---\nname: alpha\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := NewFSDeleter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("expected alpha removed, stat err = %v", err)
	}
}

func TestFSDeleter_NotFound(t *testing.T) {
	dir := t.TempDir()
	d, err := NewFSDeleter(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = d.Delete("missing")
	if !IsSkillNotFound(err) {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestFSDeleter_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	d, err := NewFSDeleter(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{"..", "../escape", "/abs/path"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := d.Delete(name); err == nil {
				t.Fatalf("expected error for name %q", name)
			}
		})
	}
}

func TestFSDeleter_RefusesSymlink(t *testing.T) {
	if testingSkipSymlink() {
		t.Skip("symlink not supported on this platform")
	}
	dir := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink create failed (likely Windows without privilege): %v", err)
	}
	d, err := NewFSDeleter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("linked"); err == nil {
		t.Fatal("expected refusal on symlink delete")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("symlink should still exist after refused delete: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target should be untouched: %v", err)
	}
}

// testingSkipSymlink lets us bail out cleanly on platforms / CI runners
// where unprivileged symlinks aren't allowed (Windows in particular).
func testingSkipSymlink() bool {
	dir, err := os.MkdirTemp("", "symtest")
	if err != nil {
		return true
	}
	defer os.RemoveAll(dir)
	if err := os.Symlink(dir, filepath.Join(dir, "x")); err != nil {
		return true
	}
	return false
}

func TestSanitizedEnv_KeepsPathAndProfileOnly(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HOME=/home/me",
		"TELEGRAM_TOKEN=secret",
		"MISTRAL_API_KEY=alsoSecret",
		"NPM_CONFIG_PREFIX=/opt/npm",
		"NOT=KEEPME",
	}
	out := sanitizedEnv(in)
	have := map[string]bool{}
	for _, kv := range out {
		have[kv] = true
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/me", "NPM_CONFIG_PREFIX=/opt/npm"} {
		if !have[want] {
			t.Errorf("missing %q", want)
		}
	}
	for _, leak := range []string{"TELEGRAM_TOKEN=secret", "MISTRAL_API_KEY=alsoSecret", "NOT=KEEPME"} {
		if have[leak] {
			t.Errorf("leaked %q", leak)
		}
	}
}
