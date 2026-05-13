package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveContainsPathsInsideRoot(t *testing.T) {
	root := newTestRoot(t)
	got, err := root.Resolve("notes/today.md")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(root.Path(), "notes", "today.md")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveRejectsOutsideAndSensitivePaths(t *testing.T) {
	root := newTestRoot(t)
	for _, rel := range []string{
		"../outside.txt",
		"/etc/passwd",
		"D:/tmp/outside.txt",
		".env",
		"data/aura.db",
		"data/aura.db-wal",
		"data/secrets/token",
		"docker/secrets/init-secrets.sh",
		".git/config",
		"wiki/raw/source.bin",
	} {
		t.Run(rel, func(t *testing.T) {
			_, err := root.Resolve(rel)
			if err == nil {
				t.Fatalf("Resolve(%q) returned nil error", rel)
			}
		})
	}
}

func TestReadAndWriteAtomic(t *testing.T) {
	root := newTestRoot(t)
	if err := root.WriteAtomic("notes/a.md", []byte("hello")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := root.Read("notes/a.md", 1024)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("Read = %q", got)
	}
}

func TestWriteAtomicRejectsBinaryExtensions(t *testing.T) {
	root := newTestRoot(t)
	err := root.WriteAtomic("bin/tool.exe", []byte("MZ"))
	if !errors.Is(err, ErrDeniedPath) {
		t.Fatalf("WriteAtomic binary error = %v, want ErrDeniedPath", err)
	}
}

func TestReadAllowsSmallBinaryButRejectsLargeBinary(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), "assets/icon.png", []byte{0, 1, 2})
	if _, err := root.Read("assets/icon.png", 1024); err != nil {
		t.Fatalf("Read small binary: %v", err)
	}
	writeRaw(t, root.Path(), "assets/big.png", make([]byte, smallBinaryReadLimit+1))
	err := mustErr(root.Read("assets/big.png", smallBinaryReadLimit+1))
	if !errors.Is(err, ErrDeniedPath) {
		t.Fatalf("Read large binary error = %v, want ErrDeniedPath", err)
	}
}

func TestSearchSkipsDeniedAndBinaryPaths(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), "notes/a.md", []byte("alpha\nneedle here\n"))
	writeRaw(t, root.Path(), "wiki/raw/raw.md", []byte("needle hidden"))
	writeRaw(t, root.Path(), "assets/icon.png", []byte("needle hidden"))
	matches, err := root.Search("needle", []string{"**/*.md"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("Search matches = %+v, want one", matches)
	}
	if matches[0].Path != "notes/a.md" || matches[0].Line != 2 || matches[0].Column != 1 {
		t.Fatalf("Search match = %+v", matches[0])
	}
}

func TestListHidesSensitivePaths(t *testing.T) {
	root := newTestRoot(t)
	writeRaw(t, root.Path(), ".env", []byte("secret"))
	writeRaw(t, root.Path(), "README.md", []byte("hello"))
	items, err := root.List(".", 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Path != "README.md" {
		t.Fatalf("List = %+v, want README only", items)
	}
}

func newTestRoot(t *testing.T) *Root {
	t.Helper()
	root, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// os.Root holds an open dir FD; on Windows TempDir cleanup fails
	// unless we Close first.
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func writeRaw(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustErr(_ []byte, err error) error {
	if err == nil {
		panic("expected error")
	}
	return err
}
