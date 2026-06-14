package onboarding

import (
	"errors"
	"strings"
	"testing"
)

func TestSessionConfirmRequiresDraft(t *testing.T) {
	s := NewSession("identity-1", "local")

	_, err := s.Apply(Input{Intent: IntentConfirm})
	if !errors.Is(err, ErrDraftRequired) {
		t.Fatalf("confirm before draft error = %v, want ErrDraftRequired", err)
	}
	if s.Status != StatusActive {
		t.Fatalf("status after rejected confirm = %q, want %q", s.Status, StatusActive)
	}
}

func TestSessionSkipAndCancelAreTerminal(t *testing.T) {
	tests := []struct {
		name       string
		input      Input
		wantStatus Status
		wantKey    string
	}{
		{
			name:       "skip",
			input:      Input{Intent: IntentSkip},
			wantStatus: StatusSkipped,
			wantKey:    "skipped",
		},
		{
			name:       "cancel",
			input:      Input{Intent: IntentCancel},
			wantStatus: StatusCanceled,
			wantKey:    "canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSession("identity-1", "local")
			out, err := s.Apply(tt.input)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if s.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", s.Status, tt.wantStatus)
			}
			if !out.Terminal {
				t.Fatal("skip/cancel must be terminal")
			}
			if got, _ := out.StateDelta[tt.wantKey].(bool); !got {
				t.Fatalf("state %q = %v, want true", tt.wantKey, out.StateDelta[tt.wantKey])
			}
			if got, _ := out.StateDelta["resume_chat"].(bool); !got {
				t.Fatalf("resume_chat = %v, want true", out.StateDelta["resume_chat"])
			}
		})
	}
}

func TestSessionEditReopensDraftAndCanConfirm(t *testing.T) {
	s := NewSession("identity-1", "local")
	// Drive through all 5 collecting steps.
	steps := []Input{
		{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}},
		{Intent: IntentAnswer, Answers: Answers{}},
		{Intent: IntentAnswer, Answers: Answers{}},
		{Intent: IntentAnswer, Answers: Answers{}},
	}
	for i, in := range steps {
		if _, err := s.Apply(in); err != nil {
			t.Fatalf("step %d answer: %v", i, err)
		}
	}
	// Final collecting step (style) → produces draft.
	draft, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{
		Lang:           "it",
		Timezone:       "Europe/Rome",
		TonePreference: "technical",
		ResponseLength: "concise",
	}})
	if err != nil {
		t.Fatalf("style answer: %v", err)
	}
	if s.Status != StatusDraft || s.Step != StepDraft {
		t.Fatalf("after style status/step = %q/%q, want draft/%q", s.Status, s.Step, StepDraft)
	}
	profileDraft, _ := draft.StateDelta["profile_draft"].(string)
	if !strings.Contains(profileDraft, "Agent.md") {
		t.Fatalf("draft transition missing Agent.md/profile_draft: %+v", draft)
	}

	revised, err := s.Apply(Input{Intent: IntentEdit, Answers: Answers{ResponseLength: "short"}})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if s.Status != StatusDraft || s.Step != StepDraft {
		t.Fatalf("after edit status/step = %q/%q, want draft/%q", s.Status, s.Step, StepDraft)
	}
	if !strings.Contains(revised.Content, "updated") {
		t.Fatalf("edit transition should describe revised draft, got %q", revised.Content)
	}
	if !strings.Contains(s.DraftAgentMD, "short") {
		t.Fatalf("edited draft did not include revised response length:\n%s", s.DraftAgentMD)
	}

	done, err := s.Apply(Input{Intent: IntentConfirm})
	if err != nil {
		t.Fatalf("confirm after edit: %v", err)
	}
	if s.Status != StatusCompleted || !done.Terminal {
		t.Fatalf("confirm status/terminal = %q/%v, want completed/true", s.Status, done.Terminal)
	}
	if got, _ := done.StateDelta["onboarding_completed"].(bool); !got {
		t.Fatalf("onboarding_completed = %v, want true", done.StateDelta["onboarding_completed"])
	}
	if got, _ := done.StateDelta["resume_chat"].(bool); !got {
		t.Fatalf("resume_chat = %v, want true", done.StateDelta["resume_chat"])
	}
}

func TestSession_FiveStepFlow(t *testing.T) {
	s := NewSession("id1", "Davide")
	if s.Step != StepIdentity {
		t.Fatalf("first step = %q, want %q", s.Step, StepIdentity)
	}
	steps := []Step{StepIdentity, StepWork, StepProjects, StepSocial, StepStyle}
	for i, want := range steps {
		if s.Step != want {
			t.Fatalf("step %d = %q, want %q", i, s.Step, want)
		}
		out, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}})
		if err != nil {
			t.Fatalf("apply step %d: %v", i, err)
		}
		if i < len(steps)-1 && out.Terminal {
			t.Fatalf("step %d unexpectedly terminal", i)
		}
	}
	if s.Step != StepDraft || s.Status != StatusDraft {
		t.Fatalf("after style: step=%q status=%q, want draft/draft", s.Step, s.Status)
	}
}

// TestSession_EditReplacesSliceField asserts that IntentEdit REPLACES a slice
// field rather than appending to it, so a correction ("Agenti AI") removes the
// old typo ("Afenti Ai") instead of accumulating both.
func TestSession_EditReplacesSliceField(t *testing.T) {
	s := NewSession("id1", "Davide")

	// Build a session at draft stage with a typo in Stack.
	for _, in := range []Input{
		{Intent: IntentAnswer, Answers: Answers{Name: "Davide", Role: "dev", Company: "Aura"}},
		{Intent: IntentAnswer, Answers: Answers{Expertise: []string{"backend"}, Stack: []string{"Afenti Ai"}}},
		{Intent: IntentAnswer, Answers: Answers{Projects: []string{"Aura"}}},
		{Intent: IntentAnswer, Answers: Answers{Interests: []string{"AI"}}},
		{Intent: IntentAnswer, Answers: Answers{Lang: "it", Timezone: "Europe/Rome"}},
	} {
		if _, err := s.Apply(in); err != nil {
			t.Fatalf("drive to draft: %v", err)
		}
	}
	if s.Step != StepDraft {
		t.Fatalf("expected StepDraft, got %q", s.Step)
	}
	if !strings.Contains(s.DraftAgentMD, "Afenti Ai") {
		t.Fatalf("expected typo in initial draft:\n%s", s.DraftAgentMD)
	}

	// Apply an edit with the corrected stack.
	_, err := s.Apply(Input{Intent: IntentEdit, Answers: Answers{Stack: []string{"Agenti AI"}}})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	if len(s.Answers.Stack) != 1 {
		t.Fatalf("Stack len = %d, want 1 (REPLACED not appended): %v", len(s.Answers.Stack), s.Answers.Stack)
	}
	if s.Answers.Stack[0] != "Agenti AI" {
		t.Fatalf("Stack[0] = %q, want %q", s.Answers.Stack[0], "Agenti AI")
	}
	if strings.Contains(s.DraftAgentMD, "Afenti Ai") {
		t.Fatalf("draft still contains the old typo:\n%s", s.DraftAgentMD)
	}
	if !strings.Contains(s.DraftAgentMD, "Agenti AI") {
		t.Fatalf("draft missing corrected value:\n%s", s.DraftAgentMD)
	}
}
