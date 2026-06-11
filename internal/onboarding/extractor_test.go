package onboarding

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/profile"
)

func TestExtractDraftIncludesFactsAndPreferences(t *testing.T) {
	voice := true
	draft, err := ExtractDraft(Answers{
		Name:           "Davide",
		Lang:           "it",
		Timezone:       "Europe/Rome",
		TonePreference: "technical",
		ResponseLength: "short",
		VoiceMode:      &voice,
	})
	if err != nil {
		t.Fatalf("ExtractDraft: %v", err)
	}
	for _, want := range []string{
		"Name: Davide",
		"Prefer Italian responses",
		"Timezone: Europe/Rome",
		"Tone: technical",
		"Response length: short",
		"Voice mode: true",
	} {
		if !strings.Contains(draft.AgentMD, want) {
			t.Fatalf("Agent.md missing %q:\n%s", want, draft.AgentMD)
		}
	}
	if draft.Preferences.Lang != "it" {
		t.Fatalf("Lang = %q, want it", draft.Preferences.Lang)
	}
	if draft.Preferences.Timezone != "Europe/Rome" {
		t.Fatalf("Timezone = %q, want Europe/Rome", draft.Preferences.Timezone)
	}
	if draft.Preferences.ResponseLength != "short" {
		t.Fatalf("ResponseLength = %q, want short", draft.Preferences.ResponseLength)
	}
	if !draft.Preferences.VoiceMode {
		t.Fatal("VoiceMode = false, want true")
	}
	if !strings.Contains(draft.PreferencesJSON, `"voice_mode":true`) {
		t.Fatalf("PreferencesJSON missing voice_mode: %s", draft.PreferencesJSON)
	}
}

func TestExtractDraftBoundsOutput(t *testing.T) {
	draft, err := ExtractDraft(Answers{
		Name:               "Davide",
		CustomInstructions: strings.Repeat("custom ", profile.MaxAgentMDBytes),
	})
	if err != nil {
		t.Fatalf("ExtractDraft: %v", err)
	}
	if got := len([]byte(draft.AgentMD)); got > profile.MaxAgentMDBytes {
		t.Fatalf("Agent.md bytes = %d, want <= %d", got, profile.MaxAgentMDBytes)
	}
}
