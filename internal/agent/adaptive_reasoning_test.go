package agent

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type reasoningControlProbe struct {
	mu         sync.Mutex
	order      *[]string
	inputs     []ReasoningControlInput
	action     ReasoningAction
	failAt     string
	deliveries int
}

func (control *reasoningControlProbe) DecideAndDeliverReasoning(
	ctx context.Context,
	input ReasoningControlInput,
	learned ReasoningRequestBuilder,
	static StaticReasoningRequestBuilder,
) (PreparedReasoningRequest, error) {
	control.mu.Lock()
	control.inputs = append(control.inputs, input)
	if control.order != nil {
		*control.order = append(*control.order, "select")
	}
	control.mu.Unlock()
	if control.failAt == "selection" {
		return static(ctx)
	}
	if control.order != nil {
		*control.order = append(*control.order, "assignment")
	}
	if control.failAt == "assignment" {
		return static(ctx)
	}
	prepared, err := learned(ctx, control.action)
	if err != nil {
		return static(ctx)
	}
	if control.failAt == "delivery" {
		return static(ctx)
	}
	control.mu.Lock()
	control.deliveries++
	if control.order != nil {
		*control.order = append(*control.order, "delivery")
	}
	control.mu.Unlock()
	return prepared, nil
}

func (control *reasoningControlProbe) snapshot() ([]ReasoningControlInput, int) {
	control.mu.Lock()
	defer control.mu.Unlock()
	return slices.Clone(control.inputs), control.deliveries
}

type reasoningOrderHook struct {
	order  *[]string
	mutate func(*llm.Request)
	err    error
}

func (hook reasoningOrderHook) OnTurnStart(context.Context, HookTurn) error { return nil }
func (hook reasoningOrderHook) BeforeModel(
	_ context.Context,
	request *llm.Request,
) (*ModelHookResult, error) {
	if hook.order != nil {
		*hook.order = append(*hook.order, "transform")
	}
	if hook.mutate != nil {
		hook.mutate(request)
	}
	return nil, hook.err
}
func (hook reasoningOrderHook) BeforeTool(
	context.Context,
	llm.ToolCall,
) (*ToolHookResult, error) {
	return nil, nil
}
func (hook reasoningOrderHook) AfterTool(
	context.Context,
	llm.ToolCall,
	tools.ToolResult,
) (*ToolResultHookResult, error) {
	return nil, nil
}
func (hook reasoningOrderHook) OnTurnEnd(context.Context, HookTurn) error { return nil }

type reasoningOrderClient struct {
	order *[]string
	turns []modelRoundTurn
	next  int
}

func (client *reasoningOrderClient) Stream(
	_ context.Context,
	_ llm.Request,
) (<-chan llm.Chunk, error) {
	*client.order = append(*client.order, "stream")
	turn := client.turns[client.next]
	client.next++
	channel := make(chan llm.Chunk, len(turn.chunks))
	for _, chunk := range turn.chunks {
		channel <- chunk
	}
	close(channel)
	return channel, turn.err
}

func TestAdaptiveReasoningOrdersEveryPrimaryRound(t *testing.T) {
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	var order []string
	control := &reasoningControlProbe{
		order: &order, action: ReasoningActionLow,
	}
	client := &reasoningOrderClient{
		order: &order,
		turns: []modelRoundTurn{
			modelRoundToolTurn(modelRoundToolCall("call-1", "model_round_probe", `{}`)),
			modelRoundTextTurn("stop", "done"),
		},
	}
	agent := newReasoningControlledAgent(
		t, client, control, NewHookManager(reasoningOrderHook{order: &order}),
		llm.ReasoningEffortLow,
	)

	runReasoningControlledAgent(t, agent, ownerID, requestID, 3)

	wantOrder := []string{
		"select", "assignment", "transform", "delivery", "stream",
		"select", "assignment", "transform", "delivery", "stream",
	}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	inputs, deliveries := control.snapshot()
	if len(inputs) != 2 || deliveries != 2 {
		t.Fatalf("rounds = %d inputs/%d deliveries, want 2/2", len(inputs), deliveries)
	}
	for index, input := range inputs {
		if input.RequestID != requestID || input.PointOrdinal != uint32(index+1) {
			t.Fatalf("round %d identity = %s/%d", index, input.RequestID, input.PointOrdinal)
		}
		if input.Override != llm.ReasoningEffortLow {
			t.Fatalf("round %d override = %q, want low", index, input.Override)
		}
	}
}

func TestAdaptiveReasoningTransportRetryReusesPreparedDelivery(t *testing.T) {
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	control := &reasoningControlProbe{action: ReasoningActionHigh}
	client := &modelRoundCaptureClient{turns: []modelRoundTurn{
		modelRoundTextErrorTurn(errors.New("connection reset by peer"), "discard"),
		modelRoundTextTurn("stop", "recovered"),
	}}
	agent := newReasoningControlledAgent(
		t, client, control, nil, llm.ReasoningEffortHigh,
	)

	runReasoningControlledAgent(t, agent, ownerID, requestID, 3)

	inputs, deliveries := control.snapshot()
	if len(inputs) != 1 || deliveries != 1 {
		t.Fatalf("retry produced inputs=%d deliveries=%d, want 1/1", len(inputs), deliveries)
	}
	rounds, present := client.snapshot()
	if len(rounds) != 2 || !present[0] || !present[1] || rounds[0] != rounds[1] {
		t.Fatalf("retry rounds = %+v present=%v, want one stable identity", rounds, present)
	}
}

func TestAdaptiveReasoningFallsBackAtPersistenceAndValidationBoundaries(t *testing.T) {
	ownerID := uuid.Must(uuid.NewV7())
	tests := []struct {
		name   string
		failAt string
		mutate func(*llm.Request)
	}{
		{name: "selection", failAt: "selection"},
		{name: "assignment", failAt: "assignment"},
		{name: "delivery", failAt: "delivery"},
		{name: "final validation", mutate: func(request *llm.Request) { request.MaxTokens++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			control := &reasoningControlProbe{
				action: ReasoningActionHigh, failAt: test.failAt,
			}
			hooks := (*HookManager)(nil)
			if test.mutate != nil {
				calls := 0
				hooks = NewHookManager(reasoningOrderHook{
					mutate: func(request *llm.Request) {
						calls++
						if calls == 1 {
							test.mutate(request)
						}
					},
				})
			}
			client := &modelRoundCaptureClient{turns: []modelRoundTurn{
				modelRoundTextTurn("stop", "static"),
			}}
			agent := newReasoningControlledAgent(
				t, client, control, hooks, llm.ReasoningEffortLow,
			)

			runReasoningControlledAgent(
				t, agent, ownerID, uuid.Must(uuid.NewV7()), 2,
			)

			if client.next != 1 {
				t.Fatalf("static stream calls = %d, want 1", client.next)
			}
			_, deliveries := control.snapshot()
			if deliveries != 0 {
				t.Fatalf("failed boundary recorded %d deliveries", deliveries)
			}
		})
	}
}

func TestAdaptiveReasoningSurfacesStaticFailure(t *testing.T) {
	ownerID := uuid.Must(uuid.NewV7())
	staticErr := errors.New("static request failed")
	control := &reasoningControlProbe{
		action: ReasoningActionHigh, failAt: "selection",
	}
	agent := newReasoningControlledAgent(
		t,
		&modelRoundCaptureClient{},
		control,
		NewHookManager(reasoningOrderHook{err: staticErr}),
		llm.ReasoningEffortLow,
	)
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	invocation := InvocationContext{
		Ctx:   identityctx.WithIdentityID(t.Context(), ownerID.String()),
		Agent: agent, RequestID: uuid.Must(uuid.NewV7()), Branch: "root",
		Budget: budget,
	}
	var got error
	for _, runErr := range agent.Run(invocation) {
		got = runErr
	}
	if !errors.Is(got, staticErr) {
		t.Fatalf("Run error = %v, want %v", got, staticErr)
	}
}

func TestAdaptiveReasoningFallbackIsExactlyStatic(t *testing.T) {
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	agent := newReasoningControlledAgent(
		t,
		&modelRoundCaptureClient{},
		&reasoningControlProbe{
			action: ReasoningActionHigh, failAt: "selection",
		},
		nil,
		llm.ReasoningEffortLow,
	)
	ctx := identityctx.WithIdentityID(t.Context(), ownerID.String())
	budget := prompt.Budget{
		Used: 1, Remaining: 2, Workspace: "workspace",
		CurrentTime: "2026-07-26T12:00:00Z", Today: "2026-07-26",
	}
	static, err := agent.prepareStaticRequest(
		ctx, budget, prompt.ReasoningTierHigh, true, requestID,
	)
	if err != nil {
		t.Fatalf("prepareStaticRequest: %v", err)
	}
	fallback, err := agent.prepareReasoningRequest(
		ctx,
		budget,
		modelRound{requestID: requestID, ordinal: 1},
		prompt.ReasoningTierHigh,
		true,
	)
	if err != nil {
		t.Fatalf("prepareReasoningRequest: %v", err)
	}
	if !reflect.DeepEqual(fallback, static) {
		t.Fatalf("fallback request differs from static:\n got: %+v\nwant: %+v", fallback, static)
	}
}

func newReasoningControlledAgent(
	t *testing.T,
	client llm.Client,
	control ReasoningControl,
	hooks *HookManager,
	override llm.ReasoningEffort,
) *LlmAgent {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(modelRoundProbeTool{})
	return NewLlmAgent(LlmAgentConfig{
		Client: client,
		LLM: llm.Config{
			Model: "test-model", Provider: "openrouter",
			MaxTokens: 256, TotalTimeoutSec: 30, AdaptiveReasoning: true,
		},
		Registry: registry, PreviewCap: 1024, RunDir: t.TempDir(),
		SessionID:   uuid.Must(uuid.NewV7()).String(),
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: "run"}},
		HookManager: hooks, ReasoningOverride: override,
		ReasoningControl: control,
	})
}

func runReasoningControlledAgent(
	t *testing.T,
	agent *LlmAgent,
	ownerID uuid.UUID,
	requestID uuid.UUID,
	maxSteps int,
) {
	t.Helper()
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	invocation := InvocationContext{
		Ctx:   identityctx.WithIdentityID(t.Context(), ownerID.String()),
		Agent: agent, RequestID: requestID, Branch: "root", Budget: budget,
	}
	for _, runErr := range agent.Run(invocation) {
		if runErr != nil {
			t.Fatalf("Run: %v", runErr)
		}
	}
}
