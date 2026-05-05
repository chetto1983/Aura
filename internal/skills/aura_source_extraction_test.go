package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAuraSourceExtractionSkillGuidesSandboxCode(t *testing.T) {
	loader := NewLoader(filepath.Join("..", "..", "skills"))

	skill, err := loader.LoadByName("aura-source-extraction")
	if err != nil {
		t.Fatalf("LoadByName(aura-source-extraction): %v", err)
	}
	if !strings.Contains(skill.Description, "Pyodide extractors") {
		t.Fatalf("description should trigger for Pyodide extraction work: %q", skill.Description)
	}

	block := PromptBlock([]Skill{skill})
	if !strings.Contains(block, "**aura-source-extraction**") || !strings.Contains(block, "read_skill") {
		t.Fatalf("prompt block should advertise skill and tell agent to read it:\n%s", block)
	}
	if strings.Contains(block, "allowNetwork=false") {
		t.Fatalf("prompt block leaked skill body instead of using progressive disclosure:\n%s", block)
	}

	requiredGuidance := []string{
		"extract.md",
		"extract.json",
		"allowNetwork=false",
		"fixed Aura-owned extractor",
		"Do not run arbitrary user Python",
		"Quality Bar",
		"deterministic",
		"test-driven-development",
	}
	for _, want := range requiredGuidance {
		if !strings.Contains(skill.Content, want) {
			t.Fatalf("skill content missing %q:\n%s", want, skill.Content)
		}
	}
}
