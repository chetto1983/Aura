package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeBuiltinsAppearsInLoader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatalf("MaterializeBuiltins: %v", err)
	}

	// The embedded skill-creator must have been written to disk.
	if _, err := os.Stat(filepath.Join(dir, "skill-creator", "SKILL.md")); err != nil {
		t.Fatalf("skill-creator/SKILL.md not materialized: %v", err)
	}

	// And it must appear in the loader's scan of the same root.
	l := NewLoader(Config{Roots: []string{dir}})
	got, ok := l.Get("skill-creator")
	if !ok {
		t.Fatalf("skill-creator not found by the loader after materialization; got %v", names(l.List()))
	}
	if got.Description == "" {
		t.Fatalf("skill-creator description is empty")
	}
}

func TestMaterializeBuiltinsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	target := filepath.Join(dir, "skill-creator", "SKILL.md")
	info1, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	// A second materialize must NOT rewrite an unchanged file (fingerprint-idempotent).
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	info2, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("idempotent materialize rewrote an unchanged file (mtime changed)")
	}
}
