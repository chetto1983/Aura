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

func hasPref(pm ProfileMemory, cat, pref string) bool {
	for _, p := range pm.Preferences {
		if p.Category == cat && p.Preference == pref {
			return true
		}
	}
	return false
}
