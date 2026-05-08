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
	if !strings.Contains(block, "**aura-source-extraction**") || !strings.Contains(block, "read_file") {
		t.Fatalf("prompt block should advertise skill and tell agent to inspect it through files:\n%s", block)
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

func TestProjectRuntimeSkillsIncludeSandboxAndAuthoringGuidance(t *testing.T) {
	loader := NewLoader(filepath.Join("..", "..", "skills"))

	cases := map[string][]string{
		"aura-python-sandbox": {
			"execute_code",
			"/tmp/aura_out",
			"allow_network=false",
			"sandbox_artifact",
		},
		"aura-skill-authoring": {
			"SKILL.md",
			"Use when",
		},
	}

	for name, wants := range cases {
		t.Run(name, func(t *testing.T) {
			skill, err := loader.LoadByName(name)
			if err != nil {
				t.Fatalf("LoadByName(%s): %v", name, err)
			}
			for _, want := range wants {
				if !strings.Contains(skill.Content, want) && !strings.Contains(skill.Description, want) {
					t.Fatalf("%s missing %q:\nDescription: %s\n\n%s", name, want, skill.Description, skill.Content)
				}
			}
		})
	}
}
