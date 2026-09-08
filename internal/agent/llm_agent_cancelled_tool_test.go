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
