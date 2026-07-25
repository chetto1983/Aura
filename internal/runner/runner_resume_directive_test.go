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

// TestIsScheduledTaskApprovalGuards covers EVERY guard of the detector, one by one. Each is
// load-bearing: a false positive here routes an ordinary pause to a deterministic outcome and
// silently drops the model's continuation turn, and a false negative re-drives a scheduled gate.
// A mutation run proved the compound condition was under-tested — only the whole-expression
// happy path was pinned, so replacing either half with `true` survived.
func TestIsScheduledTaskApprovalGuards(t *testing.T) {
	ctx := func(raw string) []byte { return []byte(raw) }
	for _, tc := range []struct {
		name    string
		pending askuser.Pending
		want    bool
	}{
		{
			"valid scheduled approval",
			askuser.Pending{Kind: tools.KindApproval, ResumeContext: ctx(
				`{"type":"scheduled_task_approval","task_id":"019f9349-e0e5-77cc-9639-804bcf88b968"}`)},
			true,
		},
		{
			"wrong kind is not scheduled",
			askuser.Pending{Kind: "clarification", ResumeContext: ctx(
				`{"type":"scheduled_task_approval","task_id":"t-1"}`)},
			false,
		},
		{
			"no resume context is not scheduled",
			askuser.Pending{Kind: tools.KindApproval},
			false,
		},
		{
			"malformed resume context is not scheduled",
			askuser.Pending{Kind: tools.KindApproval, ResumeContext: ctx(`{"type":`)},
			false,
		},
		{
			// A shell/gateway approval also carries a resume_context: only the TYPE separates it
			// from a scheduler gate, so this is the false-positive that would eat its turn.
			"another approval type is not scheduled",
			askuser.Pending{Kind: tools.KindApproval, ResumeContext: ctx(
				`{"type":"shell_approval","task_id":"t-1"}`)},
			false,
		},
		{
			// Type matches but there is nothing to activate — treating it as a gate would answer
			// Approved while no task ever transitions.
			"empty task id is not scheduled",
			askuser.Pending{Kind: tools.KindApproval, ResumeContext: ctx(
				`{"type":"scheduled_task_approval","task_id":""}`)},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isScheduledTaskApproval(tc.pending); got != tc.want {
				t.Fatalf("isScheduledTaskApproval = %v, want %v", got, tc.want)
			}
		})
	}
}
