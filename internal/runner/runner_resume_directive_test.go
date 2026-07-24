package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
)

// schedApprovalPending is defined in runner_resume_scheduled_approval_test.go (same
// package); it produces a Kind=approval pause with the scheduled_task_approval
// resume_context, which is all classifyResolve/isScheduledTaskApproval inspect.
func TestClassifyResolve(t *testing.T) {
	ordinary := askuser.Pending{Kind: "clarification"}
	gateway := askuser.Pending{Kind: tools.KindApproval} // shell/gateway approval: no scheduled resume ctx
	sched := schedApprovalPending("11112222-3333-4444-5555-666677778888")

	cases := []struct {
		name      string
		pending   askuser.Pending
		action    string
		remaining int
		want      ResolveDirective
	}{
		{"cancel wins", sched, askuser.ActionCancel, 0, ResolveDirective{OutcomeTerminated, 0}},
		{"remaining wins over continue", ordinary, askuser.ActionAccept, 2, ResolveDirective{OutcomePending, 2}},
		{"sched accept -> approved", sched, askuser.ActionAccept, 0, ResolveDirective{OutcomeApproved, 0}},
		{"sched decline -> rejected", sched, askuser.ActionDecline, 0, ResolveDirective{OutcomeRejected, 0}},
		{"ordinary accept -> continue", ordinary, askuser.ActionAccept, 0, ResolveDirective{OutcomeContinue, 0}},
		{"gateway approval -> continue", gateway, askuser.ActionAccept, 0, ResolveDirective{OutcomeContinue, 0}},
		{"sched with remaining still pending", sched, askuser.ActionAccept, 1, ResolveDirective{OutcomePending, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyResolve(c.pending, c.action, c.remaining); got != c.want {
				t.Fatalf("classifyResolve = %+v, want %+v", got, c.want)
			}
		})
	}
}
