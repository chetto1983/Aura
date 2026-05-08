package main

import (
	"strings"
	"testing"
)

func TestSkillBackedDocxScenarioRequiresSkillsBeforeDocx(t *testing.T) {
	sc := skillBackedDocxScenario()

	if sc.wantTool != "create_docx" {
		t.Fatalf("wantTool = %q, want create_docx", sc.wantTool)
	}
	for _, want := range []string{"search_files", "read_file", "create_docx"} {
		if !containsTool(sc.wantTools, want) {
			t.Fatalf("skill-backed docx scenario missing required tool %q: %+v", want, sc.wantTools)
		}
	}
	if !strings.Contains(sc.prompt, "documenti Aura") {
		t.Fatalf("prompt should be the real Aura document-summary request, got %q", sc.prompt)
	}
}

func TestSkillAwareSystemPromptRequiresReadingSkill(t *testing.T) {
	prompt := buildSystemPrompt(true)
	for _, want := range []string{"search_files", "read_file", "before create_docx"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}
