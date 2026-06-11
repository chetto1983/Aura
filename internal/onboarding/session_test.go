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
	if _, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}}); err != nil {
		t.Fatalf("name answer: %v", err)
	}
	draft, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{
		Lang:           "it",
		Timezone:       "Europe/Rome",
		TonePreference: "technical",
		ResponseLength: "concise",
	}})
	if err != nil {
		t.Fatalf("preferences answer: %v", err)
	}
	if s.Status != StatusDraft || s.Step != StepDraft {
		t.Fatalf("after preferences status/step = %q/%q, want draft/%q", s.Status, s.Step, StepDraft)
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
