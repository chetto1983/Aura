package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
)

func TestToolResultEventPreservesCancellationOutcome(t *testing.T) {
	a := &LlmAgent{}
	meta := tools.ToolResultMeta{"cancelled": true}
	ev := a.toolResultEvent(InvocationContext{}, [8]byte{}, toolRunResult{
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
	ev := a.toolResultEvent(InvocationContext{}, [8]byte{}, toolRunResult{
		ToolCallID: "call", ToolName: "web_fetch", Result: tools.ToolResult{Meta: &meta},
	})
	ti := ev.Actions.ToolInvocation
	if ti.Status != "error" || ti.Error != "http_error status 403" {
		t.Fatalf("an inline web error was recorded as status=%q error=%q", ti.Status, ti.Error)
	}
	if ti.Refused {
		t.Fatal("an inline web error was marked as a refusal")
	}
}

// outcomeOf dispatches one call through runTool and returns the ToolInvocation the
// translator and the ledger both read.
func outcomeOf(t *testing.T, gw *gateway.Gateway, tool tools.Tool, args string) *ToolInvocation {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tool)
	a := &LlmAgent{registry: reg, gateway: gw, ledgerConvID: "conv-outcome"}
	run := a.runTool(context.Background(), testBudget(t), toolCall("c1", tool.Spec().Name, args), time.Now())
	return a.toolResultEvent(InvocationContext{}, [8]byte{}, run).Actions.ToolInvocation
}

// A refused call is still a failed call to the ledger, and it is also a refusal: the cockpit
// shows the operator's own rule at work, not a crash (spec 2026-10-12, finding 4).
func TestToolResultEventMarksAGatewayRefusal(t *testing.T) {
	spy := &spyMutatingTool{name: "skill_manage"}
	hardened := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{})
	ti := outcomeOf(t, hardened, spy, `{"action":"delete","name":"x"}`)
	if ti.Status != "error" || !ti.Refused || spy.count != 0 {
		t.Fatalf("refused call: status=%q refused=%v executions=%d", ti.Status, ti.Refused, spy.count)
	}
}

// A denial from the gateway's bookkeeping and a tool that failed while running are failures,
// never refusals.
func TestToolResultEventKeepsFailuresApartFromRefusals(t *testing.T) {
	brokenLedger := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{reserveErr: errors.New("insert boom")})
	for name, tc := range map[string]struct {
		ti   *ToolInvocation
		want string
	}{
		"failed reservation": {outcomeOf(t, brokenLedger, &spyMutatingTool{name: "shell_exec"}, `{"command":"ls"}`), "reservation failed"},
		"failed execution":   {outcomeOf(t, nil, failingTool{spec: tools.Spec{Name: "read_thing"}}, `{}`), "transport failed after send"},
	} {
		if tc.ti.Status != "error" || tc.ti.Refused || !strings.Contains(tc.ti.Error, tc.want) {
			t.Errorf("%s: status=%q refused=%v error=%q, want a %q failure that is not a refusal",
				name, tc.ti.Status, tc.ti.Refused, tc.ti.Error, tc.want)
		}
	}
}
