package tools

// Tests for the surgical skills-library fence (#54 / D-43) and the "~" expansion
// (#3) that aligns the fs_* tools with shell_exec.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFSWrite_FencesSkillsDir(t *testing.T) {
	root := t.TempDir()
	skills := filepath.Join(root, "skills")
	target := filepath.Join(skills, "export", ".agents", "skills", "xlsx", "SKILL.md")
	ctx := ctxWith(t, "s", "c")
	w := &FSWrite{SkillsDir: skills}

	_, err := w.Execute(ctx, mustJSON(t, fsWriteArgs{Path: target, Content: "malicious skill"}))
	if err == nil {
		t.Fatal("fs_write inside the skills dir must be denied")
	}
	if !strings.Contains(err.Error(), "skills library") || !strings.Contains(err.Error(), "skill") {
		t.Errorf("err = %v, want a skills-library redirect to the gated skill tool", err)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Error("file was written despite the fence")
	}
}

func TestFSWrite_AllowsOutsideSkillsDir(t *testing.T) {
	root := t.TempDir()
	skills := filepath.Join(root, "skills")
	target := filepath.Join(root, "work", "out.txt")
	ctx := ctxWith(t, "s", "c")
	w := &FSWrite{SkillsDir: skills}

	if _, err := w.Execute(ctx, mustJSON(t, fsWriteArgs{Path: target, Content: "ok"})); err != nil {
		t.Fatalf("write outside the skills dir must succeed: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("file not written: %v", err)
	}
}

func TestFSWrite_EmptySkillsDir_NoFence(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "anything", "f.txt")
	ctx := ctxWith(t, "s", "c")
	w := &FSWrite{} // no SkillsDir → fence disabled (unit/pool-free paths)

	if _, err := w.Execute(ctx, mustJSON(t, fsWriteArgs{Path: target, Content: "ok"})); err != nil {
		t.Fatalf("empty SkillsDir must disable the fence: %v", err)
	}
}

func TestFSEdit_FencesSkillsDir(t *testing.T) {
	root := t.TempDir()
	skills := filepath.Join(root, "skills")
	target := filepath.Join(skills, "a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "s", "c")
	e := &FSEdit{SkillsDir: skills}

	_, err := e.Execute(ctx, mustJSON(t, fsEditArgs{Path: target, OldString: "old", NewString: "new"}))
	if err == nil || !strings.Contains(err.Error(), "skills library") {
		t.Fatalf("fs_edit inside the skills dir must be denied, got %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Errorf("content changed despite the fence: %q", b)
	}
}

func TestWithinDir(t *testing.T) {
	root := t.TempDir()
	skills := filepath.Join(root, "skills")
	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"nested", filepath.Join(skills, "a", "b.md"), true},
		{"dir_itself", skills, true},
		{"sibling", filepath.Join(root, "other", "x"), false},
		{"parent", root, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := withinDir(skills, tc.target); got != tc.want {
				t.Errorf("withinDir(%q, %q) = %v, want %v", skills, tc.target, got, tc.want)
			}
		})
	}
	if withinDir("", filepath.Join(root, "x")) {
		t.Error("an empty dir must never fence")
	}
}

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir on this host")
	}
	if got := expandHomePath("~"); got != home {
		t.Errorf("expandHomePath(~) = %q, want %q", got, home)
	}
	if got := expandHomePath("~/sub/x"); got != filepath.Join(home, "sub", "x") {
		t.Errorf("expandHomePath(~/sub/x) = %q, want %q", got, filepath.Join(home, "sub", "x"))
	}
	if got := expandHomePath("/abs/path"); got != "/abs/path" {
		t.Errorf("a non-tilde path must pass through unchanged, got %q", got)
	}
	if got := expandHomePath("~user/x"); got != "~user/x" {
		t.Errorf("~user (named home) must NOT expand, got %q", got)
	}
}
