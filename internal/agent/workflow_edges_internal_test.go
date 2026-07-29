package agent

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// textOnceClient is a minimal llm.Client that streams a single text answer then a
// stop finish — enough to drive one agent turn without importing agenttest (which
// would close an import cycle in a white-box test).
type textOnceClient struct{ text string }

func (c textOnceClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 2)
	ch <- llm.Chunk{Text: c.text}
	ch <- llm.Chunk{FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// prefixMutatingHook rewrites messages[0] in BeforeModel, simulating a hook that
// busts the provider's cache-stable prefix.
type prefixMutatingHook struct{}

func (prefixMutatingHook) OnTurnStart(context.Context, HookTurn) error { return nil }
func (prefixMutatingHook) BeforeModel(_ context.Context, req *llm.Request) (*ModelHookResult, error) {
	if len(req.Messages) > 0 {
		req.Messages[0].Content += " (rewritten by hook)"
	}
	return nil, nil
}
func (prefixMutatingHook) BeforeTool(context.Context, llm.ToolCall) (*ToolHookResult, error) {
	return nil, nil
}
func (prefixMutatingHook) AfterTool(context.Context, llm.ToolCall, tools.ToolResult) (*ToolResultHookResult, error) {
	return nil, nil
}
func (prefixMutatingHook) OnTurnEnd(context.Context, HookTurn) error { return nil }

// AG-031: a BeforeModel hook that rewrites messages[0] busts the provider cache;
// the real agent Run loop detects the prefix drift after the hook and increments
// the prefix_drift metric (without changing messages[0] itself). This drives the
// actual wiring, not just the helper.
func TestPrefixDrift_DetectedAfterHookRewrite(t *testing.T) {
	before := metricInt(metrics.legacy.prefixDriftTotal)

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	a := NewLlmAgent(LlmAgentConfig{
		Client:      textOnceClient{text: "done"},
		LLM:         llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:    reg,
		PreviewCap:  2048,
		RunDir:      t.TempDir(),
		SessionID:   uuid.Must(uuid.NewV7()).String(),
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		HookManager: NewHookManager(prefixMutatingHook{}),
	})
	b, err := NewBudget(BudgetOptions{MaxSteps: new(4)})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b}

	for _, runErr := range a.Run(ic) {
		if runErr != nil {
			t.Fatalf("Run: %v", runErr)
		}
	}
	if got := metricInt(metrics.legacy.prefixDriftTotal) - before; got < 1 {
		t.Fatalf("prefix_drift metric delta = %d, want >=1 (AG-031 wiring)", got)
	}
}

// AG-038: TryReserve atomically removes the synthesis reserve from the shared
// pool; a concurrent ConsumeStep cannot spend the reserved steps, and Release
// returns them so the parent's synthesis turn can.
func TestBudgetTryReserveIsAtomic(t *testing.T) {
	b, err := NewBudget(BudgetOptions{MaxSteps: new(5)})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if !b.TryReserve(3) {
		t.Fatal("TryReserve(3) of a 5-step budget should succeed")
	}
	if got := b.Remaining(); got != 2 {
		t.Fatalf("after reserve Remaining = %d, want 2", got)
	}
	// Reserving more than what remains fails and leaves the pool unchanged.
	if b.TryReserve(3) {
		t.Fatal("TryReserve(3) with only 2 remaining should fail")
	}
	if got := b.Remaining(); got != 2 {
		t.Fatalf("failed reserve mutated the pool: Remaining = %d, want 2", got)
	}
	b.Release(3)
	if got := b.Remaining(); got != 5 {
		t.Fatalf("after release Remaining = %d, want 5", got)
	}
}
