package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// symlinkOrSkip makes a symlink, skipping where the host cannot (Windows without the
// privilege) and failing under CI, where the tier must run.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("symlink: %v", err)
		}
		t.Skipf("this host cannot make symlinks: %v", err)
	}
}

func pluginTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "plugin.json"), "{}", 0o644)
	writeFile(t, filepath.Join(src, "bin", "run.sh"), "#!/bin/sh\n", 0o755)
	return src
}

func stagingDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	var staging []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			staging = append(staging, e.Name())
		}
	}
	return staging
}

func TestStoreClaimIsAtomicAndRefusesAnInstalledName(t *testing.T) {
	t.Parallel()
	s := Store{Root: t.TempDir()}
	src := pluginTree(t)
	if err := s.Claim(Record{Name: "p", Ref: "o/r/p"}, src); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := s.Claim(Record{Name: "p", Ref: "o/other/p"}, src); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second claim = %v, want ErrAlreadyInstalled", err)
	}
	if left := stagingDirs(t, s.Root); len(left) != 0 {
		t.Errorf("staging left behind: %v", left)
	}
	rec, ok, err := s.Get("p")
	if err != nil || !ok || rec.Ref != "o/r/p" || rec.Phase != PhaseInstalling {
		t.Fatalf("Get = (%+v, %v, %v), want the first claim's record, installing", rec, ok, err)
	}
	if _, err := os.Stat(filepath.Join(s.TreeDir("p"), "plugin.json")); err != nil {
		t.Errorf("the tree did not land: %v", err)
	}
}

func TestStoreSwapKeepsABackupUntilRestoreOrDrop(t *testing.T) {
	t.Parallel()
	s := Store{Root: t.TempDir()}
	if err := s.Claim(Record{Name: "p", Ref: "o/r/p"}, pluginTree(t)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	next := t.TempDir()
	writeFile(t, filepath.Join(next, "plugin.json"), `{"v":2}`, 0o644)
	if err := s.Swap("p", next); err != nil {
		t.Fatalf("swap: %v", err)
	}
	read := func() string {
		raw, err := os.ReadFile(filepath.Join(s.TreeDir("p"), "plugin.json"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(raw)
	}
	if got := read(); got != `{"v":2}` {
		t.Fatalf("after the swap the tree holds %q", got)
	}
	if left := stagingDirs(t, filepath.Join(s.Root, "p")); len(left) != 0 {
		t.Errorf("staging left behind: %v", left)
	}
	if err := s.Restore("p"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := read(); got != "{}" {
		t.Fatalf("after the restore the tree holds %q", got)
	}
	if err := s.Restore("p"); err != nil {
		t.Errorf("a restore with no backup = %v, want nothing to undo", err)
	}
	if err := s.Swap("p", next); err != nil {
		t.Fatalf("second swap: %v", err)
	}
	if err := s.DropBackup("p"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "p", backupDir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the backup survived the drop: %v", err)
	}
	if got := read(); got != `{"v":2}` {
		t.Errorf("the drop touched the tree: %q", got)
	}
}

func TestStoreClaimKeepsModesAndInternalSymlinks(t *testing.T) {
	t.Parallel()
	s := Store{Root: t.TempDir()}
	src := pluginTree(t)
	symlinkOrSkip(t, "plugin.json", filepath.Join(src, "manifest.json"))
	if err := s.Claim(Record{Name: "p"}, src); err != nil {
		t.Fatalf("claim: %v", err)
	}
	tree := s.TreeDir("p")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(tree, "bin", "run.sh"))
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("run.sh = (%v, %v), want mode 0755", info, err)
		}
	}
	info, err := os.Lstat(filepath.Join(tree, "manifest.json"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("manifest.json = (%v, %v), want a regular file", info, err)
	}
	if got, _ := os.ReadFile(filepath.Join(tree, "manifest.json")); string(got) != "{}" {
		t.Errorf("manifest.json = %q, want the link target's content", got)
	}
}

func TestStoreClaimRefusesAnEscapingOrDirectorySymlink(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret")
	writeFile(t, outside, "x", 0o600)
	for name, link := range map[string]func(src string) (target, path string){
		"escaping file": func(string) (string, string) { return outside, "secret" },
		"internal dir":  func(src string) (string, string) { return filepath.Join(src, "bin"), "bin2" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := Store{Root: t.TempDir()}
			src := pluginTree(t)
			target, path := link(src)
			symlinkOrSkip(t, target, filepath.Join(src, path))
			if err := s.Claim(Record{Name: "p"}, src); err == nil {
				t.Fatal("claim accepted the tree")
			}
			if _, ok, _ := s.Get("p"); ok {
				t.Error("a refused claim left a record")
			}
			if left := stagingDirs(t, s.Root); len(left) != 0 {
				t.Errorf("staging left behind: %v", left)
			}
		})
	}
}

func TestStoreListSkipsStagingAndRecordlessDirs(t *testing.T) {
	t.Parallel()
	s := Store{Root: t.TempDir()}
	if err := s.Claim(Record{Name: "b"}, pluginTree(t)); err != nil {
		t.Fatalf("claim b: %v", err)
	}
	if err := s.Claim(Record{Name: "a"}, pluginTree(t)); err != nil {
		t.Fatalf("claim a: %v", err)
	}
	writeFile(t, filepath.Join(s.Root, ".staging-123", recordFile), `{"Name":"ghost"}`, 0o600)
	if err := os.MkdirAll(filepath.Join(s.Root, "norecord", "tree"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	recs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, 0, len(recs))
	for _, r := range recs {
		names = append(names, r.Name)
	}
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Errorf("List = %v, want [a b]", names)
	}
	rec, _, _ := s.Get("a")
	rec.Phase = PhaseInstalled
	if err := s.WriteRecord(rec); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	if got, _, _ := s.Get("a"); got.Phase != PhaseInstalled {
		t.Errorf("phase after WriteRecord = %q", got.Phase)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Error("a deleted plugin is still recorded")
	}
}

func TestStoreRefusesAnInvalidName(t *testing.T) {
	t.Parallel()
	s := Store{Root: t.TempDir()}
	for _, name := range []string{"a/b", "..", "", "A"} {
		if _, _, err := s.Get(name); err == nil {
			t.Errorf("Get(%q) accepted", name)
		}
		if err := s.Delete(name); err == nil {
			t.Errorf("Delete(%q) accepted", name)
		}
		if err := s.Claim(Record{Name: name}, pluginTree(t)); err == nil {
			t.Errorf("Claim(%q) accepted", name)
		}
	}
}

func TestStoreRecordErrors(t *testing.T) {
	t.Parallel()
	s := Store{Root: t.TempDir()}
	if err := s.WriteRecord(Record{Name: "absent"}); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("WriteRecord of an absent plugin = %v, want ErrNotInstalled", err)
	}
	if recs, err := (Store{Root: filepath.Join(s.Root, "missing")}).List(); err != nil || len(recs) != 0 {
		t.Errorf("List of a missing root = (%v, %v), want empty", recs, err)
	}
	writeFile(t, filepath.Join(s.Root, "broken", recordFile), "{", 0o600)
	if _, _, err := s.Get("broken"); err == nil {
		t.Error("a corrupt record was read")
	}
	if _, err := s.List(); err == nil {
		t.Error("List hid a corrupt record")
	}
	if err := s.Claim(Record{Name: "p"}, filepath.Join(s.Root, "no-such-src")); err == nil {
		t.Error("a claim of a missing tree succeeded")
	}
}
