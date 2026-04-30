package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderLoadAll(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "beta", "beta", "Beta skill", "Beta body")
	writeSkill(t, dir, "alpha", "alpha", "Alpha skill", "Alpha body")
	writeFile(t, filepath.Join(dir, "broken", "SKILL.md"), "no frontmatter")

	loader := NewLoader(dir)
	got, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadAll() length = %d, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("LoadAll() order = %q, %q; want alpha, beta", got[0].Name, got[1].Name)
	}
	if got[0].Description != "Alpha skill" || got[0].Content != "Alpha body" {
		t.Fatalf("first skill = %#v", got[0])
	}
}

func TestLoaderLoadAllMissingDir(t *testing.T) {
	loader := NewLoader(filepath.Join(t.TempDir(), "missing"))
	got, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadAll() length = %d, want 0", len(got))
	}
}

func TestLoaderLoadByNameRejectsTraversal(t *testing.T) {
	loader := NewLoader(t.TempDir())
	if _, err := loader.LoadByName("../secret"); err == nil {
		t.Fatal("LoadByName() expected traversal error")
	}
	if _, err := loader.LoadByName("bad/name"); err == nil {
		t.Fatal("LoadByName() expected slash error")
	}
}

func TestLoaderMultiRootMerges(t *testing.T) {
	primary := t.TempDir()
	secondary := t.TempDir()
	writeSkill(t, primary, "alpha", "alpha", "Primary alpha", "alpha body")
	writeSkill(t, secondary, "bravo", "bravo", "Secondary bravo", "bravo body")

	loader := NewLoader(primary, secondary)
	got, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 skills across roots, got %d", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "bravo" {
		t.Fatalf("ordering: got %q,%q want alpha,bravo", got[0].Name, got[1].Name)
	}

	bravo, err := loader.LoadByName("bravo")
	if err != nil {
		t.Fatalf("LoadByName(bravo): %v (expected to find via secondary root)", err)
	}
	if bravo.Description != "Secondary bravo" {
		t.Errorf("description = %q", bravo.Description)
	}
}

func TestLoaderMultiRootPrimaryWinsOnDuplicate(t *testing.T) {
	primary := t.TempDir()
	secondary := t.TempDir()
	writeSkill(t, primary, "shared", "shared", "Primary version", "primary body")
	writeSkill(t, secondary, "shared", "shared", "Secondary version", "secondary body")

	loader := NewLoader(primary, secondary)
	got, err := loader.LoadByName("shared")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Primary version" {
		t.Errorf("expected primary to win, got description=%q", got.Description)
	}

	all, _ := loader.LoadAll()
	if len(all) != 1 {
		t.Errorf("expected dedupe to 1 entry, got %d", len(all))
	}
}

func TestFSDeleterMultiRootDeletesFromSecondary(t *testing.T) {
	primary := t.TempDir()
	secondary := t.TempDir()
	if err := os.MkdirAll(filepath.Join(secondary, "claude-api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondary, "claude-api", "SKILL.md"), []byte("---\nname: claude-api\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := NewFSDeleter(primary, secondary)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("claude-api"); err != nil {
		t.Fatalf("Delete should find claude-api in secondary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondary, "claude-api")); !os.IsNotExist(err) {
		t.Fatalf("expected secondary entry removed, stat err = %v", err)
	}
}

func TestFSDeleterMultiRootNotFound(t *testing.T) {
	d, err := NewFSDeleter(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("missing"); !IsSkillNotFound(err) {
		t.Fatalf("want not-found, got %v", err)
	}
}

func TestParseSkillFrontmatter(t *testing.T) {
	skill, err := parseSkill([]byte("---\nname: test-skill\ndescription: Test skill\n---\n\n# Body\n"))
	if err != nil {
		t.Fatalf("parseSkill() error = %v", err)
	}
	if skill.Name != "test-skill" || skill.Description != "Test skill" || skill.Content != "# Body" {
		t.Fatalf("parseSkill() = %#v", skill)
	}
}

func TestParseSkillRejectsInvalidFrontmatter(t *testing.T) {
	cases := map[string]string{
		"missing":      "# Body",
		"unterminated": "---\nname: test\n# Body",
		"no-name":      "---\ndescription: nope\n---\nBody",
		"bad-name":     "---\nname: ../bad\n---\nBody",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSkill([]byte(body)); err == nil {
				t.Fatal("parseSkill() expected error")
			}
		})
	}
}

func TestPromptBlock(t *testing.T) {
	block := PromptBlock([]Skill{{
		Name:        "alpha",
		Description: "Alpha skill",
		Content:     "Use alpha carefully.",
	}})
	if !strings.Contains(block, "## Available Skills") {
		t.Fatalf("PromptBlock() missing heading:\n%s", block)
	}
	// Manifest mode: name + description present, body content NOT.
	if !strings.Contains(block, "**alpha** — Alpha skill") {
		t.Fatalf("PromptBlock() missing manifest line for alpha:\n%s", block)
	}
	if strings.Contains(block, "Use alpha carefully.") {
		t.Fatalf("PromptBlock() unexpectedly leaked body content:\n%s", block)
	}
	if !strings.Contains(block, "read_skill") {
		t.Fatalf("PromptBlock() missing read_skill instruction:\n%s", block)
	}
}

func TestPromptBlockTruncatesLongDescription(t *testing.T) {
	huge := strings.Repeat("x", maxManifestDescChars+500)
	block := PromptBlock([]Skill{{Name: "alpha", Description: huge}})
	if !strings.Contains(block, "[truncated]") {
		t.Fatalf("expected truncation marker in oversized description:\n%s", block)
	}
}

func TestPromptBlockBoundsTotalSize(t *testing.T) {
	// 50 skills × ~200 bytes per manifest line should stay under the
	// 8 KiB cap so the cap is a safety net, not a normal path.
	skills := make([]Skill, 0, 50)
	for i := 0; i < 50; i++ {
		skills = append(skills, Skill{
			Name:        fmt.Sprintf("skill-%02d", i),
			Description: "Apply when condition is met.",
		})
	}
	block := PromptBlock(skills)
	if len(block) >= maxSkillsBlockChars {
		t.Fatalf("manifest unexpectedly truncated at 50 small skills (len=%d)", len(block))
	}
	if !strings.Contains(block, "**skill-49**") {
		t.Errorf("expected last skill present in manifest")
	}
}

func writeSkill(t *testing.T, root, dirName, name, description, content string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + content
	writeFile(t, filepath.Join(root, dirName, "SKILL.md"), body)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
