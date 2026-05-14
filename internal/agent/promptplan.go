package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
)

// AgentPromptPlan is the fully assembled system prompt with version metadata.
type AgentPromptPlan struct {
	Content string
	Version string
	Hash    string
	Modules []string
}

// ComposeAgentPrompt assembles the agent system prompt from config, runtime overlay,
// skill manifest, and tool manifest. Channel-neutral: callers pre-render any
// channel-specific strings before passing them in.
func ComposeAgentPrompt(cfg *config.Config, loc *time.Location, overlay, skillsBlock, toolManifest string, now time.Time) AgentPromptPlan {
	version := "aura-agent-v1"
	if cfg != nil && strings.TrimSpace(cfg.PromptVersion) != "" {
		version = strings.TrimSpace(cfg.PromptVersion)
	}
	modules := []string{"base", "runtime", "registered-tools"}
	content := conversation.RenderSystemPrompt(now, loc)
	content += fmt.Sprintf("\n\n## Aura Runtime\n- Prompt Version: %s\n- Tool Discovery: the catalog below lists every tool you have. Call tool_search to fetch input schemas, OR invoke any tool by name and the agentloop will load its schema for this turn.\n\nChoose tools autonomously when they help. For multi-step work, prefer execute_code or execute_shell to inspect, loop, transform, and verify in one runtime pass instead of asking for many model tool-call rounds. Prefer direct answers when no tool is needed.", version)
	if strings.TrimSpace(overlay) != "" {
		content += "\n\n" + strings.TrimSpace(overlay)
		modules = append(modules, "overlay")
	}
	if strings.TrimSpace(skillsBlock) != "" {
		content += "\n\n" + strings.TrimSpace(skillsBlock)
		content += "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it or when it materially improves the work."
		modules = append(modules, "skills")
	}
	if strings.TrimSpace(toolManifest) != "" {
		content += "\n\n" + strings.TrimSpace(toolManifest)
		modules = append(modules, "tool-manifest")
	}
	sum := sha256.Sum256([]byte(content))
	return AgentPromptPlan{
		Content: content,
		Version: version,
		Hash:    hex.EncodeToString(sum[:8]),
		Modules: modules,
	}
}
