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

	ProfileModeAuto = "auto"

	ProfileDefault        Profile = "default"
	ProfileMemory         Profile = "memory"
	ProfileSwarmResearch  Profile = "swarm_research"
	ProfileSandboxCompute Profile = "sandbox_compute"
	ProfileDocument       Profile = "document"
	ProfileAdminReview    Profile = "admin_review"

	ModuleBase           = "base"
	ModuleRuntime        = "runtime"
	ModuleOverlay        = "overlay"
	ModuleSkills         = "skills"
	ModuleSwarm          = "swarm"
	ModuleSandbox        = "sandbox"
	ModuleMemory         = "memory"
	ModuleFileGeneration = "file_generation"
	ModuleSecurity       = "security"
	ModuleWikiProposals  = "wiki_proposals"
)

type Profile string

type Availability struct {
	Swarm     bool
	Sandbox   bool
	Proposals bool
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
	Profile           Profile
}

type PromptPlan struct {
	Content string
	Version string
	Modules []string
	Hash    string
}

func ComposePrompt(in PromptInput) PromptPlan {
	version := strings.TrimSpace(in.Version)
	if version == "" {
		version = VersionAuraAgentV1
	}
	profile := normalizeProfile(string(in.Profile))
	if in.Now.IsZero() {
		in.Now = time.Now()
	}

	content := conversation.RenderSystemPrompt(in.Now, in.Location)
	modules := []string{ModuleBase, ModuleRuntime}

	content += fmt.Sprintf("\n\n## Aura Orchestration\n- Prompt Version: %s\n- Tool Profile: %s\n", version, profile)
	content += profilePrompt(profile)
	modules = append(modules, ModuleSecurity)

	if memoryPrompt(profile) != "" {
		content += memoryPrompt(profile)
		modules = append(modules, ModuleMemory)
	}
	if overlay := strings.TrimSpace(in.Overlay); overlay != "" {
		content += "\n\n" + overlay
		modules = append(modules, ModuleOverlay)
	}
	if skills := strings.TrimSpace(in.SkillsBlock); skills != "" {
		content += "\n\n" + skills
		content += skillPreflightPrompt(profile)
		modules = append(modules, ModuleSkills)
	}
	if in.SwarmAvailable {
		content += "\n\n" + conversation.SwarmRoutingPrompt()
		content += swarmProfilePrompt(profile)
		modules = append(modules, ModuleSwarm)
	}
	if in.SandboxAvailable {
		content += sandboxPrompt(profile)
		modules = append(modules, ModuleSandbox)
	}
	if filePrompt(profile) != "" {
		content += filePrompt(profile)
		modules = append(modules, ModuleFileGeneration)
	}
	if in.ProposalAvailable {
		content += "\n\n" + conversation.WikiProposalPrompt()
		modules = append(modules, ModuleWikiProposals)
	}

	sum := sha256.Sum256([]byte(content))
	return PromptPlan{
		Content: content,
		Version: version,
		Modules: modules,
		Hash:    hex.EncodeToString(sum[:])[:16],
	}
}

func SelectProfile(userText, mode string, available Availability) Profile {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = ProfileModeAuto
	}
	if mode != ProfileModeAuto {
		return normalizeProfile(mode)
	}

	text := routeText(userText)
	switch {
	case available.Swarm && looksLikeSwarmResearch(text):
		return ProfileSwarmResearch
	case available.Sandbox && looksLikeSandboxCompute(text):
		return ProfileSandboxCompute
	case looksLikeDocument(text):
		return ProfileDocument
	case looksLikeMemory(text):
		return ProfileMemory
	default:
		return ProfileDefault
	}
}

func ToolsForProfile(profile Profile, available Availability) ([]string, error) {
	switch normalizeProfile(string(profile)) {
	case ProfileDefault:
		return []string{
			"search_memory", "search_wiki", "read_wiki", "write_wiki",
			"list_wiki", "list_sources", "read_source",
			"web_search", "web_fetch",
			"schedule_task", "list_tasks", "cancel_task", "run_task_now",
			"daily_briefing", "request_dashboard_token",
		}, nil
	case ProfileMemory:
		tools := []string{
			"search_memory", "list_wiki", "read_wiki", "search_wiki",
			"list_sources", "read_source", "lint_wiki", "lint_sources",
			"daily_briefing",
		}
		if available.Proposals {
			tools = append(tools, "propose_wiki_change", "propose_skill_change")
		}
		return tools, nil
	case ProfileSwarmResearch:
		tools := []string{
			"search_memory", "list_wiki", "read_wiki", "search_wiki",
			"list_sources", "read_source", "lint_wiki", "lint_sources",
		}
		if available.Swarm {
			tools = append([]string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}, tools...)
		}
		return tools, nil
	case ProfileSandboxCompute:
		tools := []string{
			"execute_code", "list_tools", "read_tool",
			"search_memory", "list_sources", "read_source", "store_source",
		}
		return tools, nil
	case ProfileDocument:
		tools := []string{
			"list_skills", "read_skill", "search_skill_catalog",
			"search_memory", "list_wiki", "read_wiki", "search_wiki",
			"list_sources", "read_source",
			"create_docx", "create_xlsx", "create_pdf",
		}
		if available.Swarm {
			tools = append(tools, "run_aurabot_swarm", "read_swarm_result")
		}
		return tools, nil
	case ProfileAdminReview:
		return []string{
			"daily_briefing", "list_tasks", "run_task_now",
			"propose_wiki_change", "propose_skill_change",
			"list_skills", "read_skill", "search_skill_catalog",
		}, nil
	default:
		return nil, fmt.Errorf("unknown orchestration profile %q", profile)
	}
}

func normalizeProfile(value string) Profile {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ProfileMemory):
		return ProfileMemory
	case string(ProfileSwarmResearch):
		return ProfileSwarmResearch
	case string(ProfileSandboxCompute):
		return ProfileSandboxCompute
	case string(ProfileDocument):
		return ProfileDocument
	case string(ProfileAdminReview):
		return ProfileAdminReview
	default:
		return ProfileDefault
	}
}

func routeText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(text))), " ")
}

func looksLikeSwarmResearch(text string) bool {
	return containsAny(text, []string{
		"facciamo il punto", "tutta la pipeline", "tutta la memoria", "tutto il repo",
		"cosa manca", "what is missing", "audit", "review", "mappa", "roadmap",
		"quality", "qualita", "consolidation", "consolidamento",
	}) || conversation.LooksLikeSwarmReadGoal(text)
}

func looksLikeSandboxCompute(text string) bool {
	return containsAny(text, []string{
		"calcola", "calculate", "compute", "grafico", "chart", "plot",
		"csv", "dataframe", "dataset", "simulation", "simulazione",
		"python", "parser", "script", "debug script", "artifact", "artifacts",
		"trasforma", "transform", "analisi dati", "data analysis",
	})
}

func looksLikeDocument(text string) bool {
	return containsAny(text, []string{
		"documento word", "word modificabile", "docx", "pdf", "xlsx",
		"spreadsheet", "foglio", "presentami un documento", "crea un documento",
		"genera un documento", "report", "relazione",
	})
}

func looksLikeMemory(text string) bool {
	return containsAny(text, []string{
		"ricordi", "remember", "memoria", "memory", "second brain",
		"wiki", "fonti", "sources", "source", "cosa sai",
	})
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func profilePrompt(profile Profile) string {
	switch profile {
	case ProfileSwarmResearch:
		return "\nUse a swarm-first read-only route for broad synthesis, audits, planning, memory quality checks, and pipeline reviews. Do not write or create files in this profile."
	case ProfileSandboxCompute:
		return "\nUse Python sandbox first for computation, data transforms, charts, simulations, parser experiments, and generated artifacts. Keep generated files under /tmp/aura_out."
	case ProfileDocument:
		return "\nUse skills and memory/source evidence first, optionally swarm for broad synthesis, then typed file tools for ordinary static documents."
	case ProfileMemory:
		return "\nUse local memory/source/wiki tools for evidence-backed answers. Keep durable changes review-gated unless the user explicitly asks to remember."
	case ProfileAdminReview:
		return "\nUse review/proposal tools only. Do not silently mutate skills, MCP plugins, settings, or files."
	default:
		return "\nUse the smallest relevant tool surface. Prefer direct tools for simple work."
	}
}

func memoryPrompt(profile Profile) string {
	if profile == ProfileMemory || profile == ProfileSwarmResearch || profile == ProfileDocument {
		return "\n\n## Memory Route\nFor broad or source-backed answers, gather evidence with search_memory or read-only source/wiki tools before synthesizing. Keep citations compact when the user asks for proof."
	}
	return ""
}

func swarmProfilePrompt(profile Profile) string {
	if profile == ProfileSwarmResearch {
		return "\n\n## Swarm Profile\nCall run_aurabot_swarm before direct sequential reads unless the answer is already obvious from current context. Keep the swarm goal read-only."
	}
	if profile == ProfileDocument {
		return "\n\n## Document Evidence Profile\nFor multi-document summaries, use run_aurabot_swarm as a read-only evidence phase before creating the final file."
	}
	return ""
}

func sandboxPrompt(profile Profile) string {
	if profile == ProfileSandboxCompute {
		return "\n\n## Python Sandbox Route\nUse execute_code for calculations, transformations, charts, generated artifacts, parser experiments, and repeatable debug scripts. Write deliverable files under /tmp/aura_out so Aura persists and can deliver them."
	}
	return "\n\n## Python Sandbox Route\nUse execute_code only when the request genuinely needs computation, data processing, custom generated files, plots, simulations, or parser/debug scripts."
}

func filePrompt(profile Profile) string {
	if profile != ProfileDocument {
		return ""
	}
	return "\n\n## File Generation Route\nBefore using typed document/file tools, inspect relevant installed skills with list_skills and read_skill. Use create_docx/create_xlsx/create_pdf for ordinary static user-facing files; use execute_code only for computed artifacts."
}

func skillPreflightPrompt(profile Profile) string {
	if profile == ProfileDocument || profile == ProfileSandboxCompute {
		return "\n\n## Skill Preflight\nIf an installed skill matches the requested capability, call list_skills and read_skill before using that capability and before using typed document/file tools. This applies to DOCX, PDF, XLSX, source extraction, sandbox/coding workflows, and future MCP/plugin skills."
	}
	return "\n\n## Skill Preflight\nIf an installed skill description matches the user's request, call read_skill before acting on that skill's guidance."
}
