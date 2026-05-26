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
// pinned operational rules, skill manifest, tool manifest, and the inline wiki TOC.
// Channel-neutral: callers pre-render any channel-specific strings before passing them in.
func ComposeAgentPrompt(cfg *config.Config, loc *time.Location, overlay, pinnedOperational, skillsBlock, toolManifest, wikiTOC string, now time.Time) AgentPromptPlan {
	version := "aura-agent-v1"
	if cfg != nil && strings.TrimSpace(cfg.PromptVersion) != "" {
		version = strings.TrimSpace(cfg.PromptVersion)
	}
	modules := []string{"base", "runtime", "clarification-protocol", "registered-tools"}
	content := conversation.RenderSystemPrompt(now, loc)
	content += fmt.Sprintf("\n\n## Aura Runtime\n- Prompt Version: %s\n- Tool Discovery: the catalog below lists every tool you have. Invoke any tool by name — the agent loop will load its full schema for this turn.\n\nChoose tools autonomously when they help. For workspace inspection and edits, use the file tool's bounded actions (read/list/search/grep/patch/move/copy/walk) instead of command execution. Prefer direct answers when no tool is needed.", version)
	content += "\n\n" + conversation.ClarificationAndApprovalProtocol()
	if injected := conversation.InjectWikiTOC(content, wikiTOC); injected != content {
		content = injected
		modules = append(modules, "wiki-toc")
	}
	if strings.TrimSpace(overlay) != "" {
		content += "\n\n" + strings.TrimSpace(overlay)
		modules = append(modules, "overlay")
	}
	if strings.TrimSpace(pinnedOperational) != "" {
		content += "\n\n" + strings.TrimSpace(pinnedOperational)
		modules = append(modules, "pinned-operational")
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
