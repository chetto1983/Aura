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

type AccessLevel string

const (
	AccessDefault    AccessLevel = "default"
	AccessReadOnly   AccessLevel = "read_only"
	AccessWrite      AccessLevel = "write"
	AccessReviewOnly AccessLevel = "review_only"
	AccessSandbox    AccessLevel = "sandbox"
)

type Availability struct {
	Swarm     bool
	Sandbox   bool
	Proposals bool
}

type ProfileDecision struct {
	Profile Profile
	Reason  string
}

type ProfileCard struct {
	Profile              Profile
	Purpose              string
	Access               AccessLevel
	PositiveCues         []string
	NegativeCues         []string
	RequiredAvailability []string
	AllowedTools         []string
	DeniedTools          []string
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
	if skills := strings.TrimSpace(in.SkillsBlock); skills != "" && profileSupportsSkillTools(profile) {
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
	return SelectProfileDecision(userText, mode, available).Profile
}

func SelectProfileDecision(userText, mode string, available Availability) ProfileDecision {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = ProfileModeAuto
	}
	if mode != ProfileModeAuto {
		profile := normalizeProfile(mode)
		if missing := missingAvailability(profile, available); missing != "" {
			return ProfileDecision{
				Profile: ProfileDefault,
				Reason:  fmt.Sprintf("explicit profile %q unavailable: missing %s; using default route", profile, missing),
			}
		}
		return ProfileDecision{
			Profile: profile,
			Reason:  fmt.Sprintf("explicit profile mode %q", profile),
		}
	}

	text := routeText(userText)
	switch {
	case available.Swarm && looksLikeSwarmResearch(text):
		return ProfileDecision{Profile: ProfileSwarmResearch, Reason: "matched swarm_research broad synthesis cues"}
	case !available.Swarm && looksLikeSwarmResearch(text):
		return ProfileDecision{Profile: ProfileMemory, Reason: "swarm_research cues matched but swarm unavailable; using memory read route"}
	case available.Sandbox && looksLikeSandboxCompute(text):
		return ProfileDecision{Profile: ProfileSandboxCompute, Reason: "matched sandbox_compute compute/artifact cues"}
	case !available.Sandbox && looksLikeSandboxCompute(text):
		return ProfileDecision{Profile: ProfileDefault, Reason: "sandbox_compute cues matched but sandbox unavailable; using default route"}
	case looksLikeDocument(text):
		return ProfileDecision{Profile: ProfileDocument, Reason: "matched document/file-generation cues"}
	case looksLikeMemory(text):
		return ProfileDecision{Profile: ProfileMemory, Reason: "matched memory/source/wiki cues"}
	default:
		return ProfileDecision{Profile: ProfileDefault, Reason: "no specialized profile cues matched"}
	}
}

func ToolsForProfile(profile Profile, available Availability) ([]string, error) {
	profile = normalizeProfile(string(profile))
	cards := ProfileCards()
	card, ok := cards[profile]
	if !ok {
		return nil, fmt.Errorf("unknown orchestration profile %q", profile)
	}
	if missing := missingAvailability(profile, available); missing != "" {
		return nil, fmt.Errorf("profile %q unavailable: missing %s", profile, missing)
	}
	tools := append([]string(nil), card.AllowedTools...)
	switch profile {
	case ProfileMemory:
		if available.Proposals {
			tools = append(tools, "propose_wiki_change", "propose_skill_change")
		}
	case ProfileSwarmResearch:
		if available.Swarm {
			tools = append([]string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}, tools...)
		}
	case ProfileDocument:
		if available.Swarm {
			tools = append(tools, "run_aurabot_swarm", "read_swarm_result")
		}
	}
	return tools, nil
}

func ProfileCards() map[Profile]ProfileCard {
	return map[Profile]ProfileCard{
		ProfileDefault: {
			Profile: ProfileDefault,
			Purpose: "Safe everyday route for simple answers, memory lookup, search, and routine task scheduling.",
			Access:  AccessDefault,
			AllowedTools: []string{
				"search_memory", "search_wiki", "read_wiki",
				"list_wiki", "list_sources", "read_source",
				"web_search", "web_fetch",
				"schedule_task", "list_tasks", "cancel_task",
				"daily_briefing",
			},
			DeniedTools: []string{"write_wiki", "execute_code", "run_aurabot_swarm", "install_skill", "delete_skill", "request_dashboard_token"},
		},
		ProfileMemory: {
			Profile:      ProfileMemory,
			Purpose:      "Read-heavy source/wiki/memory route with review-gated proposals.",
			Access:       AccessReadOnly,
			PositiveCues: []string{"memory", "memoria", "wiki", "sources", "fonti", "cosa sai"},
			AllowedTools: []string{
				"search_memory", "list_wiki", "read_wiki", "search_wiki",
				"list_sources", "read_source", "lint_wiki", "lint_sources",
				"daily_briefing",
			},
			DeniedTools: []string{"execute_code", "create_docx", "create_xlsx", "create_pdf", "schedule_task"},
		},
		ProfileSwarmResearch: {
			Profile:              ProfileSwarmResearch,
			Purpose:              "Read-only broad synthesis route for audits, planning, pipeline reviews, and quality checks.",
			Access:               AccessReadOnly,
			PositiveCues:         []string{"facciamo il punto", "pipeline", "what is missing", "audit", "roadmap"},
			RequiredAvailability: []string{"swarm"},
			AllowedTools:         []string{},
			DeniedTools: []string{
				"write_wiki", "create_docx", "create_xlsx", "create_pdf",
				"execute_code", "schedule_task", "cancel_task", "run_task_now",
				"install_skill", "delete_skill", "settings_update",
			},
		},
		ProfileSandboxCompute: {
			Profile:              ProfileSandboxCompute,
			Purpose:              "Compute and artifact route for Python calculations, transformations, charts, simulations, and parser experiments.",
			Access:               AccessSandbox,
			PositiveCues:         []string{"calculate", "calcola", "chart", "grafico", "csv", "parser", "simulation"},
			RequiredAvailability: []string{"sandbox"},
			AllowedTools: []string{
				"execute_code", "list_tools", "read_tool",
				"list_skills", "read_skill",
				"search_memory", "list_sources", "read_source", "store_source",
			},
			DeniedTools: []string{"write_wiki", "schedule_task", "install_skill", "delete_skill", "settings_update"},
		},
		ProfileDocument: {
			Profile:      ProfileDocument,
			Purpose:      "Skill-first route for DOCX/XLSX/PDF/report generation with memory/source evidence.",
			Access:       AccessWrite,
			PositiveCues: []string{"docx", "pdf", "xlsx", "report", "relazione", "documento"},
			AllowedTools: []string{
				"list_skills", "read_skill", "search_skill_catalog",
				"search_memory", "list_wiki", "read_wiki", "search_wiki",
				"list_sources", "read_source",
				"create_docx", "create_xlsx", "create_pdf",
			},
			DeniedTools: []string{"install_skill", "delete_skill", "settings_update"},
		},
		ProfileAdminReview: {
			Profile:      ProfileAdminReview,
			Purpose:      "Review-only route for proposals and admin queues without silent mutation.",
			Access:       AccessReviewOnly,
			PositiveCues: []string{"review queue", "approval", "proposals", "admin"},
			AllowedTools: []string{
				"daily_briefing", "list_tasks",
				"propose_wiki_change", "propose_skill_change",
				"list_skills", "read_skill", "search_skill_catalog",
			},
			DeniedTools: []string{"write_wiki", "execute_code", "run_task_now", "install_skill", "delete_skill", "settings_update"},
		},
	}
}

func missingAvailability(profile Profile, available Availability) string {
	card, ok := ProfileCards()[normalizeProfile(string(profile))]
	if !ok {
		return ""
	}
	for _, req := range card.RequiredAvailability {
		switch req {
		case "swarm":
			if !available.Swarm {
				return req
			}
		case "sandbox":
			if !available.Sandbox {
				return req
			}
		case "proposals":
			if !available.Proposals {
				return req
			}
		}
	}
	return ""
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
		return "\nUse the swarm-first read-only route for broad synthesis, audits, planning, memory quality checks, and pipeline reviews. Direct wiki/source/memory tools are intentionally hidden in this profile so the parent agent does not duplicate worker reads. Do not write or create files in this profile."
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
	if profile == ProfileMemory || profile == ProfileDocument {
		return "\n\n## Memory Route\nFor broad or source-backed answers, gather evidence with search_memory or read-only source/wiki tools before synthesizing. Keep citations compact when the user asks for proof."
	}
	return ""
}

func swarmProfilePrompt(profile Profile) string {
	if profile == ProfileSwarmResearch {
		return "\n\n## Swarm Profile\nCall run_aurabot_swarm as the first and primary tool for this profile. Keep the goal compact and read-only. After the swarm returns, answer from its synthesis; call read_swarm_result only when a returned task is incomplete or the synthesis explicitly says a worker detail is needed."
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

func profileSupportsSkillTools(profile Profile) bool {
	switch profile {
	case ProfileSandboxCompute, ProfileDocument, ProfileAdminReview:
		return true
	default:
		return false
	}
}
