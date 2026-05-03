package conversation

import "strings"

const swarmRoutingPrompt = `## AuraBot Swarm Routing
When run_aurabot_swarm is available, use it as the fast read-only path for broad second-brain work.

- Prefer direct tools for one known wiki page, one source, one skill, one reminder, or any straightforward write/create action.
- Prefer run_aurabot_swarm for read-only goals that need multiple wiki/source/skill reads, cross-checking, audit, planning, or synthesis across the knowledge base.
- Treat run_aurabot_swarm as read-only. It cannot write wiki pages, mutate sources, install/delete skills, change settings, schedule tasks, or create files.
- Give it a compact goal in the user's language, usually with mode="wait". Use roles only when a smaller team is clearly enough.
- After it returns, answer from the synthesis and mention failed or incomplete worker tasks only if they affect the answer.

For mutation requests, gather evidence first when needed, then use the explicit write/admin tool only if the user clearly asked for that mutation.`

const swarmTurnHint = `## Suggested Tool Route
This turn looks like a broad read-only second-brain task. If you need tools, prefer one run_aurabot_swarm call before direct per-page reads. Keep the swarm goal read-only and do not mutate wiki, sources, skills, settings, tasks, or files unless the user explicitly asks for that mutation.`

// SwarmRoutingPrompt returns the stable routing instructions shown only when
// AuraBot swarm tools are actually registered for this bot instance.
func SwarmRoutingPrompt() string {
	return swarmRoutingPrompt
}

// SwarmTurnHint returns a per-turn hint for broad second-brain read tasks.
// It deliberately stays conservative: simple lookups and mutation-oriented
// requests keep using the direct tools instead of paying the swarm overhead.
func SwarmTurnHint(userText string) string {
	if LooksLikeSwarmReadGoal(userText) {
		return swarmTurnHint
	}
	return ""
}

// LooksLikeSwarmReadGoal detects prompts where parallel read-only workers are
// likely to save latency/context compared with sequential wiki/source/skill
// inspection. It is a routing hint only; the LLM still decides which tool to call.
func LooksLikeSwarmReadGoal(userText string) bool {
	text := normalizeRouteText(userText)
	if text == "" {
		return false
	}
	if containsAny(text, mutationRouteTerms) {
		return false
	}

	domains := countContains(text, swarmDomainTerms)
	broad := containsAny(text, broadReadTerms)
	scale := containsAny(text, scaleTerms)

	if domains >= 2 && (broad || scale) {
		return true
	}
	return domains >= 1 && broad && scale
}

func normalizeRouteText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(text))), " ")
}

func countContains(text string, terms []string) int {
	var count int
	for _, term := range terms {
		if strings.Contains(text, term) {
			count++
		}
	}
	return count
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

var swarmDomainTerms = []string{
	"wiki",
	"source",
	"sources",
	"fonti",
	"memoria",
	"memory",
	"second brain",
	"knowledge",
	"note",
	"skill",
	"skills",
}

var broadReadTerms = []string{
	"analizz",
	"audit",
	"review",
	"revision",
	"fattibil",
	"cosa manca",
	"manca",
	"cross-check",
	"confront",
	"colleg",
	"mappa",
	"sintet",
	"synthesis",
	"summar",
	"piano",
	"plan",
	"roadmap",
	"health",
}

var scaleTerms = []string{
	"tutto",
	"tutta",
	"tutte",
	"tutti",
	"intera",
	"intero",
	"globale",
	"complet",
	"profond",
	"larg",
	"multi",
	"paralle",
	"insieme",
	"across",
}

var mutationRouteTerms = []string{
	"write_wiki",
	"scrivi",
	"scrivere",
	"salva",
	"ricorda",
	"remember",
	"save",
	"crea",
	"create",
	"genera",
	"generate",
	"installa",
	"install",
	"delete",
	"cancella",
	"rimuovi",
	"schedule",
	"program",
	"ricordami",
	"invia",
	"send",
}
