package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveFile(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("notes/scratch.md", []byte("draft")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	if err := root.Remove("notes/scratch.md", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.Path(), "notes", "scratch.md")); !os.IsNotExist(err) {
		t.Fatalf("file still exists: err=%v", err)
	}
}

func TestRemoveRejectsDirectoryUnlessAllowed(t *testing.T) {
	root := newTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root.Path(), "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := root.Remove("notes", false); err == nil {
		t.Fatal("Remove(dir, allowDir=false) returned nil error")
	}
	if err := root.Remove("notes", true); err != nil {
		t.Fatalf("Remove(empty dir, allowDir=true): %v", err)
	}
}

func TestRemoveRejectsNonEmptyDirectory(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("notes/a.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := root.Remove("notes", true); err == nil {
		t.Fatal("Remove(non-empty dir) returned nil error")
	}
}

func TestRemoveRejectsRootAndSensitive(t *testing.T) {
	root := newTestRoot(t)
	if err := root.Remove(".", true); err == nil {
		t.Fatal("Remove(\".\") returned nil error")
	}
	writeRaw(t, root.Path(), ".env", []byte("secret"))
	if err := root.Remove(".env", false); !errors.Is(err, ErrDeniedPath) {
		t.Fatalf("Remove(.env) error = %v, want ErrDeniedPath", err)
	}
}

func TestMoveCreatesParentAndPreservesContent(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("a.md", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := root.Move("a.md", "deep/sub/b.md"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	data, err := root.Read("deep/sub/b.md", 1024)
	if err != nil {
		t.Fatalf("Read after move: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("post-move content = %q", data)
	}
}

func TestMoveRejectsBinaryDst(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("a.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := root.Move("a.md", "a.exe"); !errors.Is(err, ErrDeniedPath) {
		t.Fatalf("Move to .exe error = %v, want ErrDeniedPath", err)
	}
}

func TestMoveRejectsSensitiveDst(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("a.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := root.Move("a.md", ".env"); !errors.Is(err, ErrDeniedPath) {
		t.Fatalf("Move to .env error = %v, want ErrDeniedPath", err)
	}
}

func TestCopyDuplicates(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("a.md", []byte("body")); err != nil {
		t.Fatal(err)
	}
	if err := root.Copy("a.md", "b.md"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	for _, path := range []string{"a.md", "b.md"} {
		data, err := root.Read(path, 1024)
		if err != nil {
			t.Fatalf("Read %s: %v", path, err)
		}
		if string(data) != "body" {
			t.Fatalf("%s content = %q", path, data)
		}
	}
}

func TestCopyRejectsDirectories(t *testing.T) {
	root := newTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root.Path(), "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := root.Copy("src", "dst"); err == nil {
		t.Fatal("Copy(dir) returned nil error")
	}
}

func TestTreeStructureAndSensitiveFilter(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), "a.md", []byte("a"))
	writeRaw(t, root.Path(), "sub/b.md", []byte("b"))
	writeRaw(t, root.Path(), ".env", []byte("secret"))
	tree, err := root.Tree(".", 0, 0)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tree.Type != "dir" || len(tree.Children) == 0 {
		t.Fatalf("Tree root malformed: %+v", tree)
	}
	for _, child := range tree.Children {
		if strings.Contains(child.Path, ".env") {
			t.Fatalf("Tree leaked sensitive path: %+v", child)
		}
	}
}

func TestTreeRespectsDepthCap(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), "a/b/c/d.md", []byte("x"))
	tree, err := root.Tree(".", 2, 100)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	// At depth 2 we should see a/, a/b, but a/b's children must be truncated
	// (depthRemaining reaches 0 inside the b directory).
	var walk func(node TreeNode) bool
	hitTruncation := false
	walk = func(node TreeNode) bool {
		if node.Truncated {
			hitTruncation = true
			return true
		}
		for _, c := range node.Children {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(tree)
	if !hitTruncation {
		t.Fatalf("Tree did not mark truncation at depth=2: %+v", tree)
	}
}

func TestInfoReportsExistingAndMissing(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("notes/a.md", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got, err := root.Info("notes/a.md")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !got.Exists || got.Type != "file" || got.Size != 2 {
		t.Fatalf("Info file = %+v", got)
	}
	missing, err := root.Info("notes/ghost.md")
	if err != nil {
		t.Fatalf("Info missing: %v", err)
	}
	if missing.Exists || missing.Type != "missing" {
		t.Fatalf("Info missing = %+v", missing)
	}
}

func TestFindMatchesBasenameCaseInsensitive(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), "AGENTS.md", []byte("a"))
	writeRaw(t, root.Path(), "nested/dir/agents.md", []byte("b"))
	writeRaw(t, root.Path(), "other.md", []byte("c"))
	matches, err := root.Find("agents.md", 10)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("Find matches = %+v, want 2", matches)
	}
	for _, m := range matches {
		base := filepath.Base(m.Path)
		if !strings.EqualFold(base, "agents.md") {
			t.Fatalf("Find returned non-match %+v", m)
		}
	}
}

func TestFindSkipsSensitiveTree(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), "wiki/raw/secret.md", []byte("x"))
	writeRaw(t, root.Path(), "wiki/page.md", []byte("y"))
	matches, err := root.Find("secret.md", 10)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("Find leaked sensitive path: %+v", matches)
	}
}
