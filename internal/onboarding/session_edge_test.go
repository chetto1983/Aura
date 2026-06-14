package onboarding

import (
	"errors"
	"strings"
	"testing"
)

func TestSessionTerminalRejectsNonRestartInput(t *testing.T) {
	s := NewSession("identity-1", "local")
	if _, err := s.Apply(Input{Intent: IntentSkip}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	if s.Status != StatusSkipped {
		t.Fatalf("status = %q, want %q", s.Status, StatusSkipped)
	}

	_, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}})
	if !errors.Is(err, ErrTerminal) {
		t.Fatalf("answer on terminal session error = %v, want ErrTerminal", err)
	}
}

func TestSessionRestartFromTerminalReopensInterview(t *testing.T) {
	s := NewSession("identity-9", "operator")
	if _, err := s.Apply(Input{Intent: IntentCancel}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if s.Status != StatusCanceled {
		t.Fatalf("status = %q, want %q", s.Status, StatusCanceled)
	}

	out, err := s.Apply(Input{Intent: IntentRestart})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if s.Status != StatusActive || s.Step != StepIdentity {
		t.Fatalf("after restart status/step = %q/%q, want active/identity", s.Status, s.Step)
	}
	if s.IdentityID != "identity-9" || s.IdentityName != "operator" {
		t.Fatalf("restart lost identity: id=%q name=%q", s.IdentityID, s.IdentityName)
	}
	if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepIdentity) {
		t.Fatalf("restart should re-ask the identity question, StateDelta[onboarding_step] = %q, want %q", got, string(StepIdentity))
	}
	if out.Content == "" {
		t.Fatal("restart transition Content must not be empty")
	}
	if out.Terminal {
		t.Fatal("restart transition must not be terminal")
	}
}

func TestSessionRestartClearsPriorAnswers(t *testing.T) {
	s := NewSession("identity-2", "local")
	if _, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}}); err != nil {
		t.Fatalf("identity answer: %v", err)
	}
	if s.Answers.Name != "Davide" {
		t.Fatalf("pre-restart name = %q, want Davide", s.Answers.Name)
	}
	if _, err := s.Apply(Input{Intent: IntentRestart}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if s.Answers.Name != "" {
		t.Fatalf("restart should clear answers, got name %q", s.Answers.Name)
	}
	if s.Step != StepIdentity {
		t.Fatalf("restart step = %q, want %q", s.Step, StepIdentity)
	}
}

func TestSessionInvalidIntent(t *testing.T) {
	s := NewSession("identity-1", "local")
	_, err := s.Apply(Input{Intent: Intent("bogus")})
	if !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("unknown intent error = %v, want ErrInvalidIntent", err)
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error should name the bad intent, got %v", err)
	}
}

func TestSessionEmptyIntentDefaultsToAnswer(t *testing.T) {
	s := NewSession("identity-1", "local")
	out, err := s.Apply(Input{Text: "Davide"})
	if err != nil {
		t.Fatalf("empty-intent apply: %v", err)
	}
	if s.Step != StepWork {
		t.Fatalf("after default answer step = %q, want %q", s.Step, StepWork)
	}
	if s.Answers.Name != "Davide" {
		t.Fatalf("text answer not merged as name: %q", s.Answers.Name)
	}
	if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepWork) {
		t.Fatalf("default-intent transition should advance to work, StateDelta[onboarding_step] = %q, want %q", got, string(StepWork))
	}
	if out.Content == "" {
		t.Fatal("transition Content must not be empty")
	}
}

func TestSessionEditBeforeDraftRejected(t *testing.T) {
	s := NewSession("identity-1", "local")
	_, err := s.Apply(Input{Intent: IntentEdit, Answers: Answers{ResponseLength: "short"}})
	if !errors.Is(err, ErrDraftRequired) {
		t.Fatalf("edit before draft error = %v, want ErrDraftRequired", err)
	}
	if s.Status != StatusActive {
		t.Fatalf("rejected edit changed status to %q, want active", s.Status)
	}
}

func TestMergeAnswersTextOnlyAtIdentityStep(t *testing.T) {
	s := NewSession("identity-1", "local")
	// At StepIdentity with no explicit Answers.Name, free Text becomes the name.
	s.mergeAnswers(Input{Text: "  Grace  "})
	if s.Answers.Name != "Grace" {
		t.Fatalf("name from text = %q, want Grace", s.Answers.Name)
	}

	// Past StepIdentity, free Text is NOT promoted to a name.
	s.Step = StepWork
	s.mergeAnswers(Input{Text: "Go, Postgres"})
	if s.Answers.Name != "Grace" {
		t.Fatalf("text at work step overwrote name: %q", s.Answers.Name)
	}
}

func TestMergeAnswersAllFields(t *testing.T) {
	voice := true
	proactive := false
	s := NewSession("identity-1", "local")
	s.mergeAnswers(Input{Answers: Answers{
		Name:                "  Ada  ",
		Lang:                "  it  ",
		Timezone:            "  Europe/Rome  ",
		TonePreference:      "  warm  ",
		ResponseLength:      "  short  ",
		VoiceMode:           &voice,
		CanProactiveMessage: &proactive,
		CustomInstructions:  "  be terse  ",
	}})
	got := s.Answers
	if got.Name != "Ada" || got.Lang != "it" || got.Timezone != "Europe/Rome" {
		t.Fatalf("name/lang/timezone not trimmed-merged: %+v", got)
	}
	if got.TonePreference != "warm" || got.ResponseLength != "short" {
		t.Fatalf("tone/length not trimmed-merged: %+v", got)
	}
	if got.CustomInstructions != "be terse" {
		t.Fatalf("custom instructions not trimmed-merged: %q", got.CustomInstructions)
	}
	if got.VoiceMode == nil || !*got.VoiceMode {
		t.Fatalf("voice mode not merged: %v", got.VoiceMode)
	}
	if got.CanProactiveMessage == nil || *got.CanProactiveMessage {
		t.Fatalf("proactive not merged: %v", got.CanProactiveMessage)
	}
}

func TestMergeAnswersPreservesExistingOnEmptyFields(t *testing.T) {
	s := NewSession("identity-1", "local")
	s.Answers = Answers{Name: "Ada", Lang: "it", Timezone: "Europe/Rome"}
	// Empty incoming fields must not clobber stored values.
	s.mergeAnswers(Input{Answers: Answers{TonePreference: "warm"}})
	if s.Answers.Name != "Ada" || s.Answers.Lang != "it" || s.Answers.Timezone != "Europe/Rome" {
		t.Fatalf("empty incoming fields clobbered stored answers: %+v", s.Answers)
	}
	if s.Answers.TonePreference != "warm" {
		t.Fatalf("new field not merged: %+v", s.Answers)
	}
}

func TestCurrentPromptPerStep(t *testing.T) {
	t.Run("identity step", func(t *testing.T) {
		s := NewSession("id", "n")
		out, ok := s.currentPrompt()
		if !ok || out.Content == "" {
			t.Fatalf("identity prompt ok=%v content=%q", ok, out.Content)
		}
		if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepIdentity) {
			t.Fatalf("identity prompt StateDelta[onboarding_step] = %q, want %q", got, string(StepIdentity))
		}
	})

	t.Run("work step", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = StepWork
		out, ok := s.currentPrompt()
		if !ok || out.Content == "" {
			t.Fatalf("work prompt ok=%v content=%q", ok, out.Content)
		}
		if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepWork) {
			t.Fatalf("work prompt StateDelta[onboarding_step] = %q, want %q", got, string(StepWork))
		}
	})

	t.Run("projects step", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = StepProjects
		out, ok := s.currentPrompt()
		if !ok || out.Content == "" {
			t.Fatalf("projects prompt ok=%v content=%q", ok, out.Content)
		}
		if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepProjects) {
			t.Fatalf("projects prompt StateDelta[onboarding_step] = %q, want %q", got, string(StepProjects))
		}
	})

	t.Run("social step", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = StepSocial
		out, ok := s.currentPrompt()
		if !ok || out.Content == "" {
			t.Fatalf("social prompt ok=%v content=%q", ok, out.Content)
		}
		if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepSocial) {
			t.Fatalf("social prompt StateDelta[onboarding_step] = %q, want %q", got, string(StepSocial))
		}
	})

	t.Run("style step", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = StepStyle
		out, ok := s.currentPrompt()
		if !ok || out.Content == "" {
			t.Fatalf("style prompt ok=%v content=%q", ok, out.Content)
		}
		if got, _ := out.StateDelta["onboarding_step"].(string); got != string(StepStyle) {
			t.Fatalf("style prompt StateDelta[onboarding_step] = %q, want %q", got, string(StepStyle))
		}
	})

	t.Run("draft step without draft is not promptable", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = StepDraft
		if _, ok := s.currentPrompt(); ok {
			t.Fatal("draft step with empty DraftAgentMD must not be promptable")
		}
	})

	t.Run("draft step with draft", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = StepDraft
		s.DraftAgentMD = "# Agent.md\nName: Ada"
		out, ok := s.currentPrompt()
		if !ok || !strings.Contains(out.Content, "Confirm, edit, or skip") {
			t.Fatalf("draft prompt ok=%v content=%q", ok, out.Content)
		}
		if got, _ := out.StateDelta["profile_draft"].(string); !strings.Contains(got, "Name: Ada") {
			t.Fatalf("draft prompt missing profile_draft state: %v", out.StateDelta["profile_draft"])
		}
	})

	t.Run("unknown step", func(t *testing.T) {
		s := NewSession("id", "n")
		s.Step = Step("bogus")
		if _, ok := s.currentPrompt(); ok {
			t.Fatal("unknown step must not be promptable")
		}
	})
}

func TestApplyAnswerUnknownStep(t *testing.T) {
	s := NewSession("id", "n")
	s.Step = Step("bogus")
	_, err := s.applyAnswer(Input{Answers: Answers{Name: "Ada"}})
	if err == nil || !strings.Contains(err.Error(), "unknown onboarding step") {
		t.Fatalf("applyAnswer on unknown step err = %v, want unknown-step error", err)
	}
}

func TestPreferencesJSONZeroValue(t *testing.T) {
	s := NewSession("id", "n")
	// All Preferences fields are omitempty: a zero value marshals to "{}".
	if got := s.preferencesJSON(); got != "{}" {
		t.Fatalf("preferencesJSON zero value = %q, want {}", got)
	}
}

func TestPreferencesJSONPopulated(t *testing.T) {
	s := NewSession("id", "n")
	s.Preferences.Lang = "it"
	s.Preferences.VoiceMode = true
	got := s.preferencesJSON()
	if !strings.Contains(got, `"lang":"it"`) || !strings.Contains(got, `"voice_mode":true`) {
		t.Fatalf("preferencesJSON = %q, want lang+voice_mode", got)
	}
}
