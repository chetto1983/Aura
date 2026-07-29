package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

type scheduledMutationTool struct{ effects int }

func (s *scheduledMutationTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "skill", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

func (s *scheduledMutationTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.effects++
	return tools.ToolResult{Preview: "restored"}, nil
}

type scheduledOperationRegistry struct {
	request *idempotency.BeginRequest
	replay  *idempotency.ReplayResult
}

func (s *scheduledOperationRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	if s.request == nil {
		copyRequest := request
		s.request = &copyRequest
		return idempotency.BeginDecision{
			Decision: idempotency.DecisionAcquired, ClaimToken: 1,
		}, nil
	}
	if s.request.Operation != request.Operation || s.request.Fingerprint != request.Fingerprint {
		return idempotency.BeginDecision{Decision: idempotency.DecisionConflict}, idempotency.ErrConflict
	}
	if s.replay == nil {
		return idempotency.BeginDecision{}, errors.New("scheduled operation completed without replay")
	}
	return idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: s.replay}, nil
}

func (s *scheduledOperationRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	result := request.Result
	s.replay = &result
	return nil
}

func (*scheduledOperationRegistry) MarkIndeterminate(
	context.Context,
	idempotency.OperationKey,
	[32]byte,
	idempotency.ClaimToken,
) error {
	return nil
}

// loopTool is a trivial non-terminal tool the budget-inherit test drives the agent
// into: each call returns a small result, so the agent keeps looping (consuming the
// shared budget) until the budget gate trips. It is dedup-safe in the test because
// the scripted calls carry a unique counter arg per turn.
type loopTool struct{}

func (loopTool) Spec() tools.Spec {
	return tools.Spec{
		Name:       "loop",
		Summary:    "test loop tool",
		Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`),
	}
}

func (loopTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Preview: "ok", Bytes: 2}, nil
}

// jobRegistry builds a registry with the loop tool + ask_user + swarm_spawn so the
// childRegistry drop (swarm_spawn) and keep (ask_user) are both exercised.
func jobRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(loopTool{})
	r.Register(tools.AskUser{})
	r.Register(&tools.SwarmSpawn{})
	return r
}

// loopTurns scripts n tool-call turns each invoking loop with a unique arg (so the
// agent's dedup ring never trips), followed by a terminal text_response.
func loopTurns(n int) []agenttest.FakeTurn {
	turns := make([]agenttest.FakeTurn, 0, n+1)
	for i := range n {
		turns = append(turns, agenttest.ToolCallTurn(
			agenttest.MakeToolCall(fmt.Sprintf("c%d", i), "loop", fmt.Sprintf(`{"n":%d}`, i)),
		))
	}
	turns = append(turns, agenttest.ToolCallTurn(
		agenttest.MakeToolCall("final", "text_response", `{"text":"done"}`),
	))
	return turns
}

func TestAgentJobBudgetInherit(t *testing.T) {
	t.Parallel()
	// Script far more turns than either budget so the budget gate — not the script —
	// is what stops the run.
	fc := agenttest.NewFakeClient(loopTurns(60)...)
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}

	_, err := h.Run(context.Background(), Job{
		Payload:    []byte(`{"goal":"do the thing"}`),
		StepBudget: 10,
		RunID:      "run-budget",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// With step_budget=10 the agent consumes 10 steps then routes through one recovery
	// nudge + one finalize (max_steps+2 ceiling), so the LLM is called at most ~13
	// times — emphatically NOT the 25+ a fresh default budget would allow.
	if got := fc.CallCount(); got > 14 {
		t.Fatalf("step_budget=10 should cap LLM calls near 12 (max_steps+2), got %d — budget not inherited from the row", got)
	}
	if got := fc.CallCount(); got < 10 {
		t.Fatalf("expected at least 10 LLM calls before the budget tripped, got %d", got)
	}
}

func TestAgentJobStrictMutationDerivesStableChildAndReplays(t *testing.T) {
	t.Parallel()

	mutation := &scheduledMutationTool{}
	registry := tools.NewRegistry()
	registry.Register(mutation)
	operations := &scheduledOperationRegistry{}
	policy := gateway.New(config.ProfileSingleUserHardened, nil)
	policy.SetOperationRegistry(operations)
	turns := func() *agenttest.FakeClient {
		return agenttest.NewFakeClient(
			agenttest.ToolCallTurn(agenttest.MakeToolCall("mut-1", "skill", `{"action":"restore","name":"calc"}`)),
			agenttest.ToolCallTurn(agenttest.MakeToolCall("final", "text_response", `{"text":"done"}`)),
		)
	}
	parentFingerprint, err := idempotency.FingerprintTyped(struct {
		Task string `json:"task"`
		Run  string `json:"run"`
	}{Task: "task-39", Run: "run-39"})
	if err != nil {
		t.Fatal(err)
	}
	parent := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      idempotency.ScopeSchedulerRun,
			Key:        "task-39:run-39",
		},
		Fingerprint: parentFingerprint,
		Correlation: "run-39",
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), parent)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{
		Payload: []byte(`{"goal":"restore the calculator skill"}`), StepBudget: 10,
		RunID: "run-39", OriginConversationID: "11111111-1111-1111-1111-111111111111",
	}
	for attempt := range 2 {
		handler := AgentJobHandler{Deps: AgentDeps{Client: turns(), Registry: registry, Gateway: policy}}
		if _, err := handler.Run(ctx, job); err != nil {
			t.Fatalf("agent_job attempt %d: %v", attempt+1, err)
		}
	}
	if mutation.effects != 1 {
		t.Fatalf("mutating effects = %d, want one across scheduler retry", mutation.effects)
	}
	if operations.request == nil || operations.request.Operation.Scope != idempotency.ScopeAgentTool {
		t.Fatalf("child operation = %+v, want agent.tool scope", operations.request)
	}
	if operations.request.Operation.Key == parent.Key.Key || strings.Contains(operations.request.Operation.Key, parent.Key.Key) {
		t.Fatal("scheduler child reused or exposed its parent key")
	}
}

func TestAgentJobBudgetDefaultWhenRowUnset(t *testing.T) {
	t.Parallel()
	// step_budget=0 → fall through to the builtin default (25). The agent should make
	// well more than the 13 calls the step_budget=10 case caps at, proving the row
	// value (not a hardcoded 10) drives the budget.
	fc := agenttest.NewFakeClient(loopTurns(60)...)
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}

	if _, err := h.Run(context.Background(), Job{
		Payload: []byte(`{"goal":"do the thing"}`),
		RunID:   "run-default",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := fc.CallCount(); got <= 14 {
		t.Fatalf("default budget (25) should allow >14 LLM calls, got %d", got)
	}
}

func TestAskUserAutoReject(t *testing.T) {
	t.Parallel()
	// Turn 1: the model invokes ask_user (a pause). The handler must auto-reject and
	// re-run; Turn 2: the model finalizes with text_response.
	askCall := agenttest.MakeToolCall("ask-1", "ask_user", `{"question":"continue?","kind":"approval"}`)
	finalCall := agenttest.MakeToolCall("final", "text_response", `{"text":"proceeding without you"}`)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askCall),
		agenttest.ToolCallTurn(finalCall),
	)
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}

	done := make(chan struct{})
	var summary string
	var runErr error
	go func() {
		summary, runErr = h.Run(context.Background(), Job{
			Payload:    []byte(`{"goal":"check the market"}`),
			StepBudget: 10,
			RunID:      "run-askuser",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("agent_job blocked on ask_user — must auto-reject and never block (D-25)")
	}
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if !strings.Contains(summary, autoRejectMarker) {
		t.Fatalf("summary missing the auto-rejected marker (D-25 audit trail): %q", summary)
	}
	if !strings.Contains(summary, "proceeding without you") {
		t.Fatalf("expected the post-rejection final answer in the summary, got %q", summary)
	}
}

func TestChildRegistryDropsSwarmKeepsAskUser(t *testing.T) {
	t.Parallel()
	child := childRegistry(jobRegistry())
	if _, ok := child.Get("swarm_spawn"); ok {
		t.Fatal("agent_job child registry must DROP swarm_spawn (D-13)")
	}
	if _, ok := child.Get("ask_user"); !ok {
		t.Fatal("agent_job child registry must KEEP ask_user (D-25 — the model sees the rejection)")
	}
	if _, ok := child.Get("loop"); !ok {
		t.Fatal("agent_job child registry must keep non-swarm tools")
	}
}

func TestAgentJobMissingGoal(t *testing.T) {
	t.Parallel()
	fc := agenttest.NewFakeClient()
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}
	if _, err := h.Run(context.Background(), Job{Payload: []byte(`{}`), RunID: "r"}); err == nil {
		t.Fatal("expected an error for an agent_job payload with no goal")
	}
}

func TestAgentJobMeta(t *testing.T) {
	t.Parallel()
	h := AgentJobHandler{}
	m := h.Meta()
	if m.Kind != KindAgentJob {
		t.Fatalf("kind = %q, want agent_job", m.Kind)
	}
	if !m.ReschedulesOnRecovery {
		t.Fatal("agent_job should reschedule on recovery (D-18)")
	}
	if m.MaxDuration != agentJobMaxDuration {
		t.Fatalf("default MaxDuration = %v, want %v", m.MaxDuration, agentJobMaxDuration)
	}
}
