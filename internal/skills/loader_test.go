package skills

import (
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
	if !strings.Contains(block, "### alpha") || !strings.Contains(block, "Use alpha carefully.") {
		t.Fatalf("PromptBlock() missing skill content:\n%s", block)
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
