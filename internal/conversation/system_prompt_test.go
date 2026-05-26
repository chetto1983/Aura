package conversation

import (
	"strings"
	"testing"
)

func TestDefaultSystemPromptProfessionalCapsuleContract(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, want := range []string{
		"## Aura Identity",
		"## Authority And Context Precedence",
		"## Operating Loop",
		"## Tool Policy",
		"## Minimal Tool Routing",
		"## Memory Layers",
		"## Context Capsules",
		"## Ask User Policy",
		"## Output Contract",
		"## Privacy And Safety",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("DefaultSystemPrompt missing section %q", want)
		}
	}
	for _, want := range []string{
		`query="select:<tool_name>"`,
		"Use it exactly once when the answer is ready",
		"The system prompt itself is stable policy, not conversation memory",
		"Do not carry raw transcripts",
		"full tool outputs",
		"full schema catalogs",
		"artifact IDs",
		"tool_attempt metadata",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("DefaultSystemPrompt missing contract phrase %q", want)
		}
	}
}

func TestDefaultSystemPromptExcludesRetiredOverlayAndToolNames(t *testing.T) {
	prompt := DefaultSystemPrompt()
	for _, forbidden := range []string{
		"SOUL.md",
		"USER.md",
		"AGENT.md",
		"TOOLS.md",
		"execute_shell",
		"execute_code",
		"ask_user_clarification",
		"subagent_dispatch",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("DefaultSystemPrompt contains retired prompt/tool reference %q", forbidden)
		}
	}
}
