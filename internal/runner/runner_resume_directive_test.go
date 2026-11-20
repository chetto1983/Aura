package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
)

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
		{"sched accept -> acknowledgement", sched, askuser.ActionAccept, 0, ResolveDirective{OutcomeContinue, 0}},
		{"sched decline -> acknowledgement", sched, askuser.ActionDecline, 0, ResolveDirective{OutcomeContinue, 0}},
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

func TestScheduledApprovalContext(t *testing.T) {
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
			"scheduled type with missing id must still enter validation",
			askuser.Pending{Kind: tools.KindApproval, ResumeContext: ctx(
				`{"type":"scheduled_task_approval","task_id":""}`)},
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := scheduledApprovalContext(tc.pending)
			if got := rc != nil; got != tc.want {
				t.Fatalf("scheduled approval context recognized = %v, want %v", got, tc.want)
			}
		})
	}
}
