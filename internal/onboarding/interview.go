package onboarding

import (
	"iter"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

const (
	InterviewStepAgentName = "InterviewStepAgent"
	LoopName               = "ProfileOnboardingLoop"
)

type InterviewStepAgent struct {
	session *Session
}

func NewInterviewStepAgent(session *Session) *InterviewStepAgent {
	if session == nil {
		session = NewSession("", "")
	}
	return &InterviewStepAgent{session: session}
}

func NewLoop(session *Session, maxIter uint) agent.Agent {
	return workflow.NewLoop(LoopName, maxIter, NewInterviewStepAgent(session))
}

func (a *InterviewStepAgent) Name() string { return InterviewStepAgentName }

func (*InterviewStepAgent) Description() string {
	return "collects first-run profile facts and escalates on confirm, skip, or cancel"
}

func (*InterviewStepAgent) SubAgents() []agent.Agent { return nil }

func (a *InterviewStepAgent) FindAgent(name string) agent.Agent {
	if name == a.Name() {
		return a
	}
	return nil
}

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
