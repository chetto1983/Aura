package onboarding

import "testing"

func TestMapProfileEntitiesFirstAndFields(t *testing.T) {
	t.Parallel()
	voice := true
	pm := MapProfile(Answers{
		Name:               "Davide",
		Role:               "Programmatore",
		Company:            "PmSync",
		Location:           "Caraglio",
		Expertise:          []string{"Go", "AI"},
		People:             []string{"Andrea — partner"},
		Lang:               "it",
		TonePreference:     "concise",
		ResponseLength:     "short",
		CustomInstructions: "always cite sources",
		Vetoes:             []string{"never delete prod"},
		VoiceMode:          &voice,
	})

	// entities-first ordering + types
	if len(pm.Entities) != 3 {
		t.Fatalf("entities = %d, want 3 (PERSON/ORG/LOCATION): %#v", len(pm.Entities), pm.Entities)
	}
	if pm.Entities[0].EntityType != "PERSON" || pm.Entities[0].Name != "Davide" {
		t.Errorf("entity[0] = %#v, want Davide/PERSON", pm.Entities[0])
	}
	if pm.Entities[0].Description != "Programmatore, PmSync" {
		t.Errorf("PERSON description = %q, want %q", pm.Entities[0].Description, "Programmatore, PmSync")
	}
	if pm.Entities[1].EntityType != "ORGANIZATION" || pm.Entities[2].EntityType != "LOCATION" {
		t.Errorf("entity types = %q/%q, want ORGANIZATION/LOCATION", pm.Entities[1].EntityType, pm.Entities[2].EntityType)
	}

	// facts subject = name
	facts := map[string]string{}
	for _, f := range pm.Facts {
		if f.Subject != "Davide" {
			t.Errorf("fact %q subject = %q, want Davide", f.Predicate, f.Subject)
		}
		facts[f.Predicate] = f.ObjectValue
	}
	if facts["works_for"] != "PmSync" || facts["role"] != "Programmatore" || facts["expertise"] != "Go, AI" || facts["knows"] != "Andrea — partner" {
		t.Errorf("facts mismatch: %#v", facts)
	}

	// controlled preference categories only
	for _, p := range pm.Preferences {
		if p.Category != catCommunicationStyle && p.Category != catSystem {
			t.Errorf("preference category %q not in controlled vocabulary", p.Category)
		}
	}
	if !hasPref(pm, catCommunicationStyle, "Preferred language: it") ||
		!hasPref(pm, catSystem, "always cite sources") ||
		!hasPref(pm, catSystem, "Never: never delete prod") ||
		!hasPref(pm, catSystem, "Voice mode: on") {
		t.Errorf("missing expected preferences: %#v", pm.Preferences)
	}
}

func TestMapProfileEmptyAndSubjectFallback(t *testing.T) {
	t.Parallel()
	pm := MapProfile(Answers{})
	if len(pm.Entities) != 0 || len(pm.Facts) != 0 || len(pm.Preferences) != 0 {
		t.Fatalf("empty answers produced writes: %#v", pm)
	}
	// no name -> facts fall back to the "operator" subject
	pm = MapProfile(Answers{Role: "dev"})
	if len(pm.Entities) != 0 {
		t.Errorf("no name should produce no PERSON entity: %#v", pm.Entities)
	}
	if len(pm.Facts) != 1 || pm.Facts[0].Subject != "operator" || pm.Facts[0].Predicate != "role" {
		t.Errorf("subject fallback = %#v, want operator/role", pm.Facts)
	}
}

// TestMapProfileSeedFormProjection pins the exact projection of the Amendment #95 seed
// form — the six typed fields and nothing else. It is the arithmetic behind "a full
// submission is nine tool calls": 3 entities + 4 facts + 1 preference, plus the sentinel
// the store stamps. It also proves the name survives byte-identically, which is the whole
// reason the LLM interview was removed.
func TestMapProfileSeedFormProjection(t *testing.T) {
	t.Parallel()
	pm := MapProfile(Answers{
		Name: "Davide", Lang: "it", Location: "Caraglio",
		Timezone: "Europe/Rome", Role: "founder", Company: "PmSync",
	})

	if len(pm.Entities) != 3 || len(pm.Facts) != 4 || len(pm.Preferences) != 1 {
		t.Fatalf("seed projection = %d entities / %d facts / %d preferences, want 3/4/1: %#v",
			len(pm.Entities), len(pm.Facts), len(pm.Preferences), pm)
	}
	if pm.Entities[0].Name != "Davide" || pm.Entities[0].EntityType != "PERSON" {
		t.Errorf("entity[0] = %#v, want the typed name as PERSON", pm.Entities[0])
	}
	if len(pm.Entities[0].Aliases) != 1 || pm.Entities[0].Aliases[0] != "Davide" {
		t.Errorf("PERSON aliases = %v, want the typed name verbatim", pm.Entities[0].Aliases)
	}
	if pm.Entities[1].Name != "PmSync" || pm.Entities[2].Name != "Caraglio" {
		t.Errorf("entities = %#v, want PmSync ORGANIZATION and Caraglio LOCATION", pm.Entities)
	}

	want := map[string]string{
		"role": "founder", "works_for": "PmSync",
		"located_in": "Caraglio", "timezone": "Europe/Rome",
	}
	for _, f := range pm.Facts {
		if f.Subject != "Davide" {
			t.Errorf("fact %q subject = %q, want the typed name byte-identical", f.Predicate, f.Subject)
		}
		if want[f.Predicate] != f.ObjectValue {
			t.Errorf("fact %q = %q, want %q", f.Predicate, f.ObjectValue, want[f.Predicate])
		}
		delete(want, f.Predicate)
	}
	if len(want) != 0 {
		t.Errorf("missing facts: %v", want)
	}
	if !hasPref(pm, catCommunicationStyle, "Preferred language: it") {
		t.Errorf("preferences = %#v, want the language preference", pm.Preferences)
	}
}

// TestMapProfileNamePreservedVerbatim pins the mapper's only normalization (surrounding
// whitespace) and proves it never case-folds, transliterates or truncates — the "David"
// for "Davide" defect must be unrepresentable on this path.
func TestMapProfileNamePreservedVerbatim(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Davide", "José-María", "Anne-Marie O'Brien", "李雷"} {
		pm := MapProfile(Answers{Name: name, Role: "dev"})
		if pm.Entities[0].Name != name {
			t.Errorf("entity name = %q, want byte-identical %q", pm.Entities[0].Name, name)
		}
		if pm.Facts[0].Subject != name {
			t.Errorf("fact subject = %q, want byte-identical %q", pm.Facts[0].Subject, name)
		}
	}
}

// TestMapProfileBooleanPreferencesOffArm covers onOff's `false` branch: a boolean the
// operator explicitly turned OFF must still produce a preference (a nil pointer means
// "unset" and produces none — the two are not the same).
func TestMapProfileBooleanPreferencesOffArm(t *testing.T) {
	t.Parallel()
	off := false
	pm := MapProfile(Answers{Name: "Davide", VoiceMode: &off, CanProactiveMessage: &off})
	if !hasPref(pm, catSystem, "Voice mode: off") || !hasPref(pm, catSystem, "Proactive messaging: off") {
		t.Fatalf("preferences = %#v, want both booleans recorded as off", pm.Preferences)
	}

	unset := MapProfile(Answers{Name: "Davide"})
	if len(unset.Preferences) != 0 {
		t.Errorf("nil booleans produced %#v, want no preference (unset != off)", unset.Preferences)
	}
}

func hasPref(pm ProfileMemory, cat, pref string) bool {
	for _, p := range pm.Preferences {
		if p.Category == cat && p.Preference == pref {
			return true
		}
	}
	return false
}
