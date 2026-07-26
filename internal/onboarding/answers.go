// Package onboarding holds the deterministic profile seed: the structured Answers a
// first-run form collects, and the mapper that projects them onto agent-memory writes
// (Amendment #95 — the LLM interview that used to acquire these fields is removed; the
// operator types them, and the agent keeps them up to date from ordinary use).
package onboarding

// Answers contains structured profile facts. The seed form fills Name/Role/Company/
// Location/Lang/Timezone; the remaining fields stay zero-valued there and are filled by
// the agent's own memory writes or the profile editor (MapProfile emits no write for an
// empty field).
type Answers struct {
	Name      string
	Role      string
	Company   string
	Location  string
	Expertise []string
	Stack     []string
	Projects  []string
	Goals     []string
	Interests []string
	People    []string // e.g. "Andrea — business partner"
	Vetoes    []string // hard "never do" rules; set via direct profile edit

	// style/preferences
	Lang                string
	Timezone            string
	TonePreference      string
	ResponseLength      string
	VoiceMode           *bool
	CanProactiveMessage *bool
	CustomInstructions  string
}
