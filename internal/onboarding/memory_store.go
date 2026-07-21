package onboarding

import (
	"context"
	"strings"
)

// Sentinel-fact predicates that replace the on-disk Metadata.Onboarding{Completed,
// Skipped} flags (Amendment #87). They are written with subject = the identity UUID so
// the onboarding-status read is a single memory_get_facts(subject=identityID) scan that
// never collides with the operator's profile facts (whose subject is their name).
const (
	PredicateOnboardingCompleted = "onboarding_completed"
	PredicateOnboardingSkipped   = "onboarding_skipped"
)

// Controlled preference categories (Amendment #87 fixed vocabulary — the live graph
// showed free-form category drift, e.g. communication_style vs comunicazione).
const (
	catCommunicationStyle = "communication_style"
	catSystem             = "system"
)

// OnboardingState is the 3-state onboarding gate read back from the memory sentinel.
type OnboardingState struct {
	Completed bool
	Skipped   bool
}

// ProfileMemoryStore persists the confirmed/skipped onboarding profile into the
// agent-memory graph and reads the completion sentinel back. It is the Amendment #87
// replacement for the on-disk profile.Store: the concrete implementation (cmd/aura)
// drives the memory MCP and MUST scope every call to the identity. Both the web (agui)
// and Telegram onboarding paths depend on this one port.
type ProfileMemoryStore interface {
	StoreConfirmed(ctx context.Context, identityID string, a Answers, rawDraft string) error
	StoreSkipped(ctx context.Context, identityID string) error
	Status(ctx context.Context, identityID string) (OnboardingState, error)
}

// MemoryEntity / MemoryFact / MemoryPreference are the tool-agnostic writes the mapper
// emits; the cmd/aura adapter turns each into a memory_add_* call.
type MemoryEntity struct {
	Name        string
	EntityType  string
	Description string
	Aliases     []string
}

// MemoryFact is one subject-predicate-object declarative fact.
type MemoryFact struct {
	Subject     string
	Predicate   string
	ObjectValue string
}

// MemoryPreference is one categorized operator preference.
type MemoryPreference struct {
	Category   string
	Preference string
	Context    string
}

// ProfileMemory is the deterministic memory projection of a confirmed interview:
// entities first (so facts resolve them by name), then facts, then preferences —
// mirroring the shape a real agent emits organically (conv 03b9c7c2).
type ProfileMemory struct {
	Entities    []MemoryEntity
	Facts       []MemoryFact
	Preferences []MemoryPreference
}

// MapProfile deterministically projects confirmed interview Answers into memory writes.
// It is pure (no I/O) so it is unit-tested without the memory sidecar; the interview
// already ran the per-step LLM extraction, so no further LLM call is needed
// (Amendment #87). Empty fields produce no write.
func MapProfile(a Answers) ProfileMemory {
	subject := nz(a.Name)
	if subject == "" {
		subject = "operator"
	}
	var pm ProfileMemory

	if nz(a.Name) != "" {
		pm.Entities = append(pm.Entities, MemoryEntity{
			Name:        nz(a.Name),
			EntityType:  "PERSON",
			Description: joinNonEmpty(", ", a.Role, a.Company),
			Aliases:     []string{nz(a.Name)},
		})
	}
	if nz(a.Company) != "" {
		pm.Entities = append(pm.Entities, MemoryEntity{Name: nz(a.Company), EntityType: "ORGANIZATION"})
	}
	if nz(a.Location) != "" {
		pm.Entities = append(pm.Entities, MemoryEntity{Name: nz(a.Location), EntityType: "LOCATION"})
	}

	addFact := func(pred, obj string) {
		if nz(obj) != "" {
			pm.Facts = append(pm.Facts, MemoryFact{Subject: subject, Predicate: pred, ObjectValue: nz(obj)})
		}
	}
	addFact("role", a.Role)
	addFact("works_for", a.Company)
	addFact("located_in", a.Location)
	addFact("timezone", a.Timezone)
	addFact("expertise", strings.Join(a.Expertise, ", "))
	addFact("stack", strings.Join(a.Stack, ", "))
	addFact("projects", strings.Join(a.Projects, ", "))
	addFact("goals", strings.Join(a.Goals, ", "))
	addFact("interests", strings.Join(a.Interests, ", "))
	for _, p := range a.People {
		addFact("knows", p)
	}

	addPref := func(cat, pref, ctx string) {
		if nz(pref) != "" {
			pm.Preferences = append(pm.Preferences, MemoryPreference{Category: cat, Preference: pref, Context: ctx})
		}
	}
	if nz(a.Lang) != "" {
		addPref(catCommunicationStyle, "Preferred language: "+nz(a.Lang), "")
	}
	if nz(a.TonePreference) != "" {
		addPref(catCommunicationStyle, "Tone: "+nz(a.TonePreference), "")
	}
	if nz(a.ResponseLength) != "" {
		addPref(catCommunicationStyle, "Response length: "+nz(a.ResponseLength), "")
	}
	if nz(a.CustomInstructions) != "" {
		addPref(catSystem, nz(a.CustomInstructions), "operator custom instructions")
	}
	for _, v := range a.Vetoes {
		addPref(catSystem, "Never: "+nz(v), "")
	}
	if a.VoiceMode != nil {
		addPref(catSystem, "Voice mode: "+onOff(*a.VoiceMode), "")
	}
	if a.CanProactiveMessage != nil {
		addPref(catSystem, "Proactive messaging: "+onOff(*a.CanProactiveMessage), "")
	}
	return pm
}

func nz(s string) string { return strings.TrimSpace(s) }

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if nz(p) != "" {
			kept = append(kept, nz(p))
		}
	}
	return strings.Join(kept, sep)
}
