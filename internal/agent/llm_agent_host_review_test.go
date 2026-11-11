package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// reviewerTool is a tool the host parks for review: it counts both the reviews it is asked
// for and the times it runs.
type reviewerTool struct {
	reviews, executions int
}

func (r *reviewerTool) Spec() tools.Spec {
	return tools.Spec{Name: "task", Mutating: true, Multiplexed: true}
}

func (r *reviewerTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	r.executions++
	return tools.ToolResult{Preview: "ran"}, nil
}

func (r *reviewerTool) HostReview(_ context.Context, _, toolCallID string, _ json.RawMessage) (*tools.ErrAwaitingUserInput, bool) {
	r.reviews++
	return &tools.ErrAwaitingUserInput{Question: "Review", Kind: tools.KindApproval, ToolCallID: toolCallID}, true
}

// denyPolicies denies every subject, the identity's policy store at its strictest.
type denyPolicies struct{}

func (denyPolicies) Get(context.Context, string, string, string) (gateway.Policy, bool, error) {
	return gateway.PolicyDeny, true, nil
}

func hostReviewAgent(t *testing.T) (*LlmAgent, *reviewerTool) {
	t.Helper()
	reviewer := &reviewerTool{}
	registry := tools.NewRegistry()
	registry.Register(reviewer)
	return newBareAgent(t, registry), reviewer
}

const reviewedArgs = `{"action":"schedule","kind":"message"}`

func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), "11111111-1111-4111-8111-111111111111")
}

func TestAHostReviewedCallParksWithoutExecuting(t *testing.T) {
	agent, reviewer := hostReviewAgent(t)
	call := toolCall("call-1", "task", reviewedArgs)
	pauses := agent.pauseCalls(gateway.WithResponder(ownerCtx()), []llm.ToolCall{call})
	if len(pauses) != 1 || pauses[0].pause.ToolCallID != call.ID || pauses[0].originalToolName != "task" {
		t.Fatalf("pauses = %+v", pauses)
	}
	if reviewer.executions != 0 {
		t.Fatalf("the parked call ran %d times", reviewer.executions)
	}
}

// Decision 21: the review needs a person in this conversation. A swarm worker inherits the
// responder marker and has nobody on it.
func TestAHostReviewedCallWithoutAResponderRunsNormally(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"no responder":       ownerCtx(),
		"delegated dispatch": tools.WithDelegatedDispatch(gateway.WithResponder(ownerCtx())),
	} {
		t.Run(name, func(t *testing.T) {
			agent, reviewer := hostReviewAgent(t)
			call := toolCall("call-1", "task", reviewedArgs)
			if pauses := agent.pauseCalls(ctx, []llm.ToolCall{call}); len(pauses) != 0 {
				t.Fatalf("parked: %+v", pauses)
			}
			agent.runTool(ctx, testBudget(t), call, time.Now())
			if reviewer.reviews != 0 || reviewer.executions != 1 {
				t.Fatalf("reviews %d, executions %d; want 0 and 1", reviewer.reviews, reviewer.executions)
			}
		})
	}
}

// Decision 31: a denied tool is never parked; it reaches the gateway, which refuses it.
func TestAPolicyDeniedTaskIsNeverParked(t *testing.T) {
	agent, reviewer := hostReviewAgent(t)
	gw := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{})
	gw.SetPolicyStore(denyPolicies{})
	agent.gateway = gw
	ctx := gateway.WithResponder(ownerCtx())
	call := toolCall("call-1", "task", reviewedArgs)
	if pauses := agent.pauseCalls(ctx, []llm.ToolCall{call}); len(pauses) != 0 {
		t.Fatalf("a denied call was parked: %+v", pauses)
	}
	run := agent.runTool(ctx, testBudget(t), call, time.Now())
	ti := agent.toolResultEvent(InvocationContext{}, [8]byte{}, run).Actions.ToolInvocation
	if reviewer.reviews != 0 || reviewer.executions != 0 || !ti.Refused {
		t.Fatalf("reviews %d, executions %d, refused %v; want 0, 0, true", reviewer.reviews, reviewer.executions, ti.Refused)
	}
}
