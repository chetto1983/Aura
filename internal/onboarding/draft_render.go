package onboarding

import "strings"

// MaxAgentMDBytes caps the rendered Agent.md draft size.
const MaxAgentMDBytes = 32768

// Preferences is the structured companion the interview extracts alongside the
// Agent.md draft. Relocated from internal/profile for the Amendment #87
// supersession: the interview owns the draft-render surface; the on-disk
// profile package is retired.
type Preferences struct {
	Lang                string `json:"lang,omitempty"`
	Timezone            string `json:"timezone,omitempty"`
	Location            string `json:"location,omitempty"`
	VoiceMode           bool   `json:"voice_mode,omitempty"`
	CanProactiveMessage bool   `json:"can_proactive_message,omitempty"`
	TonePreference      string `json:"tone_preference,omitempty"`
	ResponseLength      string `json:"response_length,omitempty"`
}

// AgentContent is the structured form rendered into the Agent.md draft.
type AgentContent struct {
	Identity           []string
	ExpertiseTools     []string
	ProjectsGoals      []string
	Interests          []string
	People             []string
	Style              []string
	Vetoes             []string
	CustomInstructions []string
}

// RenderAgentMD renders the Agent.md draft with a stable section order. It is the
// human-review artifact shown at the onboarding confirm step; it is no longer
// persisted to disk (Amendment #87 stores the profile in the memory graph).
func RenderAgentMD(c AgentContent) string {
	var b strings.Builder
	b.WriteString("# Agent.md\n")
	writeSection(&b, "Identity", c.Identity)
	writeSection(&b, "Expertise & Tools", c.ExpertiseTools)
	writeSection(&b, "Projects & Goals", c.ProjectsGoals)
	writeSection(&b, "Interests", c.Interests)
	writeSection(&b, "People", c.People)
	writeSection(&b, "Style", c.Style)
	writeSection(&b, "Vetoes", c.Vetoes)
	writeSection(&b, "Custom Instructions", c.CustomInstructions)
	return b.String()
}

func writeSection(b *strings.Builder, name string, items []string) {
	b.WriteString("\n## ")
	b.WriteString(name)
	b.WriteByte('\n')
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item), "-"))
		if item == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteByte('\n')
	}
}
