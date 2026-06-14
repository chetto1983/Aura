package onboarding

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

func newTestIC(t *testing.T, a agent.Agent) agent.InvocationContext {
	t.Helper()
	maxSteps := 20
	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return agent.InvocationContext{
		Ctx:       context.Background(),
		Agent:     a,
		RequestID: uuid.Must(uuid.NewV7()),
		Branch:    "test.onboarding",
		Budget:    budget,
	}
}

func TestNewInterviewStepAgentNilSession(t *testing.T) {
	a := NewInterviewStepAgent(nil)
	if a == nil || a.session == nil {
		t.Fatal("nil session must be replaced with a fresh session")
	}
	if a.session.Step != StepIdentity || a.session.Status != StatusActive {
		t.Fatalf("default session state = %q/%q, want identity/active", a.session.Step, a.session.Status)
	}
}

func TestInterviewStepAgentMetadata(t *testing.T) {
	a := NewInterviewStepAgent(NewSession("id", "n"))
	if a.Name() != InterviewStepAgentName {
		t.Fatalf("Name() = %q, want %q", a.Name(), InterviewStepAgentName)
	}
	if a.Description() == "" {
		t.Fatal("Description() must not be empty")
	}
	if subs := a.SubAgents(); subs != nil {
		t.Fatalf("SubAgents() = %v, want nil (leaf agent)", subs)
	}
}

func TestInterviewStepAgentFindAgent(t *testing.T) {
	a := NewInterviewStepAgent(NewSession("id", "n"))
	if got := a.FindAgent(InterviewStepAgentName); got != agent.Agent(a) {
		t.Fatalf("FindAgent(self) = %v, want the agent itself", got)
	}
	if got := a.FindAgent("OtherAgent"); got != nil {
		t.Fatalf("FindAgent(miss) = %v, want nil", got)
	}
}

func TestInterviewStepAgentRunNoTransitionEmitsNothing(t *testing.T) {
	// A terminal session yields no transition: Run must emit zero events, no error.
	s := NewSession("id", "n")
	if _, err := s.Apply(Input{Intent: IntentSkip}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	a := NewInterviewStepAgent(s)

	var events []*agent.Event
	for ev, err := range a.Run(newTestIC(t, a)) {
		if err != nil {
			t.Fatalf("Run on terminal session errored: %v", err)
		}
		if ev != nil {
			events = append(events, ev)
		}
	}
	if len(events) != 0 {
		t.Fatalf("terminal session emitted %d events, want 0", len(events))
	}
}

func TestInterviewStepAgentRunSurfacesTransitionError(t *testing.T) {
	// Prompt the session, then queue an invalid-intent input. After the prompt is
	// consumed, applyQueued -> Apply returns ErrInvalidIntent, surfaced via Run's
	// error slot with a nil event.
	s := NewSession("id", "n")
	s.prompted = true
	s.Queue(Input{Intent: Intent("nope")})
	a := NewInterviewStepAgent(s)

	var (
		gotErr   error
		eventCnt int
	)
	for ev, err := range a.Run(newTestIC(t, a)) {
		if err != nil {
			gotErr = err
		}
		if ev != nil {
			eventCnt++
		}
	}
	if !errors.Is(gotErr, ErrInvalidIntent) {
		t.Fatalf("Run error = %v, want ErrInvalidIntent", gotErr)
	}
	if eventCnt != 0 {
		t.Fatalf("error transition emitted %d events, want 0", eventCnt)
	}
}

func TestInterviewLoopQueuedRestartIsFullReset(t *testing.T) {
	// restart is a shouldApplyBeforePrompt intent: it is applied before the identity
	// prompt and performs a full session reset (*s = *NewSession), which discards
	// any remaining queued inputs. The trailing skip is therefore dropped, and the
	// loop emits exactly the re-asked identity question (non-terminal).
	s := NewSession("identity-1", "local")
	s.Queue(
		Input{Intent: IntentRestart},
		Input{Intent: IntentSkip},
	)
	events := drainOnboarding(t, NewLoop(s, 8))
	if len(events) != 1 {
		t.Fatalf("queued restart emitted %d events, want 1 (reset discards the queue)", len(events))
	}
	if events[0].Actions.Escalate {
		t.Fatal("restart re-prompt must not escalate")
	}
	if events[0].LLMResponse.Content == "" {
		t.Fatal("restart re-prompt Content must not be empty")
	}
	if got, _ := events[0].Actions.StateDelta["onboarding_step"].(string); got != string(StepIdentity) {
		t.Fatalf("restart should re-ask the identity step, StateDelta[onboarding_step] = %q, want %q", got, string(StepIdentity))
	}
	if s.Status != StatusActive || s.Step != StepIdentity {
		t.Fatalf("after queued restart status/step = %q/%q, want active/identity", s.Status, s.Step)
	}
	if len(s.pending) != 0 {
		t.Fatalf("restart should clear the pending queue, %d left", len(s.pending))
	}
}

func TestInterviewLoopSkipBeforePromptShortCircuits(t *testing.T) {
	// A queued skip is applied before the identity prompt (shouldApplyBeforePrompt),
	// terminating immediately without ever asking the identity question.
	s := NewSession("identity-1", "local")
	s.Queue(Input{Intent: IntentSkip})
	events := drainOnboarding(t, NewLoop(s, 8))
	if len(events) != 1 {
		t.Fatalf("skip-before-prompt emitted %d events, want 1", len(events))
	}
	last := events[0]
	if !last.Actions.Escalate {
		t.Fatal("final event after skip must escalate")
	}
	if got, _ := last.Actions.StateDelta["skipped"].(bool); !got {
		t.Fatalf("skipped = %v, want true", last.Actions.StateDelta["skipped"])
	}
	if s.Status != StatusSkipped {
		t.Fatalf("session status = %q, want %q", s.Status, StatusSkipped)
	}
}

func TestInterviewLoopCancelBeforeAnyPrompt(t *testing.T) {
	// cancel is a shouldApplyBeforePrompt intent: it short-circuits the identity prompt.
	s := NewSession("identity-1", "local")
	s.Queue(Input{Intent: IntentCancel})
	events := drainOnboarding(t, NewLoop(s, 8))
	if len(events) != 1 {
		t.Fatalf("cancel-before-prompt emitted %d events, want 1", len(events))
	}
	if !events[0].Actions.Escalate {
		t.Fatal("cancel event must escalate")
	}
	if got, _ := events[0].Actions.StateDelta["canceled"].(bool); !got {
		t.Fatalf("canceled = %v, want true", events[0].Actions.StateDelta["canceled"])
	}
}

func TestTransitionEventCarriesContext(t *testing.T) {
	ic := newTestIC(t, nil)
	out := Transition{
		Content:    "hello",
		StateDelta: map[string]any{"k": "v"},
		Terminal:   true,
	}
	ev := transitionEvent(ic, "author-x", out)
	if ev.Author != "author-x" {
		t.Fatalf("Author = %q, want author-x", ev.Author)
	}
	if ev.RequestID != ic.RequestID || ev.Branch != ic.Branch {
		t.Fatal("event must carry request id and branch from context")
	}
	if ev.LLMResponse == nil || ev.LLMResponse.Content != "hello" {
		t.Fatalf("event content = %v, want hello", ev.LLMResponse)
	}
	if !ev.Actions.Escalate {
		t.Fatal("terminal transition must set Escalate")
	}
	if got, _ := ev.Actions.StateDelta["k"].(string); got != "v" {
		t.Fatalf("state delta not carried: %v", ev.Actions.StateDelta)
	}
	if ev.Timestamp.IsZero() {
		t.Fatal("timestamp must be set")
	}
}
