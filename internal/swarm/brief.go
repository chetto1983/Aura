package swarm

import "strings"

// workerOverlay is the D-06 static worker framing. It rides in the FIRST user
// message (messages[1]) of every worker rather than mutating messages[0]: the
// system prompt stays byte-identical across all workers (KV-cache invariant,
// D-06), and the worker-role framing travels in the brief. Load-bearing literal —
// a test asserts the brief carries it.
const workerOverlay = "You are a headless swarm worker. You cannot see the conversation, the user, " +
	"or your sibling workers; you only see this brief. Work autonomously and produce a single, " +
	"self-contained final report. The parent agent reads ONLY your final text answer — anything you " +
	"do not state in the final answer is lost."

// The four D-07 section markers. They are load-bearing literals — structuredBrief
// emits each one verbatim and a test asserts all four are present (the
// finalizeNudge substring-assert idiom).
const (
	briefObjective  = "## Objective"
	briefOutput     = "## Output format"
	briefTools      = "## Tool guidance"
	briefBoundaries = "## Task boundaries"
)

// structuredBrief builds the deterministic D-07 4-part worker brief for one goal,
// prefixed by the D-06 worker overlay. The byte content is a pure function of goal,
// so two workers handed the same goal get the same brief (KV-cache friendly). It is
// returned as the body of a single RoleUser message (messages[1]); messages[0] (the
// system prompt) is appended unchanged by agent.NewLlmAgent.
func structuredBrief(goal string) string {
	var b strings.Builder
	b.WriteString(workerOverlay)
	b.WriteString("\n\n")
	b.WriteString(briefObjective)
	b.WriteString("\n")
	b.WriteString(goal)
	b.WriteString("\n\n")
	b.WriteString(briefOutput)
	b.WriteString("\nAnswer with a concise, self-contained final report addressing the objective. " +
		"State every fact you found; do not refer to context you cannot include here.\n\n")
	b.WriteString(briefTools)
	b.WriteString("\nUse the tools available to you to gather what the objective needs. " +
		"Call text_response with your final report when you are done. " +
		"You may not spawn further workers.\n\n")
	b.WriteString(briefBoundaries)
	b.WriteString("\nStay strictly within this objective. Do not attempt sibling tasks, " +
		"do not ask the user unless the objective genuinely cannot proceed without a decision.\n")
	return b.String()
}
