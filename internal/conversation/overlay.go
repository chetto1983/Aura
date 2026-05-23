package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// overlayFiles is the trusted operator prompt overlay set. Each file is read
// fresh every turn so edits take effect with no recompile or restart.
//
// AGENT.md is Aura's runtime workspace note, readable through file tools when
// needed. It is deliberately not injected into the system prompt. AGENTS.md is
// also excluded because it contains repository development instructions.
// TOOLS.md was retired 2026-05-24: tool guidance lives inside each tool's
// Description (where the model actually consumes it) — a separate overlay file
// duplicated the surface and rotted independently.
var overlayFiles = []string{"SOUL.md", "USER.md"}

// overlayWatchedFiles is the set of overlay basenames that, when written by
// the file tool, trigger a mid-conversation system-prompt hot-reload. Includes
// AGENT.md and OPS.md even though they are not in overlayFiles: writes to
// either can still affect context (AGENT.md is file-tool readable; OPS.md is a
// planned sibling overlay).
var overlayWatchedFiles = map[string]struct{}{
	"SOUL.md":  {},
	"USER.md":  {},
	"OPS.md":   {},
	"AGENT.md": {},
}

// IsOverlayFileName reports whether name (basename only, no directory) is one
// of the operator overlay files whose writes should trigger a prompt reload.
func IsOverlayFileName(name string) bool {
	_, ok := overlayWatchedFiles[name]
	return ok
}

var defaultPromptOverlayFiles = map[string]string{
	"SOUL.md": `# Soul

Aura risponde in modo naturale, diretto e umano. Usa i tool come strumenti interni: il risultato tecnico serve per capire, non per essere incollato all'utente. Quando non serve un tool, risponde direttamente.
`,
}

// EnsurePromptOverlayDefaults creates the minimal runtime overlay files Aura
// needs for natural answers when the runtime workspace is empty. Existing files
// are never overwritten. AGENT.md/AGENTS.md stay excluded from prompt overlay.
func EnsurePromptOverlayDefaults(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, body := range defaultPromptOverlayFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

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
