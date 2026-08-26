package skills

import (
	"os"
	"path/filepath"
	"strings"
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

func TestMaterializeFindSkillsAuraAlwaysOn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatalf("MaterializeBuiltins: %v", err)
	}

	// The find-skills-aura builtin (amendment #51 / D-40) must materialize alongside
	// skill-creator.
	if _, err := os.Stat(filepath.Join(dir, "find-skills-aura", "SKILL.md")); err != nil {
		t.Fatalf("find-skills-aura/SKILL.md not materialized: %v", err)
	}

	// It must load from the same root AND be always:true (it rides messages[1]).
	l := NewLoader(Config{Roots: []string{dir}})
	got, ok := l.Get("find-skills-aura")
	if !ok {
		t.Fatalf("find-skills-aura not found by the loader after materialization; got %v", names(l.List()))
	}
	if !got.Always {
		t.Fatalf("find-skills-aura must be always:true (it teaches self-extension in the always-block)")
	}
	if got.Description == "" {
		t.Fatalf("find-skills-aura description is empty")
	}
}

func TestFindSkillsAuraExplainsAdministratorInstallBoundary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatalf("MaterializeBuiltins: %v", err)
	}
	got, ok := NewLoader(Config{Roots: []string{dir}}).Get("find-skills-aura")
	if !ok {
		t.Fatal("find-skills-aura not found")
	}
	for _, want := range []string{"skill_manage action=install", "governance.write", "administrator"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("find-skills-aura body does not contain %q", want)
		}
	}
	if strings.Contains(got.Body, "skill action=install") {
		t.Error("find-skills-aura still teaches the removed read-tool install syntax")
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
