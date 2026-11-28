package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"go.uber.org/goleak"
)

// denyFactStore is the gateway's ledger seam: it keeps the facts the gateway writes and
// acquires every reservation.
type denyFactStore struct {
	mu    sync.Mutex
	facts []toolinvocations.Event
}

func (s *denyFactStore) Insert(_ context.Context, e toolinvocations.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = append(s.facts, e)
	return nil
}

func (*denyFactStore) Reserve(context.Context, toolinvocations.Event) (bool, *toolinvocations.Event, error) {
	return true, nil, nil
}

func (*denyFactStore) GetEnd(context.Context, string, string, string) (*toolinvocations.Event, error) {
	return nil, nil
}

func (s *denyFactStore) reasons() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []any
	for _, f := range s.facts {
		out = append(out, f.Meta["reason"])
	}
	return out
}

// gatedSkill is the Destructive skill_manage delete the gateway asks the operator about.
type gatedSkill struct {
	mu         sync.Mutex
	executions int
}

func (*gatedSkill) Spec() tools.Spec {
	return tools.Spec{
		Name: "skill_manage", Summary: "skill_manage", Description: "skill_manage",
		Parameters: json.RawMessage(`{"type":"object"}`), Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

func (g *gatedSkill) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.executions++
	return tools.ToolResult{Preview: "deleted"}, nil
}

// AD-M8: a swarm worker never parks. The gated call it makes is refused with no_approver,
// so the worker never reports needs_user_input and its history never gains the rebuilt
// ask_user call a pause would add.
func TestSwarmWorkerGatedCallIsRefusedNotParked(t *testing.T) {
	defer goleak.VerifyNone(t)
	store := &denyFactStore{}
	skill := &gatedSkill{}
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("g1", "skill_manage", `{"action":"delete","name":"obsolete-skill"}`)),
		agenttest.TextChunks("stop", "I could not delete it: nobody can approve it here."),
	)
	rc := testRunConfig(t, client, 20)
	rc.ParentRegistry.Register(skill)
	rc.Gateway = gateway.New(config.ProfileSingleUserHardened, store)
	rc.ConvID = "11111111-1111-4111-8111-111111111111"
	// A synchronous worker inherits the live turn's responder and identity, and has nobody on it.
	ctx := gateway.WithResponder(identityctx.WithIdentityID(context.Background(), "11111111-1111-4111-8111-111111111112"))

	report, history := runChild(ctx, rc, rc.ParentBudget, 0, "remove the obsolete skill")

	if report.Status != StatusOK || report.ToolCallID != "" || report.Question != "" {
		t.Fatalf("report = %+v, want ok with no pause", report)
	}
	for _, m := range history {
		for _, call := range m.ToolCalls {
			if call.Function.Name == "ask_user" {
				t.Fatalf("the worker's history gained a rebuilt ask_user call: %+v", m)
			}
		}
	}
	var refused bool
	for _, m := range history {
		if m.Role == llm.RoleTool && m.ToolCallID == "g1" && strings.Contains(m.Content, "no interactive approver") {
			refused = true
		}
	}
	if !refused || skill.executions != 0 {
		t.Fatalf("refused %v, executions %d; want the no-approver refusal and no run", refused, skill.executions)
	}
	if reasons := store.reasons(); len(reasons) != 1 || reasons[0] != "no_approver" {
		t.Fatalf("deny facts = %v, want one no_approver", reasons)
	}
}
