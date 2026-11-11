package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// trustedSend is a mounted send to someone else: the job may never run it.
type trustedSend struct{ executions int }

func (s *trustedSend) Spec() tools.Spec {
	return tools.Spec{Name: "whatsapp__send_message", TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message", Mutating: true}
}

func (s *trustedSend) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.executions++
	return tools.ToolResult{}, nil
}

// refusedWrite is an ordinary mutating tool the gateway would refuse.
type refusedWrite struct{}

func (refusedWrite) Spec() tools.Spec { return tools.Spec{Name: "write_thing", Mutating: true} }
func (refusedWrite) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

const sendArgs = `{"recipient":"10000000000","message":"Ciao"}`

func runOutboundJob(t *testing.T, turns ...agenttest.FakeTurn) (string, *trustedSend) {
	t.Helper()
	send := &trustedSend{}
	registry := jobRegistry()
	registry.Register(send)
	h := AgentJobHandler{Deps: AgentDeps{Client: agenttest.NewFakeClient(turns...), Registry: registry}}
	summary, err := h.Run(context.Background(), Job{Payload: []byte(`{"goal":"say hi to the contact"}`), StepBudget: 10, RunID: "run-outbound"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return summary, send
}

func TestAJobWhoseSendWasRefusedSaysSoInItsOutcome(t *testing.T) {
	summary, send := runOutboundJob(t,
		agenttest.ToolCallTurn(agenttest.MakeToolCall("send-1", "whatsapp__send_message", sendArgs)),
		agenttest.TextChunks("stop", "Fatto, inviato."),
	)
	if send.executions != 0 {
		t.Fatalf("the job sent the message %d times", send.executions)
	}
	if !strings.HasSuffix(summary, outboundRefusedLine) {
		t.Fatalf("summary = %q, want it to end with the refused line", summary)
	}
}

func TestTheLineIsAddedOnceForTwoRefusedSends(t *testing.T) {
	summary, _ := runOutboundJob(t,
		agenttest.ToolCallTurn(agenttest.MakeToolCall("send-1", "whatsapp__send_message", sendArgs)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("send-2", "whatsapp__send_message", sendArgs)),
		agenttest.TextChunks("stop", "Fatto."),
	)
	if strings.Count(summary, outboundRefusedLine) != 1 {
		t.Fatalf("summary = %q, want the line once", summary)
	}
}

func TestAnOrdinaryRefusalAddsNoLine(t *testing.T) {
	h := AgentJobHandler{}
	inv := &agentToolEnd{name: "write_thing", args: `{}`}
	h.Deps.Registry = tools.NewRegistry()
	h.Deps.Registry.Register(refusedWrite{})
	if h.refusedOutboundSend(inv.event()) {
		t.Fatal("an ordinary refusal was read as a refused send")
	}
	h.Deps.Registry.Register(&trustedSend{})
	if !h.refusedOutboundSend((&agentToolEnd{name: "whatsapp__send_message", args: sendArgs}).event()) {
		t.Fatal("a refused trusted send was not recognized")
	}
}

// agentToolEnd is a refused tool call's end event, as the agent emits it.
type agentToolEnd struct{ name, args string }

func (e *agentToolEnd) event() *agent.ToolInvocation {
	return &agent.ToolInvocation{Event: agent.ToolInvocationEnd, Refused: true, ToolName: e.name, Arguments: e.args}
}
