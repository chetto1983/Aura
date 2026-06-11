package onboarding

import (
	"iter"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

const (
	// InterviewStepAgentName is the agent name emitted on onboarding events.
	InterviewStepAgentName = "InterviewStepAgent"
	// LoopName is the workflow loop name for profile onboarding.
	LoopName = "ProfileOnboardingLoop"
)

// InterviewStepAgent adapts onboarding session transitions to agent events.
type InterviewStepAgent struct {
	session *Session
}

// NewInterviewStepAgent creates an interview step agent backed by a session.
func NewInterviewStepAgent(session *Session) *InterviewStepAgent {
	if session == nil {
		session = NewSession("", "")
	}
	return &InterviewStepAgent{session: session}
}

// NewLoop creates the profile onboarding workflow loop.
func NewLoop(session *Session, maxIter uint) agent.Agent {
	return workflow.NewLoop(LoopName, maxIter, NewInterviewStepAgent(session))
}

// Name returns the onboarding step agent name.
func (a *InterviewStepAgent) Name() string { return InterviewStepAgentName }

// Description returns a short description of the onboarding step agent.
func (*InterviewStepAgent) Description() string {
	return "collects first-run profile facts and escalates on confirm, skip, or cancel"
}

// SubAgents returns no child agents because onboarding is a leaf step.
func (*InterviewStepAgent) SubAgents() []agent.Agent { return nil }

// FindAgent returns this agent when the requested name matches.
func (a *InterviewStepAgent) FindAgent(name string) agent.Agent {
	if name == a.Name() {
		return a
	}
	return nil
}

// Run emits the next onboarding transition event, when one is available.
func (a *InterviewStepAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		out, ok, err := a.session.nextTransition()
		if err != nil {
			yield(nil, err)
			return
		}
		if !ok {
			return
		}
		_ = yield(transitionEvent(ic, a.Name(), out), nil)
	}
}

func transitionEvent(ic agent.InvocationContext, author string, out Transition) *agent.Event {
	return &agent.Event{
		RequestID: ic.RequestID,
		SpanID:    ic.SpanID,
		Author:    author,
		Branch:    ic.Branch,
		LLMResponse: &agent.LLMResponse{
			Content: out.Content,
		},
		Actions: agent.Actions{
			Escalate:   out.Terminal,
			StateDelta: out.StateDelta,
		},
		Timestamp: time.Now().UTC(),
	}
}
