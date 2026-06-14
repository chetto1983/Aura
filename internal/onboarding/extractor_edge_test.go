package onboarding

import (
	"strings"
	"testing"
)

func TestExtractDraftEmptyAnswersOmitsFacts(t *testing.T) {
	draft, err := ExtractDraft(Answers{})
	if err != nil {
		t.Fatalf("ExtractDraft: %v", err)
	}
	if strings.Contains(draft.AgentMD, "Name:") {
		t.Fatalf("empty answers should not render a Name fact:\n%s", draft.AgentMD)
	}
	if draft.Preferences.Lang != "" || draft.Preferences.Timezone != "" {
		t.Fatalf("empty answers should yield zero-value preferences, got %+v", draft.Preferences)
	}
	// Every Preferences field is omitempty, so a zero value marshals to "{}".
	if draft.PreferencesJSON != "{}" {
		t.Fatalf("PreferencesJSON for empty answers = %q, want {}", draft.PreferencesJSON)
	}
}

func TestExtractDraftEnglishAndProactivePreferences(t *testing.T) {
	proactive := true
	draft, err := ExtractDraft(Answers{
		Name:                "Sam",
		Lang:                "English",
		CanProactiveMessage: &proactive,
	})
	if err != nil {
		t.Fatalf("ExtractDraft: %v", err)
	}
	if !strings.Contains(draft.AgentMD, "Prefer English responses") {
		t.Fatalf("English language preference missing:\n%s", draft.AgentMD)
	}
	if !strings.Contains(draft.AgentMD, "Can proactive message: true") {
		t.Fatalf("CanProactiveMessage line missing:\n%s", draft.AgentMD)
	}
	if !draft.Preferences.CanProactiveMessage {
		t.Fatal("Preferences.CanProactiveMessage = false, want true")
	}
}

func TestExtractDraftUnknownLanguageFallsThrough(t *testing.T) {
	draft, err := ExtractDraft(Answers{Name: "Sam", Lang: "Klingon"})
	if err != nil {
		t.Fatalf("ExtractDraft: %v", err)
	}
	if !strings.Contains(draft.AgentMD, "Language: Klingon") {
		t.Fatalf("unknown language should fall through to verbatim line:\n%s", draft.AgentMD)
	}
}

func TestLanguagePreferenceMapping(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "   ", want: ""},
		{name: "it code", in: "it", want: "Prefer Italian responses"},
		{name: "italian word", in: "Italiano", want: "Prefer Italian responses"},
		{name: "ita upper", in: "ITA", want: "Prefer Italian responses"},
		{name: "en code", in: "en", want: "Prefer English responses"},
		{name: "english word", in: "English", want: "Prefer English responses"},
		{name: "eng upper", in: " ENG ", want: "Prefer English responses"},
		{name: "verbatim fallthrough", in: "Deutsch", want: "Language: Deutsch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := languagePreference(tt.in); got != tt.want {
				t.Fatalf("languagePreference(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIdentityLinesEmptyName(t *testing.T) {
	if got := identityLines(Answers{}); got != nil {
		t.Fatalf("identityLines with empty answers = %v, want nil", got)
	}
	got := identityLines(Answers{Name: "Ada"})
	if len(got) != 1 || got[0] != "Name: Ada" {
		t.Fatalf("identityLines = %v, want [Name: Ada]", got)
	}
}

func TestStyleLinesVoiceModeFalseStillRenders(t *testing.T) {
	voice := false
	got := styleLines(Answers{VoiceMode: &voice})
	if len(got) != 1 || got[0] != "Voice mode: false" {
		t.Fatalf("styleLines = %v, want [Voice mode: false]", got)
	}
}

func TestCustomInstructionLines(t *testing.T) {
	if got := customInstructionLines(Answers{}); got != nil {
		t.Fatalf("empty custom instructions = %v, want nil", got)
	}
	got := customInstructionLines(Answers{CustomInstructions: "Always be terse."})
	if len(got) != 1 || got[0] != "Always be terse." {
		t.Fatalf("customInstructionLines = %v, want [Always be terse.]", got)
	}
}

func TestCleanFieldTruncatesOversizedInput(t *testing.T) {
	oversized := strings.Repeat("x", maxAnswerFieldBytes+500)
	got := cleanField(oversized)
	if len([]byte(got)) > maxAnswerFieldBytes {
		t.Fatalf("cleanField output = %d bytes, want <= %d", len([]byte(got)), maxAnswerFieldBytes)
	}
	if got := cleanField("  trimmed  "); got != "trimmed" {
		t.Fatalf("cleanField did not trim: %q", got)
	}
}

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{name: "non-positive limit", in: "hello", limit: 0, want: ""},
		{name: "negative limit", in: "hello", limit: -5, want: ""},
		{name: "under limit unchanged", in: "hi", limit: 10, want: "hi"},
		{name: "exact limit unchanged", in: "hello", limit: 5, want: "hello"},
		{name: "truncated and trimmed", in: "abcdef", limit: 3, want: "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateBytes(tt.in, tt.limit); got != tt.want {
				t.Fatalf("truncateBytes(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}

func TestTruncateBytesRespectsRuneBoundaries(t *testing.T) {
	// Multibyte runes (each "à" = 2 bytes); a 3-byte limit must not split a rune.
	got := truncateBytes("ààà", 3)
	if len([]byte(got)) > 3 {
		t.Fatalf("truncateBytes split past limit: %q (%d bytes)", got, len([]byte(got)))
	}
	if got != "à" {
		t.Fatalf("truncateBytes(ààà, 3) = %q, want à", got)
	}
}
