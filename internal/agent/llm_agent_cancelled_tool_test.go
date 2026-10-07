package agent

import (
	"github.com/chetto1983/aura/internal/agent/tools"
	"testing"
)

func TestToolResultEventPreservesCancellationOutcome(t *testing.T) {
	a := &LlmAgent{}
	meta := tools.ToolResultMeta{"cancelled": true}
	ev := a.toolResultEvent(InvocationContext{}, [8]byte{}, nil, toolRunResult{
		ToolCallID: "call", ToolName: "shell_exec", Result: tools.ToolResult{Meta: &meta},
	})
	if ev.Actions.ToolInvocation.Status != "canceled" || ev.Actions.ToolInvocation.Meta["cancelled"] != true {
		t.Fatalf("cancellation became success: %+v", ev.Actions.ToolInvocation)
	}
}

// web_search and web_fetch hand their errors to the model inline so it can adapt;
// the ledger must still record them as failures. On 2026-10-07 three failed fetches of
// one conversation sat in aura.tool_invocations as status=ok with no error.
func TestToolResultEventRecordsAnInlineToolErrorAsError(t *testing.T) {
	a := &LlmAgent{}
	meta := tools.ToolResultMeta{tools.MetaToolError: "http_error status 403"}
	ev := a.toolResultEvent(InvocationContext{}, [8]byte{}, nil, toolRunResult{
		ToolCallID: "call", ToolName: "web_fetch", Result: tools.ToolResult{Meta: &meta},
	})
	ti := ev.Actions.ToolInvocation
	if ti.Status != "error" || ti.Error != "http_error status 403" {
		t.Fatalf("an inline web error was recorded as status=%q error=%q", ti.Status, ti.Error)
	}
}
