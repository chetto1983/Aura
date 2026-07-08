//go:build live_e2e

// adaptive_reasoning_live_e2e_test.go is the AUTONOMOUS, real-LLM regression guard
// for Aura's adaptive-reasoning projection. It locks the three provider contracts
// that the production path depends on but that the unit/wire tests cannot prove —
// they assert the request SHAPE, this asserts the live DeepSeek-via-OpenRouter
// BEHAVIOR. Each contract was established by controlled probing on 2026-06-11
// (scripts/deepseek_reasoning_probe.py + throwaway controls); this test freezes
// those findings so a provider drift that breaks them fails loudly instead of
// silently degrading every turn.
//
// Verified contracts (controlled, same-prompt comparisons):
//
//	C1 NONE-tier off-switch     reasoning.effort:"none" => ZERO reasoning tokens AND a
//	                            non-empty answer (genuine suppression, no starvation).
//	                            The native DeepSeek `thinking:{type:disabled}` toggle is
//	                            NOT forwarded by OpenRouter — effort:none is the only
//	                            off-switch, which is exactly what the NONE tier projects.
//	C2 HIGH-tier no-starvation  effort:"high"+exclude+max_tokens=4096 on a hard prompt
//	                            => finish_reason "stop" AND a non-empty answer. max_tokens
//	                            does NOT bound reasoning under exclude (probe: reasoning ran
//	                            to ~8351 tokens past the 4096 cap and still answered), so a
//	                            deep turn is never truncated mid-thought.
//	C3 ROUTER off-switch        the tier classifier's reasoning:{enabled:false}+max_tokens:32
//	                            => ZERO reasoning tokens AND non-empty content. Without it the
//	                            32-token budget is spent entirely on reasoning (finish "length",
//	                            empty content) and the router fails to every-turn fallback.
//
// Like the other live_e2e tiers this is a PAID, OPERATOR-AUTHORIZED gate behind the
// live_e2e build tag — never default CI/Makefile quality. It needs ONLY the paid key
// (no Postgres): with OPENROUTER_API_KEY unset it t.Skips (the one legitimate local
// skip). Reasoning text is never asserted on or printed — only token presence,
// finish_reason, and answer length.
//
// REPRODUCE:
//
//	cp /d/Aura/.env ./.env
//	set -a; . ./.env; set +a
//	go test -tags live_e2e -run TestLiveReasoning -timeout 600s -v ./internal/llm/openai_compat/
package openai_compat

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// liveModel is the production default; an operator can repoint it with AURA_LLM_MODEL
// to validate the same contracts against a different OpenRouter reasoning model.
const liveModel = "deepseek/deepseek-v4-flash:nitro"

// hardReasoningPrompt reliably triggers heavy reasoning (~4k-8k reasoning tokens in
// the 2026-06-11 probe) so the C2 no-starvation assertion is meaningful rather than
// trivially passing on a one-shot answer.
const hardReasoningPrompt = "Hai tre brocche da 12, 7 e 5 litri; solo quella da 12 e' piena. " +
	"Travasando senza misurini ottieni esattamente 6 litri in una brocca: " +
	"elenca la sequenza minima di travasi, poi una frase di conferma."

// streamResult is the drained, reasoning-text-free summary of one live stream.
type streamResult struct {
	reasoningChars int
	contentChars   int
	finishReason   string
}

// requireLiveLLM skips when the paid key is unset (the one legitimate local skip —
// this tier is PAID and never runs in CI, so there is no no-skip-as-green obligation).
func requireLiveLLM(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("live_e2e: OPENROUTER_API_KEY unset — PAID gate, not CI. " +
			"Run: cp /d/Aura/.env ./.env; set -a; . ./.env; set +a; " +
			"go test -tags live_e2e -run TestLiveReasoning -timeout 600s -v ./internal/llm/openai_compat/")
	}
}

// liveCfg builds the production-shaped config from the env key, defaulting the model
// to liveModel unless AURA_LLM_MODEL repoints it.
func liveCfg() llm.Config {
	model := os.Getenv("AURA_LLM_MODEL")
	if model == "" {
		model = liveModel
	}
	return llm.Config{
		Provider:          "openrouter",
		BaseURL:           "https://openrouter.ai/api/v1",
		Model:             model,
		APIKey:            os.Getenv("OPENROUTER_API_KEY"),
		Temperature:       0,
		MaxTokens:         4096,
		AdaptiveReasoning: true,
		ConnectTimeoutSec: 10,
		Headers:           map[string]string{"HTTP-Referer": "https://github.com/chetto1983/aura", "X-Title": "Aura"},
	}
}

// streamLive runs req through the REAL client and drains the channel into a
// reasoning-text-free summary. A terminal stream error fails the test.
func streamLive(t *testing.T, cfg llm.Config, req llm.Request, timeout time.Duration) streamResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ch, err := New(cfg).Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var res streamResult
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("stream chunk error: %v", c.Err)
		}
		res.reasoningChars += len(c.Reasoning)
		res.contentChars += len(c.Text)
		if c.FinishReason != "" {
			res.finishReason = c.FinishReason
		}
	}
	return res
}

// C1: the NONE tier (effort:none) suppresses reasoning entirely yet still answers.
func TestLiveReasoning_NoneTierOffSwitch(t *testing.T) {
	requireLiveLLM(t)
	cfg := liveCfg()
	builder := prompt.NewPromptBuilder()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})

	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "Rispondi in italiano. Mantieni la risposta finale breve."},
		{Role: llm.RoleUser, Content: "ciao"},
	}
	req := builder.BuildWithReasoningTier(history, reg, cfg.Provider, cfg, prompt.Budget{}, prompt.ReasoningTierNone, nil)

	res := streamLive(t, cfg, req, 60*time.Second)
	if res.reasoningChars != 0 {
		t.Fatalf("NONE tier leaked reasoning: got %d reasoning chars, want 0 (effort:none must suppress)", res.reasoningChars)
	}
	if res.contentChars == 0 {
		t.Fatalf("NONE tier produced no answer (finish=%q) — off-switch starved the response", res.finishReason)
	}
}

// C2: the HIGH tier completes a deep reasoning turn at max_tokens=4096 — reasoning
// runs past the cap (exclude=true) without truncating the answer.
func TestLiveReasoning_HighTierNoStarvation(t *testing.T) {
	requireLiveLLM(t)
	cfg := liveCfg()
	builder := prompt.NewPromptBuilder()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})

	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "Rispondi in italiano. Mantieni la risposta finale breve."},
		{Role: llm.RoleUser, Content: hardReasoningPrompt},
	}
	req := builder.BuildWithReasoningTier(history, reg, cfg.Provider, cfg, prompt.Budget{}, prompt.ReasoningTierHigh, nil)
	if req.MaxTokens != 4096 {
		t.Fatalf("precondition: HIGH tier projected max_tokens=%d, want 4096 (the starvation-risk default)", req.MaxTokens)
	}

	res := streamLive(t, cfg, req, 300*time.Second)
	if res.finishReason != "stop" {
		t.Fatalf("HIGH tier did not finish cleanly on a deep turn: finish=%q (a max_tokens-bounded reasoning regression shows as %q)", res.finishReason, "length")
	}
	if res.contentChars == 0 {
		t.Fatalf("HIGH tier reasoned but produced no answer — reasoning starved the response at max_tokens=%d", req.MaxTokens)
	}
}

// C3: the tier router's reasoning:{enabled:false} suppresses reasoning while still
// emitting the JSON classification — the off-switch the every-turn router relies on.
func TestLiveReasoning_RouterEnabledFalseOffSwitch(t *testing.T) {
	requireLiveLLM(t)
	cfg := liveCfg()
	disabled := false
	// Mirrors internal/agent.adaptiveReasoningTier's hand-built router request shape.
	req := llm.Request{
		Model: cfg.Model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "Classify the latest user request into one reasoning tier for the next assistant turn. Reply only JSON: {\"tier\":\"none\"|\"low\"|\"high\"}."},
			{Role: llm.RoleUser, Content: hardReasoningPrompt},
		},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &disabled},
		ToolChoice:  "none",
	}

	res := streamLive(t, cfg, req, 60*time.Second)
	if res.reasoningChars != 0 {
		t.Fatalf("router off-switch failed: got %d reasoning chars at enabled:false (would burn the 32-token budget and break classification)", res.reasoningChars)
	}
	if res.contentChars == 0 {
		t.Fatalf("router produced no classification text (finish=%q) — reasoning starved the 32-token budget", res.finishReason)
	}
}
