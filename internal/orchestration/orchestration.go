package orchestration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
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
	Priority             int
	PositiveCues         []string
	NegativeCues         []string
	RequiredAvailability []string
	AvailabilityFallback Profile
	AllowedTools         []string
	ConditionalTools     []ConditionalToolSet
	DeniedTools          []string
	LoopPolicy           LoopPolicy
}

type ConditionalToolSet struct {
	Availability string
	Tools        []string
	Prepend      bool
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
	for _, card := range profileCardsByPriority() {
		if card.Profile == ProfileDefault || !card.matches(text) {
			continue
		}
		if missing := missingAvailabilityForCard(card, available); missing != "" {
			fallback := normalizeProfile(string(card.AvailabilityFallback))
			return ProfileDecision{
				Profile: fallback,
				Reason:  fmt.Sprintf("%s cues matched but %s unavailable; using %s route", card.Profile, missing, fallback),
			}
		}
		return ProfileDecision{
			Profile: card.Profile,
			Reason:  fmt.Sprintf("matched %s profile card cues", card.Profile),
		}
	}
	return ProfileDecision{Profile: ProfileDefault, Reason: "no specialized profile card cues matched"}
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
	for _, set := range card.ConditionalTools {
		if !availabilityEnabled(set.Availability, available) {
			continue
		}
		if set.Prepend {
			tools = append(append([]string(nil), set.Tools...), tools...)
			continue
		}
		tools = append(tools, set.Tools...)
	}
	return tools, nil
}

func ProfileCards() map[Profile]ProfileCard {
	cards := make(map[Profile]ProfileCard, len(profileCardCatalog))
	for _, card := range profileCardCatalog {
		cards[card.Profile] = cloneProfileCard(card)
	}
	return cards
}

func ProfileCardFor(profile Profile) (ProfileCard, bool) {
	for _, card := range profileCardCatalog {
		if card.Profile == normalizeProfile(string(profile)) {
			return cloneProfileCard(card), true
		}
	}
	return ProfileCard{}, false
}

var profileCardCatalog = []ProfileCard{
	{
		Profile:  ProfileDefault,
		Purpose:  "Safe everyday route for simple answers, memory lookup, search, and routine task scheduling.",
		Access:   AccessDefault,
		Priority: 100,
		AllowedTools: []string{
			"list_skills", "read_skill", "search_skill_catalog",
			"search_memory", "search_wiki", "read_wiki",
			"list_wiki", "list_sources", "read_source",
			"web_search", "web_fetch",
			"schedule_task", "list_tasks", "cancel_task",
			"daily_briefing",
			"write_wiki",
		},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "proposals", Tools: []string{"propose_wiki_change", "propose_skill_change"}},
		},
		DeniedTools: []string{"execute_code", "run_aurabot_swarm", "install_skill", "delete_skill", "request_dashboard_token"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                8,
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate high-risk or mutation tool calls; keep default turns short and conservative.",
			MaxElapsed:              30 * time.Second,
		},
	},
	{
		Profile:      ProfileMemory,
		Purpose:      "Source/wiki/memory route with optional swarm evidence, direct durable memory writes, and review-gated proposals.",
		Access:       AccessWrite,
		Priority:     50,
		PositiveCues: []string{"memory", "memoria", "wiki", "sources", "fonti", "cosa sai", "ricordi", "remember", "second brain", "source", "ricordati", "ricorda", "salva", "save", "record", "annota"},
		NegativeCues: []string{"documento", "docx", "pdf", "xlsx", "grafico", "chart", "csv", "pipeline audit"},
		AllowedTools: []string{
			"list_skills", "read_skill", "search_skill_catalog",
			"search_memory", "list_wiki", "read_wiki", "search_wiki",
			"list_sources", "read_source", "lint_wiki", "lint_sources",
			"daily_briefing", "write_wiki",
		},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "swarm", Tools: []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}, Prepend: true},
			{Availability: "proposals", Tools: []string{"propose_wiki_change", "propose_skill_change"}},
		},
		DeniedTools: []string{"execute_code", "create_docx", "create_xlsx", "create_pdf", "schedule_task"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                8,
			TerminalTools:           []string{"write_wiki"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate memory writes unless the later call targets distinct durable content.",
			MaxElapsed:              30 * time.Second,
		},
	},
	{
		Profile:  ProfileSwarmResearch,
		Purpose:  "Read-only broad synthesis route for audits, planning, pipeline reviews, and quality checks.",
		Access:   AccessReadOnly,
		Priority: 40,
		PositiveCues: []string{
			"facciamo il punto", "pipeline", "tutta la pipeline", "tutta la memoria", "tutto il repo",
			"cosa manca", "what is missing", "audit", "review all", "deep review", "mappa", "roadmap", "quality",
			"qualita", "consolidation", "consolidamento", "analyze all memory", "analyse all memory",
			"review all memory", "audit all memory", "map all memory", "analyze the wiki",
			"analyse the wiki", "review the wiki", "across the knowledge base", "whole knowledge base",
			"entire knowledge base", "cross-check", "synthesis", "sintet",
		},
		NegativeCues: []string{
			"dashboard", "settings", "impostazioni", "admin", "documento", "docx", "pdf", "xlsx",
			"csv", "grafico", "chart", "write_wiki", "scrivi", "scrivere", "salva", "ricorda",
			"remember", "save", "crea", "create", "genera", "generate", "installa", "install",
			"delete", "cancella", "rimuovi", "schedule", "program", "ricordami", "invia", "send",
			"memory capture", "post-turn", "auto_low_risk", "review-gated", "reviewable proposals",
		},
		RequiredAvailability: []string{"swarm"},
		AvailabilityFallback: ProfileMemory,
		AllowedTools:         []string{},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "swarm", Tools: []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}, Prepend: true},
		},
		DeniedTools: []string{
			"write_wiki", "create_docx", "create_xlsx", "create_pdf",
			"execute_code", "schedule_task", "cancel_task", "run_task_now",
			"install_skill", "delete_skill", "settings_update",
		},
		LoopPolicy: LoopPolicy{
			MaxSteps:                2,
			TerminalTools:           []string{"run_aurabot_swarm"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate run_aurabot_swarm calls after the first completed swarm delegation; answer from the aggregate result.",
			MaxElapsed:              30 * time.Second,
		},
	},
	{
		Profile:              ProfileSandboxCompute,
		Purpose:              "Compute and artifact route for Python calculations, transformations, charts, simulations, and parser experiments.",
		Access:               AccessSandbox,
		Priority:             20,
		PositiveCues:         []string{"calculate", "calcola", "compute", "grafico", "chart", "plot", "csv", "dataframe", "dataset", "simulation", "simulazione", "python", "parser", "script", "debug script", "artifact", "artifacts", "trasforma", "transform", "analisi dati", "data analysis"},
		NegativeCues:         []string{"documento word", "word modificabile", "docx", "pdf", "relazione"},
		RequiredAvailability: []string{"sandbox"},
		AvailabilityFallback: ProfileDefault,
		AllowedTools: []string{
			"list_skills", "read_skill",
			"execute_code", "list_tools", "read_tool",
			"search_memory", "list_sources", "read_source", "store_source",
		},
		DeniedTools: []string{"write_wiki", "schedule_task", "install_skill", "delete_skill", "settings_update"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                3,
			TerminalTools:           []string{"execute_code"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject repeated execute_code calls that recompute the same artifact; allow one final no-tool response after execute_code.",
			MaxElapsed:              30 * time.Second,
		},
	},
	{
		Profile:      ProfileDocument,
		Purpose:      "Skill-first route for DOCX/XLSX/PDF/report generation with memory/source evidence.",
		Access:       AccessWrite,
		Priority:     30,
		PositiveCues: []string{"documento word", "word modificabile", "docx", "pdf", "xlsx", "spreadsheet", "foglio", "presentami un documento", "crea un documento", "genera un documento", "report", "relazione", "documento", "documenti"},
		NegativeCues: []string{"calcola", "calculate", "compute", "grafico", "chart", "plot", "csv"},
		AllowedTools: []string{
			"list_skills", "read_skill", "search_skill_catalog",
			"search_memory", "list_sources",
			"create_docx", "create_xlsx", "create_pdf",
			"read_wiki", "read_source",
		},
		DeniedTools: []string{"install_skill", "delete_skill", "settings_update"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                6,
			TerminalTools:           []string{"create_docx", "create_xlsx", "create_pdf"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate file generation calls for the same target format unless revising a failed artifact.",
			MaxElapsed:              45 * time.Second,
		},
	},
	{
		Profile:      ProfileAdminReview,
		Purpose:      "Review-only route for proposals and admin queues without silent mutation.",
		Access:       AccessReviewOnly,
		Priority:     10,
		PositiveCues: []string{"review queue", "approval", "proposals", "admin", "dashboard", "settings", "impostazioni", "request_dashboard_token", "coda review", "coda approvazioni", "docker release", "plugin", "mcp"},
		AllowedTools: []string{
			"daily_briefing", "list_tasks",
			"propose_wiki_change", "propose_skill_change",
			"list_skills", "read_skill", "search_skill_catalog",
		},
		DeniedTools: []string{"write_wiki", "execute_code", "run_task_now", "install_skill", "delete_skill", "settings_update"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                4,
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate admin proposal calls that mutate or request the same review action.",
			MaxElapsed:              30 * time.Second,
		},
	},
}

func missingAvailability(profile Profile, available Availability) string {
	card, ok := ProfileCardFor(normalizeProfile(string(profile)))
	if !ok {
		return ""
	}
	return missingAvailabilityForCard(card, available)
}

func missingAvailabilityForCard(card ProfileCard, available Availability) string {
	for _, req := range card.RequiredAvailability {
		if !availabilityEnabled(req, available) {
			return req
		}
	}
	return ""
}

func availabilityEnabled(name string, available Availability) bool {
	switch name {
	case "swarm":
		return available.Swarm
	case "sandbox":
		return available.Sandbox
	case "proposals":
		return available.Proposals
	default:
		return false
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

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func (card ProfileCard) matches(text string) bool {
	if containsAny(text, card.NegativeCues) {
		return false
	}
	if containsAny(text, card.PositiveCues) {
		return true
	}
	return false
}

func cloneProfileCard(card ProfileCard) ProfileCard {
	card.PositiveCues = append([]string(nil), card.PositiveCues...)
	card.NegativeCues = append([]string(nil), card.NegativeCues...)
	card.RequiredAvailability = append([]string(nil), card.RequiredAvailability...)
	card.AllowedTools = append([]string(nil), card.AllowedTools...)
	card.ConditionalTools = cloneConditionalToolSets(card.ConditionalTools)
	card.DeniedTools = append([]string(nil), card.DeniedTools...)
	card.LoopPolicy = cloneLoopPolicy(card.LoopPolicy)
	return card
}

func cloneConditionalToolSets(sets []ConditionalToolSet) []ConditionalToolSet {
	out := make([]ConditionalToolSet, len(sets))
	for i, set := range sets {
		out[i] = set
		out[i].Tools = append([]string(nil), set.Tools...)
	}
	return out
}

func profileCardsByPriority() []ProfileCard {
	cards := make([]ProfileCard, len(profileCardCatalog))
	for i, card := range profileCardCatalog {
		cards[i] = cloneProfileCard(card)
	}
	sort.SliceStable(cards, func(i, j int) bool {
		return cards[i].Priority < cards[j].Priority
	})
	return cards
}

func profilePrompt(profile Profile) string {
	switch profile {
	case ProfileSwarmResearch:
		return "\nUse the swarm-first read-only route for broad synthesis, audits, planning, memory quality checks, and pipeline reviews. Direct wiki/source/memory tools are intentionally hidden in this profile so the parent agent does not duplicate worker reads. Do not write or create files in this profile."
	case ProfileSandboxCompute:
		return "\nUse Python sandbox for computation, data transforms, charts, simulations, parser experiments, and generated artifacts. Load applicable skill guidance before executing sandbox code. Keep generated files under /tmp/aura_out."
	case ProfileDocument:
		return "\nUse skills and memory/source evidence first, optionally swarm for broad synthesis, then typed file tools for ordinary static documents."
	case ProfileMemory:
		return "\nUse local memory/source/wiki tools for evidence-backed answers. For broad pipeline, audit, quality, or \"what is missing\" requests, call run_aurabot_swarm first when it is exposed, then use direct memory/write tools only for the final compact action. For facts, preferences, decisions, and ordinary remember/save requests in the current turn, answer normally and let automatic post-turn memory capture create the reviewable memory update. Use write_wiki only when an immediate wiki edit is clearly needed; after a successful write_wiki batch, stop calling tools and summarize."
	case ProfileAdminReview:
		return "\nUse review/proposal tools only. Do not silently mutate skills, MCP plugins, settings, files, or wiki pages. Never call write_wiki in this profile; for remember/save requests, answer normally and rely on automatic post-turn memory capture, or use propose_wiki_change when a review item is explicitly needed."
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
		return "\n\n## Document Evidence Profile\nFor document summaries, gather compact evidence with search_memory and list_sources, then create the requested file with the typed file tool. Keep source/wiki reads narrow; do not keep expanding the evidence loop once you have enough to draft."
	}
	if profile == ProfileMemory {
		return "\n\n## Memory Swarm Route\nWhen the request spans the whole pipeline, repo, wiki, memory quality, stale references, or asks what is missing, prefer one run_aurabot_swarm call before direct reads. Treat the swarm result as the broad evidence pass; do not duplicate its worker reads unless a narrow fact must be verified before a write."
	}
	return ""
}

func sandboxPrompt(profile Profile) string {
	if profile == ProfileSandboxCompute {
		return "\n\n## Python Sandbox Route\nFirst inspect the applicable sandbox/coding skill with list_skills and read_skill. Then use execute_code for calculations, transformations, charts, generated artifacts, parser experiments, and repeatable debug scripts. Write deliverable files under /tmp/aura_out so Aura persists and can deliver them."
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
		return "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it, when you are unsure about a domain-specific workflow, or when a previous tool error suggests one. Do not read skills just to satisfy a ritual; if the next tool is obvious, use it and keep working."
	}
	return "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it or when it would materially improve the work. Do not read skills on every turn."
}

func profileSupportsSkillTools(profile Profile) bool {
	switch profile {
	case ProfileSandboxCompute, ProfileDocument, ProfileAdminReview:
		return true
	default:
		return false
	}
}
