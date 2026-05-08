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

	ToolsetModeAuto = "auto"
	ProfileModeAuto = ToolsetModeAuto

	ToolsetDefault  Toolset = "default"
	ToolsetCompute  Toolset = "compute"
	ToolsetDocument Toolset = "document"
	ToolsetAdmin    Toolset = "admin"

	ProfileDefault  Profile = Profile(ToolsetDefault)
	ProfileCompute  Profile = Profile(ToolsetCompute)
	ProfileDocument Profile = Profile(ToolsetDocument)
	ProfileAdmin    Profile = Profile(ToolsetAdmin)

	// Legacy profile names are normalized to the four toolsets above at every
	// boundary so persisted AURA_TOOL_PROFILE_MODE values keep working.
	ProfileMemory         Profile = "memory"
	ProfileSwarmResearch  Profile = "swarm_research"
	ProfileSandboxCompute Profile = "sandbox_compute"
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

type Toolset string

type AccessLevel string

const (
	AccessDefault    AccessLevel = "default"
	AccessReadOnly   AccessLevel = "read_only"
	AccessWrite      AccessLevel = "write"
	AccessReviewOnly AccessLevel = "review_only"
	AccessSandbox    AccessLevel = "sandbox"
)

type Availability struct {
	Swarm          bool
	Sandbox        bool
	Proposals      bool
	WorkspaceFiles bool
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

	content += fmt.Sprintf("\n\n## Aura Orchestration\n- Prompt Version: %s\n- Toolset: %s\n", version, profile)
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
		Purpose:  "Default toolset for chat, memory, workspace files, sources, search, scheduling, and bounded swarm research when available.",
		Access:   AccessDefault,
		Priority: 100,
		AllowedTools: []string{
			"search_memory",
			"list_sources", "read_source", "store_source",
			"web_search", "web_fetch",
			"schedule_task", "list_tasks", "cancel_task",
			"daily_briefing",
		},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "workspace_files", Tools: []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"}, Prepend: true},
			{Availability: "swarm", Tools: []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}},
		},
		DeniedTools: []string{"execute_code", "create_docx", "create_xlsx", "create_pdf", "install_skill", "delete_skill", "request_dashboard_token", "settings_update", "run_task_now"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                8,
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate tool calls with identical arguments; keep default turns short and evidence-backed.",
			MaxElapsed:              30 * time.Second,
		},
	},
	{
		Profile:              ProfileCompute,
		Purpose:              "Compute toolset for Python calculations, transformations, charts, simulations, parser experiments, and generated artifacts.",
		Access:               AccessSandbox,
		Priority:             20,
		PositiveCues:         []string{"calculate", "calcola", "compute", "grafico", "chart", "plot", "csv", "dataframe", "dataset", "simulation", "simulazione", "python", "parser", "script", "debug script", "artifact", "artifacts", "trasforma", "transform", "analisi dati", "data analysis"},
		NegativeCues:         []string{"documento word", "word modificabile", "docx", "pdf", "relazione"},
		RequiredAvailability: []string{"sandbox"},
		AvailabilityFallback: ProfileDefault,
		AllowedTools: []string{
			"search_memory",
			"list_sources", "read_source", "store_source",
			"web_search", "web_fetch",
			"execute_code", "list_tools", "read_tool",
		},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "workspace_files", Tools: []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"}, Prepend: true},
		},
		DeniedTools: []string{"install_skill", "delete_skill", "settings_update", "request_dashboard_token"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                4,
			TerminalTools:           []string{"execute_code"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject repeated execute_code calls that recompute the same artifact; allow one final no-tool response after execute_code.",
			MaxElapsed:              30 * time.Second,
		},
	},
	{
		Profile:      ProfileDocument,
		Purpose:      "Document toolset for DOCX/XLSX/PDF/report generation with memory/source evidence.",
		Access:       AccessWrite,
		Priority:     30,
		PositiveCues: []string{"documento word", "word modificabile", "docx", "pdf", "xlsx", "spreadsheet", "foglio", "presentami un documento", "crea un documento", "genera un documento", "report", "relazione", "documento", "documenti"},
		NegativeCues: []string{"calcola", "calculate", "compute", "grafico", "chart", "plot", "csv"},
		AllowedTools: []string{
			"search_memory",
			"list_sources", "read_source",
			"web_search", "web_fetch",
			"create_docx", "create_xlsx", "create_pdf",
		},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "workspace_files", Tools: []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"}, Prepend: true},
			{Availability: "swarm", Tools: []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}},
		},
		DeniedTools: []string{"install_skill", "delete_skill", "settings_update", "request_dashboard_token"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                6,
			TerminalTools:           []string{"create_docx", "create_xlsx", "create_pdf"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate file generation calls for the same target format unless revising a failed artifact.",
			MaxElapsed:              45 * time.Second,
		},
	},
	{
		Profile:      ProfileAdmin,
		Purpose:      "Admin toolset for explicit dashboard, settings, skill, token, task, and review-queue work; concrete tools still enforce auth/admin gates.",
		Access:       AccessReviewOnly,
		Priority:     10,
		PositiveCues: []string{"review queue", "approval", "proposals", "admin", "dashboard", "settings", "impostazioni", "request_dashboard_token", "coda review", "coda approvazioni", "docker release", "plugin", "mcp"},
		AllowedTools: []string{
			"search_memory",
			"list_sources", "read_source",
			"web_search", "web_fetch",
			"daily_briefing", "list_tasks", "schedule_task", "cancel_task", "run_task_now",
			"request_dashboard_token", "install_skill", "delete_skill", "settings_update",
		},
		ConditionalTools: []ConditionalToolSet{
			{Availability: "workspace_files", Tools: []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"}, Prepend: true},
			{Availability: "swarm", Tools: []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}},
		},
		DeniedTools: []string{"execute_code", "create_docx", "create_xlsx", "create_pdf"},
		LoopPolicy: LoopPolicy{
			MaxSteps:                6,
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate admin calls that request the same review or mutation action.",
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
	case "workspace_files":
		return available.WorkspaceFiles
	default:
		return false
	}
}

func normalizeProfile(value string) Profile {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ToolsetCompute), string(ProfileSandboxCompute):
		return ProfileCompute
	case string(ToolsetDocument):
		return ProfileDocument
	case string(ToolsetAdmin), string(ProfileAdminReview):
		return ProfileAdmin
	case string(ToolsetDefault), string(ProfileMemory), string(ProfileSwarmResearch):
		return ProfileDefault
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
	case ProfileCompute:
		return "\nUse Python sandbox for computation, data transforms, charts, simulations, parser experiments, and generated artifacts. Inspect applicable skill files only when they materially improve the work. Keep generated files under /tmp/aura_out."
	case ProfileDocument:
		return "\nUse memory/source evidence first, optionally inspect relevant skill files, then typed file tools for ordinary static documents."
	case ProfileAdmin:
		return "\nUse admin tools only for explicit dashboard, settings, token, skill, MCP, task, or review-queue work. Concrete tools still enforce auth/admin gates. Do not silently mutate durable state when a proposal/review path is more appropriate."
	default:
		return "\nUse the default broad-safe toolset. Prefer memory, workspace file, source, search, and web tools for simple work; use one swarm pass for broad audits when it is exposed."
	}
}

func memoryPrompt(profile Profile) string {
	if profile == ProfileDefault || profile == ProfileDocument {
		return "\n\n## Memory Route\nFor broad or source-backed answers, gather evidence with search_memory, source tools, or workspace file reads before synthesizing. Keep citations compact when the user asks for proof."
	}
	return ""
}

func swarmProfilePrompt(profile Profile) string {
	if profile == ProfileDocument {
		return "\n\n## Document Evidence Profile\nFor document summaries, gather compact evidence with search_memory and list_sources, then create the requested file with the typed file tool. Keep source/wiki reads narrow; do not keep expanding the evidence loop once you have enough to draft."
	}
	if profile == ProfileDefault {
		return "\n\n## Default Swarm Route\nWhen the request spans the whole pipeline, repo, logs, wiki, memory quality, stale references, or asks what is missing, use one run_aurabot_swarm call when it is exposed. Treat the swarm result as a broad evidence pass; use direct reads afterward only for narrow verification."
	}
	return ""
}

func sandboxPrompt(profile Profile) string {
	if profile == ProfileCompute {
		return "\n\n## Python Sandbox Route\nUse execute_code for calculations, transformations, charts, generated artifacts, parser experiments, and repeatable debug scripts. If a sandbox/coding skill is relevant, inspect its SKILL.md with workspace file tools first. Write deliverable files under /tmp/aura_out so Aura persists and can deliver them."
	}
	return "\n\n## Python Sandbox Route\nUse execute_code only when the request genuinely needs computation, data processing, custom generated files, plots, simulations, or parser/debug scripts."
}

func filePrompt(profile Profile) string {
	if profile != ProfileDocument {
		return ""
	}
	return "\n\n## File Generation Route\nBefore using typed document/file tools, inspect relevant installed skill files only when they materially improve the result. Use create_docx/create_xlsx/create_pdf for ordinary static user-facing files; use execute_code only for computed artifacts."
}

func skillPreflightPrompt(profile Profile) string {
	if profile == ProfileDocument || profile == ProfileCompute {
		return "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it, when you are unsure about a domain-specific workflow, or when a previous tool error suggests one. Do not read skills just to satisfy a ritual; if the next tool is obvious, use it and keep working."
	}
	return "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it or when it would materially improve the work. Do not read skills on every turn."
}

func profileSupportsSkillTools(profile Profile) bool {
	return profile == ProfileDefault || profile == ProfileCompute || profile == ProfileDocument || profile == ProfileAdmin
}
