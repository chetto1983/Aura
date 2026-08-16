package onboarding

import (
	"fmt"
	"strings"
)

// The deterministic profile as the model reads it: one block in messages[1], present on
// every turn, never retrieved.
//
// Retrieval was the old design and it fails in both directions. A profile fact competes for
// rank against real memories (a recall for "what did we decide about the invoices" scoring
// against "role: programmatore"), and a fact the agent must NEVER fail to know cannot
// depend on a similarity search returning it -- a veto the agent only sometimes remembers
// is not a veto.
//
// It rides messages[1] rather than the system prompt because it changes when the operator
// edits it, and messages[0] is the cached prefix every conversation shares.

// profileBlockTag wraps the block so the model can tell settings from conversation, and so
// a later reader can strip it deterministically.
const profileBlockTag = "operator_profile"

// RenderProfileBlock renders the answers into the always-block, or "" when there is nothing
// to say. Empty fields are omitted entirely: a line reading "Company: " teaches the model
// that the operator has no company, which is not what an unanswered form means.
func RenderProfileBlock(a Answers) string {
	var lines []string
	add := func(label, value string) {
		if v := strings.TrimSpace(value); v != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", label, v))
		}
	}
	addList := func(label string, values []string) {
		if joined := joinList(values); joined != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", label, joined))
		}
	}

	add("Name", a.Name)
	add("Role", a.Role)
	add("Company", a.Company)
	add("Location", a.Location)
	add("Timezone", a.Timezone)
	add("Language", a.Lang)
	addList("Expertise", a.Expertise)
	addList("Stack", a.Stack)
	addList("Projects", a.Projects)
	addList("Goals", a.Goals)
	addList("Interests", a.Interests)
	addList("People", a.People)
	add("Tone", a.TonePreference)
	add("Response length", a.ResponseLength)
	add("Custom instructions", a.CustomInstructions)

	// Vetoes go LAST and under their own heading. They are the only entries here that are
	// prohibitions rather than description, and burying them in a list of interests is how
	// a hard rule reads as a preference.
	if vetoes := joinList(a.Vetoes); vetoes != "" {
		lines = append(lines, "Never do: "+vetoes)
	}

	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("<%s>\n%s\n</%s>", profileBlockTag, strings.Join(lines, "\n"), profileBlockTag)
}

// joinList drops blanks so a list the operator half-filled does not render trailing
// separators the model then imitates. (memory_store.go has a variadic joinNonEmpty for a
// different shape -- parts of one sentence, not a list.)
func joinList(values []string) string {
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, ", ")
}
