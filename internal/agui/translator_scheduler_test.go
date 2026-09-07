package agui

import (
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

// translator_scheduler_test.go covers the aura.scheduler CUSTOM event: the signal that a run
// changed the schedule, so a cockpit with the governance board open rereads it.
//
// Without it the board is a snapshot taken at mount. queryClient.ts sets
// refetchOnWindowFocus:false for the whole SPA and useSchedulerMutations invalidates only on a
// cockpit approve/run/cancel — so a reminder created IN CHAT reached Postgres, fired, delivered
// on Telegram, and never appeared on the board. Measured 2026-09-07 on the operator's
// deployment: the row was in scheduler_tasks at 07:31:16 and the board beside the chat did not
// have it.

// toolEndWithScheduler builds a tool-RESULT event that ALSO carries a SchedulerDelta — the
// shape the task tool's Meta lift stamps onto the same tool-end event.
func toolEndWithScheduler(callID, name, preview string, delta map[string]any) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{
			ToolInvocation: &agent.ToolInvocation{
				Event: agent.ToolInvocationEnd, ToolCallID: callID, ToolName: name, ResultPreview: preview,
			},
			SchedulerDelta: delta,
		}}
}

// TestTranslatorToolResultEmitsInlineScheduler is the path the operator actually hits: the
// task tool answers inside a run, so the delta rides a tool-END event and must be emitted
// inline by emitToolResultCustom rather than by the standalone branch (which a tool-lifecycle
// event never reaches — it `continue`s).
func TestTranslatorToolResultEmitsInlineScheduler(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-1", "task", `{"action":"create"}`),
		toolEndWithScheduler("call-1", "task", "1 task(s)", map[string]any{"action": "create"}),
	})

	if got := countType(evs, "TOOL_CALL_RESULT"); got != 1 {
		t.Fatalf("TOOL_CALL_RESULT count = %d, want 1", got)
	}
	if got := countType(evs, "CUSTOM"); got != 1 {
		t.Fatalf("CUSTOM count = %d, want the one aura.scheduler frame", got)
	}
}

// TestTranslatorSchedulerCarriesTheCanonicalName pins the wire name. The cockpit keys on the
// string, so a rename here is a silent break there — which is why the constant is exported and
// asserted rather than spelled twice.
func TestTranslatorSchedulerCarriesTheCanonicalName(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-1", "task", `{"action":"cancel"}`),
		toolEndWithScheduler("call-1", "task", "cancelled", map[string]any{"action": "cancel"}),
	})

	var names []string
	for _, e := range evs {
		if ce, ok := e.(*events.CustomEvent); ok {
			names = append(names, ce.Name)
		}
	}
	if len(names) != 1 || names[0] != SchedulerEventName {
		t.Fatalf("custom event names = %v, want exactly [%q]", names, SchedulerEventName)
	}
}

// TestTranslatorWithoutASchedulerDeltaEmitsNothing keeps the branch additive: every tool result
// that is not a schedule change must produce the same event stream it produced before.
func TestTranslatorWithoutASchedulerDeltaEmitsNothing(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-1", "task", `{"action":"list"}`),
		toolEndWithScheduler("call-1", "task", "2 task(s)", nil),
	})

	if got := countType(evs, "CUSTOM"); got != 0 {
		t.Fatalf("CUSTOM count = %d, want none for a read-only task action", got)
	}
}
