package onboarding

import (
	"strings"
	"testing"
)

func TestRenderAgentMDSectionsSkipEmptyAndKeepOrder(t *testing.T) {
	t.Parallel()
	md := RenderAgentMD(AgentContent{
		Identity:       []string{"Name: Davide", "- Role: dev"}, // "- " prefix stripped
		ExpertiseTools: []string{"Go"},
		Style:          []string{"", "  ", "concise"}, // blank items skipped
	})
	for _, want := range []string{
		"# Agent.md", "## Identity", "- Name: Davide", "- Role: dev",
		"## Expertise & Tools", "- Go", "## Style", "- concise",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("RenderAgentMD missing %q in:\n%s", want, md)
		}
	}
	if got := strings.Count(md, "\n- "); got != 4 { // Name, Role, Go, concise
		t.Errorf("bullet count = %d, want 4 (blank Style items skipped):\n%s", got, md)
	}
	if strings.Index(md, "## Identity") > strings.Index(md, "## Expertise & Tools") {
		t.Error("stable section order violated: Identity must precede Expertise & Tools")
	}
}
