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
// pinned operational rules, skill manifest, and compact routing hints.
// Channel-neutral: callers pre-render any channel-specific strings before passing them in.
func ComposeAgentPrompt(cfg *config.Config, loc *time.Location, overlay, pinnedOperational, skillsBlock, toolManifest, wikiTOC string, now time.Time) AgentPromptPlan {
	version := "aura-agent-v1"
	if cfg != nil && strings.TrimSpace(cfg.PromptVersion) != "" {
		version = strings.TrimSpace(cfg.PromptVersion)
	}
	modules := []string{"base", "turn-runtime", "clarification-protocol", "tool-routing"}
	content := conversation.DefaultSystemPrompt()
	content += "\n\n" + conversation.RenderTurnRuntimeCapsule(now, loc)
	content += fmt.Sprintf("\n\n## Aura Runtime\n- Prompt Version: %s\n- Tool schemas supplied with the LLM request are ground truth. Copy parameter names and enum values exactly.\n- Only a small hot-path tool set is loaded by default. Use tool_search to load specialized or write-capable tools when the task clearly needs them.\n- Use search for memory/wiki/source recall before guessing. Use web for current public facts. Use text_response to finish the turn.", version)
	content += "\n\n" + conversation.ClarificationAndApprovalProtocol()
	if strings.TrimSpace(wikiTOC) != "" {
		content += "\n\n## Wiki Access\nThe wiki graph is available through search actions, not as a dumped table of contents. Search first, then read or expand only the relevant page/path/subgraph."
		modules = append(modules, "wiki-access")
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
		content += "\n\n## Tool Surface\nThe full registered catalog is discoverable with tool_search. Default hot-path schemas are intentionally limited to reduce context bloat and wrong-tool calls. Load write tools only when the user requested a write or approved one."
		modules = append(modules, "tool-surface")
	}
	sum := sha256.Sum256([]byte(content))
	return AgentPromptPlan{
		Content: content,
		Version: version,
		Hash:    hex.EncodeToString(sum[:8]),
		Modules: modules,
	}
}
