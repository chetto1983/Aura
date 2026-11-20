package runner

import (
	"github.com/chetto1983/aura/internal/askuser"
)

// ResolveOutcome is the runner-owned decision about what a channel does after a pause
// resolves. It is a semantic CODE (never user prose): the runner is not locale-aware, so
// each channel maps Approved/Rejected to its own localized confirmation.
type ResolveOutcome int

// The ResolveOutcome codes a channel switches on to render a resolved pause.
const (
	OutcomeContinue   ResolveOutcome = iota // in-session: the channel drives its own continuation render
	OutcomeApproved                         // worker gate approved: render its deterministic confirmation
	OutcomeRejected                         // worker gate rejected: render its deterministic confirmation
	OutcomePending                          // remaining>0: render the next FIFO pause, nothing else
	OutcomeTerminated                       // cancel: the runner already auto-resolved; nothing to render
)

// ResolveDirective is the single output of resolving one pause. Both Telegram and the
// cockpit render it; neither re-derives the continue-vs-outcome decision (codex/ADK
// "one resolver, transports only render it").
type ResolveDirective struct {
	Outcome   ResolveOutcome
	Remaining int
}

// classifyResolve is the ONE continuation/outcome decision. Pure function of the pause's
// nature + the action + the remaining count — no channel/transport input.
//
//	cancel                              -> Terminated (runner already aborted the turn)
//	remaining>0                         -> Pending (more FIFO pauses to answer first)
//	worker-owned approval              -> Approved/Rejected (the worker owns continuation)
//	otherwise                          -> Continue (including scheduled task acknowledgements)
func classifyResolve(pending askuser.Pending, action string, remaining int) ResolveDirective {
	if action == askuser.ActionCancel {
		return ResolveDirective{Outcome: OutcomeTerminated}
	}
	if remaining > 0 {
		return ResolveDirective{Outcome: OutcomePending, Remaining: remaining}
	}
	if pending.OwningWorkerID != "" {
		if action == askuser.ActionDecline {
			return ResolveDirective{Outcome: OutcomeRejected}
		}
		return ResolveDirective{Outcome: OutcomeApproved}
	}
	return ResolveDirective{Outcome: OutcomeContinue}
}
