package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// overlayFiles is the list of optional Markdown files Aura reads from the
// prompt overlay directory on every conversation turn. Picobot established
// this set: SOUL.md = identity/tone, AGENTS.md = collaboration norms,
// USER.md = durable facts about the operator, TOOLS.md = operator hints
// for tool selection. Each file is read fresh each turn so editing one
// takes effect on the very next message — no recompile, no restart.
var overlayFiles = []string{"SOUL.md", "AGENTS.md", "USER.md", "TOOLS.md"}

// LoadPromptOverlay reads any of overlayFiles present under dir and
// returns a concatenated block ready to append to the system prompt.
// Missing files are skipped silently; an unreadable directory yields the
// empty string with no error so a misconfigured path can never block a
// turn. Each section is fenced with a Markdown heading derived from the
// file name so the LLM can attribute guidance to its source.
func LoadPromptOverlay(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}

	var sb strings.Builder
	for _, name := range overlayFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(data))
		if body == "" {
			continue
		}
		// Heading is the file name without .md, lowercased.
		heading := strings.TrimSuffix(name, ".md")
		fmt.Fprintf(&sb, "\n\n## %s\n\n%s", heading, body)
	}
	return strings.TrimSpace(sb.String())
}
