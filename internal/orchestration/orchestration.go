package orchestration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
)

const (
	VersionAuraAgentV1 = "aura-agent-v1"

	ToolsetModeAuto = "auto"

	ToolsetDefault  Toolset = "default"
	ToolsetCompute  Toolset = "compute"
	ToolsetDocument Toolset = "document"
	ToolsetAdmin    Toolset = "admin"

	ModuleBase           = "base"
	ModuleRuntime        = "runtime"
	ModuleOverlay        = "overlay"
	ModuleSkills         = "skills"
	ModuleSandbox        = "sandbox"
	ModuleMemory         = "memory"
	ModuleFileGeneration = "file_generation"
	ModuleSecurity       = "security"
	ModuleWikiProposals  = "wiki_proposals"
)

type Toolset string

type Availability struct {
	Swarm          bool
	Sandbox        bool
	Proposals      bool
	WorkspaceFiles bool
}

type ToolsetDecision struct {
	Toolset Toolset
	Reason  string
}

type ToolsetContext struct {
	Toolset      Toolset
	Availability Availability
}

type RuntimeToolset interface {
	Name() string
	Tools(ToolsetContext) ([]string, error)
}

type ToolPredicate func(ToolsetContext, string) bool

type staticToolset struct {
	toolset Toolset
}

type filteredToolset struct {
	base      RuntimeToolset
	predicate ToolPredicate
}

type PromptInput struct {
	Version           string
	Now               time.Time
	Location          *time.Location
	Overlay           string
	SkillsBlock       string
	SwarmAvailable    bool
	SandboxAvailable  bool
	ProposalAvailable bool
	Toolset           Toolset
}

type PromptPlan struct {
	Content string
	Version string
	Modules []string
	Hash    string
}

var (
	workspaceTools = []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"}
	swarmTools     = []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}

	defaultTools = []string{
		"search_memory",
		"list_sources", "read_source", "store_source",
		"web_search", "web_fetch",
		"schedule_task", "list_tasks", "cancel_task",
		"daily_briefing",
	}
	computeTools = []string{
		"search_memory",
		"list_sources", "read_source", "store_source",
		"web_search", "web_fetch",
		"execute_code", "list_tools", "read_tool",
	}
	documentTools = []string{
		"search_memory",
		"create_docx", "create_xlsx", "create_pdf",
	}
	adminTools = []string{
		"search_memory",
		"list_sources", "read_source",
		"web_search", "web_fetch",
		"daily_briefing", "list_tasks", "schedule_task", "cancel_task", "run_task_now",
		"request_dashboard_token", "install_skill", "delete_skill", "settings_update",
	}
)

func ComposePrompt(in PromptInput) PromptPlan {
	version := strings.TrimSpace(in.Version)
	if version == "" {
		version = VersionAuraAgentV1
	}
	toolset := normalizeToolset(string(in.Toolset))
	if in.Now.IsZero() {
		in.Now = time.Now()
	}

	content := conversation.RenderSystemPrompt(in.Now, in.Location)
	modules := []string{ModuleBase, ModuleRuntime}

	content += fmt.Sprintf("\n\n## Aura Runtime\n- Prompt Version: %s\n- Toolset: %s\n", version, toolset)
	content += toolsetPrompt(toolset)
	modules = append(modules, ModuleSecurity)

	if memory := memoryPrompt(toolset); memory != "" {
		content += memory
		modules = append(modules, ModuleMemory)
	}
	if overlay := strings.TrimSpace(in.Overlay); overlay != "" {
		content += "\n\n" + overlay
		modules = append(modules, ModuleOverlay)
	}
	if skills := strings.TrimSpace(in.SkillsBlock); skills != "" {
		content += "\n\n" + skills
		content += skillUsePrompt()
		modules = append(modules, ModuleSkills)
	}
	if in.SandboxAvailable {
		content += sandboxPrompt(toolset)
		modules = append(modules, ModuleSandbox)
	}
	if filePrompt(toolset) != "" {
		content += filePrompt(toolset)
		modules = append(modules, ModuleFileGeneration)
	}

	sum := sha256.Sum256([]byte(content))
	return PromptPlan{
		Content: content,
		Version: version,
		Modules: modules,
		Hash:    hex.EncodeToString(sum[:])[:16],
	}
}

func SelectToolset(userText, mode string, available Availability) Toolset {
	return SelectToolsetDecision(userText, mode, available).Toolset
}

func SelectToolsetDecision(_ string, mode string, available Availability) ToolsetDecision {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = ToolsetModeAuto
	}
	if mode != ToolsetModeAuto {
		toolset := normalizeToolset(mode)
		if toolset == ToolsetCompute && !available.Sandbox {
			return ToolsetDecision{
				Toolset: ToolsetDefault,
				Reason:  "explicit compute toolset unavailable: missing sandbox; using default",
			}
		}
		return ToolsetDecision{Toolset: toolset, Reason: fmt.Sprintf("explicit toolset %q", toolset)}
	}

	return ToolsetDecision{Toolset: ToolsetDefault, Reason: "auto mode uses default toolset"}
}

func ToolsForToolset(toolset Toolset, available Availability) ([]string, error) {
	return NewRuntimeToolset(toolset).Tools(ToolsetContext{Toolset: toolset, Availability: available})
}

func NewRuntimeToolset(toolset Toolset) RuntimeToolset {
	return staticToolset{toolset: normalizeToolset(string(toolset))}
}

func FilterToolset(base RuntimeToolset, predicate ToolPredicate) RuntimeToolset {
	return filteredToolset{base: base, predicate: predicate}
}

func (s staticToolset) Name() string {
	return string(s.toolset)
}

func (s staticToolset) Tools(ctx ToolsetContext) ([]string, error) {
	toolset := normalizeToolset(string(s.toolset))
	if ctx.Toolset != "" {
		toolset = normalizeToolset(string(ctx.Toolset))
	}
	available := ctx.Availability
	toolset = normalizeToolset(string(toolset))
	if toolset == ToolsetCompute && !available.Sandbox {
		return nil, fmt.Errorf("toolset %q unavailable: missing sandbox", toolset)
	}

	var tools []string
	switch toolset {
	case ToolsetDefault:
		tools = append(tools, defaultTools...)
	case ToolsetCompute:
		tools = append(tools, computeTools...)
	case ToolsetDocument:
		tools = append(tools, documentTools...)
	case ToolsetAdmin:
		tools = append(tools, adminTools...)
	default:
		return nil, fmt.Errorf("unknown toolset %q", toolset)
	}
	if available.WorkspaceFiles && toolset != ToolsetDocument {
		tools = append(tools, workspaceTools...)
	}
	if available.Swarm && toolset != ToolsetDefault {
		tools = append(tools, swarmTools...)
	}
	return uniqueTools(tools), nil
}

func (f filteredToolset) Name() string {
	if f.base == nil {
		return ""
	}
	return f.base.Name()
}

func (f filteredToolset) Tools(ctx ToolsetContext) ([]string, error) {
	if f.base == nil {
		return nil, fmt.Errorf("toolset is nil")
	}
	tools, err := f.base.Tools(ctx)
	if err != nil {
		return nil, err
	}
	if f.predicate == nil {
		return tools, nil
	}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		if f.predicate(ctx, tool) {
			out = append(out, tool)
		}
	}
	return out, nil
}

func normalizeToolset(value string) Toolset {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ToolsetCompute):
		return ToolsetCompute
	case string(ToolsetDocument):
		return ToolsetDocument
	case string(ToolsetAdmin):
		return ToolsetAdmin
	case string(ToolsetDefault):
		return ToolsetDefault
	default:
		return ToolsetDefault
	}
}

func uniqueTools(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, tool := range in {
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		out = append(out, tool)
	}
	return out
}

func toolsetPrompt(toolset Toolset) string {
	switch toolset {
	case ToolsetCompute:
		return "\nUse execute_code for real computation, transformations, charts, parser experiments, and generated artifacts. Keep generated files under /tmp/aura_out."
	case ToolsetDocument:
		return "\nUse only the tools exposed for this turn. In document mode, the active tools are search_memory when exposed plus create_docx/create_xlsx/create_pdf. Use compact memory evidence first, then one typed file tool for ordinary static documents."
	case ToolsetAdmin:
		return "\nUse admin tools only for explicit dashboard, settings, token, skill, MCP, task, or review-queue work. Concrete tools still enforce auth/admin gates."
	default:
		return "\nUse the default toolset. Answer directly when the conversation already has enough context. Call tools only for missing facts, durable actions, file/source inspection, scheduling, or explicit user requests; keep ordinary turns short."
	}
}

func memoryPrompt(toolset Toolset) string {
	if toolset == ToolsetDocument {
		return "\n\n## Memory\nFor document generation, use the Retrieval Capsule directly when it is present, then create_docx/create_xlsx/create_pdf. If no capsule is present and search_memory is exposed, use at most one search_memory call for compact evidence. Do not browse workspace files, web pages, or raw source bodies unless those tools are explicitly exposed in the current turn."
	}
	if toolset == ToolsetDefault {
		return "\n\n## Memory\nUse search_memory for Aura wiki, source, archive, proposal, and graph facts when the user needs project memory or evidence. Prefer one focused memory query before trying raw file, source, or web tools."
	}
	return ""
}

func sandboxPrompt(toolset Toolset) string {
	if toolset == ToolsetCompute {
		return "\n\n## Python Sandbox\nUse execute_code for calculations, transformations, charts, generated artifacts, parser experiments, and repeatable debug scripts. Write deliverable files under /tmp/aura_out so Aura can persist and deliver them."
	}
	return "\n\n## Python Sandbox\nUse execute_code only when the request genuinely needs computation, data processing, custom generated files, plots, simulations, or parser/debug scripts."
}

func filePrompt(toolset Toolset) string {
	if toolset != ToolsetDocument {
		return ""
	}
	return "\n\n## File Generation\nUse create_docx/create_xlsx/create_pdf for ordinary static user-facing files; use execute_code only for computed artifacts. Tool call examples are attached to each tool definition."
}

func skillUsePrompt() string {
	return "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it or when it would materially improve the work. Do not read skills just to satisfy a ritual."
}
