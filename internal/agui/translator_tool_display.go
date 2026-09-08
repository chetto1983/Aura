package agui

import (
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
)

func eventDisplay(ev *agent.Event) *display.Payload {
	ti := ev.Actions.ToolInvocation
	if ti != nil && ti.Event == agent.ToolInvocationEnd && (ti.ToolName == "shell_exec" || ti.ToolName == "sandbox_exec") {
		// Retained transcripts may carry a display produced before cancellation
		// was represented. Re-project its outcome without rewriting the audit record.
		p, ok := display.NormalizeToolPreview(ti.ToolCallID, ti.ToolName, ti.ResultPreview, display.NewRegistry())
		if ok && p.Code != nil && p.Code.Cancelled {
			return &p
		}
	}
	return ev.Actions.Display
}
