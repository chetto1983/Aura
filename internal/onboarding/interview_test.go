package onboarding

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

func TestInterviewLoopConfirmEmitsProfileState(t *testing.T) {
	voice := true
	s := NewSession("identity-1", "local")
	s.Queue(
		Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}},
		Input{Intent: IntentAnswer, Answers: Answers{}},
		Input{Intent: IntentAnswer, Answers: Answers{}},
		Input{Intent: IntentAnswer, Answers: Answers{}},
		Input{Intent: IntentAnswer, Answers: Answers{
			Lang:           "it",
			Timezone:       "Europe/Rome",
			TonePreference: "technical",
			ResponseLength: "concise",
			VoiceMode:      &voice,
		}},
		Input{Intent: IntentConfirm},
	)

	loop := NewLoop(s, 12)
	if loop.FindAgent("InterviewStepAgent") == nil {
		t.Fatal("loop must contain InterviewStepAgent")
	}
	events := drainOnboarding(t, loop)
	// 5 step questions + 1 draft + 1 confirm-terminal = 7 events.
	if len(events) != 7 {
		t.Fatalf("confirm path emitted %d events, want 7", len(events))
	}
	if events[0].LLMResponse.Content == "" {
		t.Fatal("first event Content must not be empty")
	}
	if got, _ := events[0].Actions.StateDelta["onboarding_step"].(string); got != string(StepIdentity) {
		t.Fatalf("first event should be the identity step, StateDelta[onboarding_step] = %q, want %q", got, string(StepIdentity))
	}
	assertNoToolCalls(t, events)

	last := events[len(events)-1]
	if !last.Actions.Escalate {
		t.Fatal("confirm final event must escalate")
	}
	if got, _ := last.Actions.StateDelta["onboarding_completed"].(bool); !got {
		t.Fatalf("onboarding_completed = %v, want true", last.Actions.StateDelta["onboarding_completed"])
	}
	if got, _ := last.Actions.StateDelta["resume_chat"].(bool); !got {
		t.Fatalf("resume_chat = %v, want true", last.Actions.StateDelta["resume_chat"])
	}
	agentMD, _ := last.Actions.StateDelta["agent_md"].(string)
	if !strings.Contains(agentMD, "Name: Davide") || !strings.Contains(agentMD, "Response length: concise") {
		t.Fatalf("agent_md missing extracted content:\n%s", agentMD)
	}
	prefsJSON, _ := last.Actions.StateDelta["preferences_json"].(string)
	if !strings.Contains(prefsJSON, `"lang":"it"`) || !strings.Contains(prefsJSON, `"voice_mode":true`) {
		t.Fatalf("preferences_json missing structured preferences: %s", prefsJSON)
	}
}

func TestInterviewLoopSkipEscalatesResumeChat(t *testing.T) {
	s := NewSession("identity-1", "local")
	s.Queue(Input{Intent: IntentSkip})

	events := drainOnboarding(t, NewLoop(s, 8))
	if len(events) != 1 {
		t.Fatalf("skip path emitted %d events, want 1", len(events))
	}
	last := events[0]
	if !last.Actions.Escalate {
		t.Fatal("skip final event must escalate")
	}
	if got, _ := last.Actions.StateDelta["skipped"].(bool); !got {
		t.Fatalf("skipped = %v, want true", last.Actions.StateDelta["skipped"])
	}
	if got, _ := last.Actions.StateDelta["resume_chat"].(bool); !got {
		t.Fatalf("resume_chat = %v, want true", last.Actions.StateDelta["resume_chat"])
	}
}

func TestInterviewLoopEditEmitsRevisedDraftBeforeConfirm(t *testing.T) {
	s := NewSession("identity-1", "local")
	s.Queue(
		Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}},
		Input{Intent: IntentAnswer, Answers: Answers{}},
		Input{Intent: IntentAnswer, Answers: Answers{}},
		Input{Intent: IntentAnswer, Answers: Answers{}},
		Input{Intent: IntentAnswer, Answers: Answers{Lang: "it", Timezone: "Europe/Rome", ResponseLength: "concise"}},
		Input{Intent: IntentEdit, Answers: Answers{ResponseLength: "short"}},
		Input{Intent: IntentConfirm},
	)

	events := drainOnboarding(t, NewLoop(s, 12))
	// 5 step questions + 1 draft + 1 revised-draft + 1 confirm-terminal = 8 events.
	if len(events) != 8 {
		t.Fatalf("edit path emitted %d events, want 8", len(events))
	}
	revisedDraft, _ := events[len(events)-2].Actions.StateDelta["profile_draft"].(string)
	if !strings.Contains(revisedDraft, "Response length: short") {
		t.Fatalf("revised draft missing edit:\n%s", revisedDraft)
	}
	last := events[len(events)-1]
	if !last.Actions.Escalate {
		t.Fatal("edit confirm final event must escalate")
	}
	agentMD, _ := last.Actions.StateDelta["agent_md"].(string)
	if !strings.Contains(agentMD, "Response length: short") {
		t.Fatalf("confirmed Agent.md missing edited response length:\n%s", agentMD)
	}
}

func drainOnboarding(t *testing.T, a agent.Agent) []*agent.Event {
	t.Helper()
	maxSteps := 20
	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{
		Ctx:       context.Background(),
		Agent:     a,
		RequestID: uuid.Must(uuid.NewV7()),
		Branch:    "test.onboarding",
		Budget:    budget,
	}
	var events []*agent.Event
	for ev, err := range a.Run(ic) {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if ev != nil {
			events = append(events, ev)
		}
	}
	return events
}

func assertNoToolCalls(t *testing.T, events []*agent.Event) {
	t.Helper()
	for i, ev := range events {
		if ev.LLMResponse != nil && len(ev.LLMResponse.ToolCalls) > 0 {
			t.Fatalf("event %d unexpectedly carried tool calls: %+v", i, ev.LLMResponse.ToolCalls)
		}
	}
}
