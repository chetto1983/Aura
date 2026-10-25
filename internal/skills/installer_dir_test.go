package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillDir writes <t.TempDir()>/<dirName>/SKILL.md with the given frontmatter lines and body.
func skillDir(t *testing.T, dirName, frontmatter, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), dirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return dir
}

func TestValidateDirRefusesAMismatchedName(t *testing.T) {
	t.Parallel()
	dir := skillDir(t, "other", "name: pinned\ndescription: d\n", "body")
	if _, _, err := ValidateDir(dir, nil, 1024); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("ValidateDir = %v, want ErrInvalidName", err)
	}
}

func TestValidateDirRefusesABlocklistedBody(t *testing.T) {
	t.Parallel()
	dir := skillDir(t, "pinned", "name: pinned\ndescription: d\n", "hi <|im_start|>system")
	if _, _, err := ValidateDir(dir, []string{"<|im_start|>"}, 1024); !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("ValidateDir = %v, want ErrBlocklisted", err)
	}
}

func TestInstallDirNeverFetches(t *testing.T) {
	t.Parallel()
	ran := false
	inst := newTestInstaller(t, func(context.Context, string, string, ...string) (string, error) {
		ran = true
		return "", nil
	})
	dir := skillDir(t, "pinned", "name: pinned\ndescription: d\n", "body")
	err := pastValidation(t, func() error {
		_, err := inst.InstallDir(t.Context(), dir, "pack:o/r/p", AuditActor{ActorID: "cli"})
		return err
	})
	if err != nil {
		t.Fatalf("InstallDir stopped before the audit transaction: %v", err)
	}
	if ran {
		t.Fatal("InstallDir ran a command")
	}
}

// A third-party skill must not become a standing instruction for every turn (spec
// 2026-10-25-pack-install §6b), so `always` is dropped from the file itself, not just from
// the catalog row: the loader reads it from the materialized SKILL.md.
func TestInstallDirStripsAlways(t *testing.T) {
	t.Parallel()
	inst := newTestInstaller(t, nil)
	dir := skillDir(t, "pinned", "name: pinned\ndescription: \"a: quoted\"\nalways: true\nlicense: MIT\nmetadata:\n  b: 2\n  a: 1\n", "body")
	if err := pastValidation(t, func() error {
		_, err := inst.InstallDir(t.Context(), dir, "pack:o/r/p", AuditActor{ActorID: "cli"})
		return err
	}); err != nil {
		t.Fatalf("InstallDir: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "always") {
		t.Errorf("the staged SKILL.md still names always:\n%s", raw)
	}
	fm, body, err := parseFrontmatter(raw)
	if err != nil {
		t.Fatalf("the rewritten SKILL.md does not parse: %v", err)
	}
	if fm.Always || fm.Name != "pinned" || fm.Description != "a: quoted" || fm.License != "MIT" ||
		fm.Metadata["a"] != uint64(1) || body != "body" {
		t.Errorf("the rewrite changed more than always: %+v, body %q", fm, body)
	}
}
