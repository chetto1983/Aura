package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// The deliver-on-stop gate (amendment #192) reads the loop's edited paths, which the
// serial result loop records only for a Mutating tool named like a write tool, and its
// delivered paths, which only a send_file result carrying an artifact descriptor sets.
// These fakes reproduce exactly those two contracts without a sandbox.

type fakeWriteFile struct{}

func (fakeWriteFile) Spec() tools.Spec {
	return tools.Spec{Name: "write_file", Summary: "Fake write.", Description: "Fake write.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`), Mutating: true}
}

func (fakeWriteFile) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "written")
}

type fakeSendFile struct{}

func (fakeSendFile) Spec() tools.Spec {
	return tools.Spec{Name: "send_file", Summary: "Fake send.", Description: "Fake send.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}
}

func (fakeSendFile) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	res, err := tools.NewResult(ctx, "sent")
	if err != nil {
		return res, err
	}
	res.Meta = &tools.ToolResultMeta{"artifact": map[string]any{"name": "report.html"}}
	return res, nil
}

func newDeliveryAgent(t *testing.T, fc *agenttest.FakeClient) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(fakeWriteFile{})
	r.Register(fakeSendFile{})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30},
		Registry:   r,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		Workspace:  "/workspace",
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "fammi il report e mandamelo"}},
	})
}

const reportPath = "/workspace/artifacts/report.html"

// writeCall (llm_agent_verification_test.go) builds the write_file calls.
func sendCall(id, path string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "send_file", `{"path":"`+path+`"}`)
}

func deliveryNudge(t *testing.T, req llm.Request) llm.Message {
	t.Helper()
	// The loop appends its context block (<budget>, <workspace>) after history, so the
	// nudge is the last message BEFORE it: scan rather than index.
	for _, msg := range req.Messages {
		if strings.HasPrefix(msg.Content, "[System: You wrote "+reportPath) {
			return msg
		}
	}
	t.Fatalf("no delivery nudge in the retry request: %+v", req.Messages)
	return llm.Message{}
}

func countDiscards(evs []*agent.Event) int {
	n := 0
	for _, ev := range evs {
		if ev.Actions.DiscardStreamed {
			n++
		}
	}
	return n
}

// The shape measured on Telegram: the model writes the artifact, answers with its path
// and stops. The gate repudiates that draft, nudges once as the user, and the run ends
// on the answer that follows the send_file.
func TestDeliveryGate_ContentStop_NudgesUntilSent(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", reportPath)),
		agenttest.TextChunks("stop", "Ecco il report: ", reportPath),
		agenttest.ToolCallTurn(sendCall("c3", reportPath)),
		agenttest.TextChunks("stop", "Consegnato."),
	)
	a := newDeliveryAgent(t, fc)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 4 {
		t.Fatalf("model calls = %d, want 4 (write, draft, send, answer)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "Consegnato." {
		t.Fatalf("final = %q, want the post-delivery answer", got)
	}
	if nudge := deliveryNudge(t, fc.RecordedRequests()[2]); nudge.Role != llm.RoleUser {
		t.Fatalf("content-stop nudge role = %s, want user", nudge.Role)
	}
	if n := countDiscards(evs); n != 1 {
		t.Fatalf("DiscardStreamed events = %d, want exactly one for the vetoed draft", n)
	}
}

// At the text_response seam the nudge must answer the terminal call as a tool result,
// or the wire would carry an unanswered tool_call.
func TestDeliveryGate_TextResponse_NudgeRidesToolResult(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", "artifacts/report.html")),
		agenttest.ToolCallTurn(textResponseCall("c2", "Fatto, è in artifacts/report.html")),
		agenttest.ToolCallTurn(sendCall("c3", reportPath)),
		agenttest.ToolCallTurn(textResponseCall("c4", "Consegnato.")),
	)
	a := newDeliveryAgent(t, fc)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := finalContent(t, evs); got != "Consegnato." {
		t.Fatalf("final = %q (calls=%d)", got, fc.CallCount())
	}
	nudge := deliveryNudge(t, fc.RecordedRequests()[2])
	if nudge.Role != llm.RoleTool || nudge.ToolCallID != "c2" {
		t.Fatalf("terminal nudge = role %s call %q, want tool result on c2", nudge.Role, nudge.ToolCallID)
	}
}

// Delivered in the same turn, or nothing under artifacts/ at all: the gate is silent
// and the run is byte-identical to before the amendment.
func TestDeliveryGate_SilentWhenSentOrNotAnArtifact(t *testing.T) {
	for name, script := range map[string][]agenttest.FakeTurn{
		"sent": {
			agenttest.ToolCallTurn(writeCall("c1", reportPath), sendCall("c2", reportPath)),
			agenttest.TextChunks("stop", "Consegnato."),
		},
		"script only": {
			agenttest.ToolCallTurn(writeCall("c1", "/workspace/scripts/fetch.py")),
			agenttest.TextChunks("stop", "Script pronto."),
		},
	} {
		t.Run(name, func(t *testing.T) {
			fc := agenttest.NewFakeClient(script...)
			a := newDeliveryAgent(t, fc)
			evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if fc.CallCount() != 2 || countDiscards(evs) != 0 {
				t.Fatalf("calls=%d discards=%d, want 2 and 0", fc.CallCount(), countDiscards(evs))
			}
		})
	}
}

// One nudge per run: a model that answers the nudge in prose without sending is not
// nudged again — the gate has said its piece and the turn ends on that answer.
func TestDeliveryGate_OneNudgeThenAcceptsExplanation(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", reportPath)),
		agenttest.TextChunks("stop", "Ecco: ", reportPath),
		agenttest.TextChunks("stop", "È un file intermedio, non serve inviarlo."),
	)
	a := newDeliveryAgent(t, fc)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 3 || !strings.HasPrefix(finalContent(t, evs), "È un file intermedio") {
		t.Fatalf("calls=%d final=%q", fc.CallCount(), finalContent(t, evs))
	}
}
