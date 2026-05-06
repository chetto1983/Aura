package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillBackedDocxScenarioRequiresSkillsBeforeDocx(t *testing.T) {
	sc := skillBackedDocxScenario()

	if sc.wantTool != "create_docx" {
		t.Fatalf("wantTool = %q, want create_docx", sc.wantTool)
	}
	for _, want := range []string{"list_skills", "read_skill", "create_docx"} {
		if !containsTool(sc.wantTools, want) {
			t.Fatalf("skill-backed docx scenario missing required tool %q: %+v", want, sc.wantTools)
		}
	}
	if !strings.Contains(sc.prompt, "documenti Aura") {
		t.Fatalf("prompt should be the real Aura document-summary request, got %q", sc.prompt)
	}
}

func TestDebugSkillLoaderSeesCatalogInstalledSkillsUnderSkillsPath(t *testing.T) {
	root := t.TempDir()
	writeDebugSkill(t, filepath.Join(root, "skills", ".claude", "skills", "docx", "SKILL.md"), "docx")

	loader := newDebugSkillLoader(filepath.Join(root, "skills"), "")
	loaded, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "docx" {
		t.Fatalf("loaded skills = %+v, want docx from skills/.claude/skills", loaded)
	}
}

func TestSkillAwareSystemPromptRequiresReadingSkill(t *testing.T) {
	prompt := buildSystemPrompt(true)
	for _, want := range []string{"list_skills", "read_skill", "before create_docx"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func writeDebugSkill(t *testing.T, path, name string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: Use for Word documents.\n---\n\n# " + name + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
