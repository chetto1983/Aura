package handlers

import (
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// outboundRefusedLine closes the outcome of a job that tried to write to someone else. The
// gateway refuses such a send in a headless run (it needs a reviewed draft), and without this
// line the model's own last words ("done, sent") were the whole report.
const outboundRefusedLine = "A message to someone else was not sent: a scheduled job cannot write to other people; schedule a reviewed message instead."

// refusedOutboundSend reports whether an event is the refusal of a send to someone else: the
// end of a refused call to a tool the registry knows as a managed recipe's send (decision 10).
// The review refusal is the first check in Decide, so such a refusal can only be that one.
func (h AgentJobHandler) refusedOutboundSend(inv *agent.ToolInvocation) bool {
	if inv == nil || inv.Event != agent.ToolInvocationEnd || !inv.Refused || h.Deps.Registry == nil {
		return false
	}
	tool, ok := h.Deps.Registry.Get(inv.ToolName)
	if !ok {
		return false
	}
	_, outbound := messagedrafts.TrustedOutboundTarget(tool.Spec(), json.RawMessage(inv.Arguments))
	return outbound
}
