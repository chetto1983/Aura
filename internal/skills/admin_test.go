package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
		"NPM_CONFIG_CACHE=/data/.npm",
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
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/me", "NPM_CONFIG_CACHE=/data/.npm", "NPM_CONFIG_PREFIX=/opt/npm"} {
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

func TestFSProposalApplier_CreateOrUpdateWritesSkillMD(t *testing.T) {
	dir := t.TempDir()
	applier, err := NewFSProposalApplier(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := "---\nname: morning-brief\ndescription: Morning brief\n---\n\n# Body\n"

	if err := applier.ApplySkillProposal(context.Background(), LocalProposal{
		Action:  "skill_create",
		Name:    "morning-brief",
		Content: content,
	}); err != nil {
		t.Fatalf("ApplySkillProposal: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "morning-brief", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("SKILL.md = %q", got)
	}
}

func TestFSProposalApplier_RejectsNameMismatch(t *testing.T) {
	applier, err := NewFSProposalApplier(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = applier.ApplySkillProposal(context.Background(), LocalProposal{
		Action:  "skill_create",
		Name:    "morning-brief",
		Content: "---\nname: other\ndescription: Other\n---\n\n# Body\n",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestFSProposalApplier_DeleteRemovesSkill(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "old-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old-skill", "SKILL.md"), []byte("---\nname: old-skill\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	applier, err := NewFSProposalApplier(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := applier.ApplySkillProposal(context.Background(), LocalProposal{Action: "skill_delete", Name: "old-skill"}); err != nil {
		t.Fatalf("ApplySkillProposal delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old-skill")); !os.IsNotExist(err) {
		t.Fatalf("expected deleted skill, stat err = %v", err)
	}
}
