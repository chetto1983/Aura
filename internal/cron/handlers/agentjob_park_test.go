package handlers

import (
	"context"
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

// AD-M8: a scheduled job has nobody to ask, so its gated call is refused with no_approver.
// It never pauses, so the handler never auto-rejects a pause or rebuilds an ask_user turn.
func TestAgentJobGatedCallIsRefusedNotParked(t *testing.T) {
	t.Parallel()
	store := &denyFactStore{}
	skill := &scheduledMutationTool{}
	registry := tools.NewRegistry()
	registry.Register(skill)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("g1", "skill_manage", `{"action":"delete","name":"obsolete-skill"}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("final", "text_response", `{"text":"Nobody could approve the delete."}`)),
	)
	handler := AgentJobHandler{Deps: AgentDeps{
		Client: fc, Registry: registry, Gateway: gateway.New(config.ProfileSingleUserHardened, store),
	}}
	summary, err := handler.Run(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), Job{
		Payload: []byte(`{"goal":"remove the obsolete skill"}`), StepBudget: 10,
		RunID: "run-gated", OriginConversationID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(summary, autoRejectMarker) || skill.effects != 0 {
		t.Fatalf("summary %q, effects %d; want no auto-rejected pause and no run", summary, skill.effects)
	}
	var refused bool
	for _, m := range fc.LastRequest().Messages {
		if m.Role == llm.RoleTool && m.ToolCallID == "g1" && strings.Contains(m.Content, "no interactive approver") {
			refused = true
		}
		for _, call := range m.ToolCalls {
			if call.Function.Name == "ask_user" {
				t.Fatalf("the job's history gained a rebuilt ask_user call: %+v", m)
			}
		}
	}
	if !refused {
		t.Fatal("the model never read the no-approver refusal")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.facts) != 1 || store.facts[0].Meta["reason"] != "no_approver" {
		t.Fatalf("deny facts = %+v, want one no_approver", store.facts)
	}
}
