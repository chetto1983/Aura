package agent

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// buildReqRegistry mirrors the production manifest closely enough to exercise
// RenderToolDefs through the builder.
func buildReqRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	return r
}

func newBuildReqAgent(t *testing.T, cfg llm.Config) *LlmAgent {
	t.Helper()
	return NewLlmAgent(LlmAgentConfig{
		LLM:       cfg,
		Registry:  buildReqRegistry(),
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
}

// TestBuildRequest_BranchParity pins the post-refactor invariant: buildRequest
// runs EXACTLY ONE builder per branch and its output is byte-identical to the
// corresponding old chosen request — BuildWithReasoningTier when adaptiveTierOK,
// Build otherwise. The discarded Build() (a wasted RenderToolDefs() per turn,
// QUAL-02/T7) is eliminated without changing the emitted request.
func TestBuildRequest_BranchParity(t *testing.T) {
	// openrouter + AdaptiveReasoning makes the two builders genuinely diverge
	// (the tier sets req.Reasoning), so the parity assertions are discriminating
	// rather than trivially equal.
	cfg := llm.Config{
		Model:             "test-model",
		Provider:          "openrouter",
		Temperature:       0.4,
		MaxTokens:         256,
		AdaptiveReasoning: true,
	}
	a := newBuildReqAgent(t, cfg)

	budget := prompt.Budget{
		Used:      1,
		Remaining: 9,
		Workspace: a.workspace,
	}
	tier := prompt.ReasoningTierHigh

	// adaptiveTierOK == true → must equal BuildWithReasoningTier output.
	gotTier := a.buildRequest(budget, tier, true)
	wantTier := a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, tier, a.activated)
	if !reflect.DeepEqual(gotTier, wantTier) {
		t.Fatalf("tierOK branch: buildRequest != BuildWithReasoningTier\n got=%+v\nwant=%+v", gotTier, wantTier)
	}

	// adaptiveTierOK == false → must equal plain Build output.
	gotPlain := a.buildRequest(budget, tier, false)
	wantPlain := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.activated)
	if !reflect.DeepEqual(gotPlain, wantPlain) {
		t.Fatalf("non-tier branch: buildRequest != Build\n got=%+v\nwant=%+v", gotPlain, wantPlain)
	}

	// Guard against a trivially-passing test: with adaptive reasoning ON the two
	// branches MUST differ (the tier branch projects req.Reasoning). If they were
	// equal the parity checks above could not distinguish a wrong builder choice.
	if reflect.DeepEqual(gotTier, gotPlain) {
		t.Fatal("expected the tier and non-tier branches to diverge (req.Reasoning), but they were identical")
	}
	if (gotTier.Reasoning != llm.ReasoningConfig{}) == (gotPlain.Reasoning != llm.ReasoningConfig{}) {
		t.Fatalf("only the tier branch should set req.Reasoning: tier=%+v plain=%+v", gotTier.Reasoning, gotPlain.Reasoning)
	}
}
