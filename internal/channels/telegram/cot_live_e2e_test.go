//go:build live_e2e

// cot_live_e2e_test.go is the AUTONOMOUS, real-LLM end-to-end proof for the Telegram
// live chain-of-thought feature. It exercises the WHOLE data plane the unit tests
// cannot, because three layers had to align for a single byte of reasoning to reach
// the user and each was independently gated:
//
//  1. the adaptive-reasoning POLICY must request exclude:false (cfg.ShowReasoning) —
//     exclude:true yields ZERO reasoning deltas in the stream (verified live);
//  2. the AG-UI TRANSLATOR must pass the real delta through (showReasoning) instead of
//     substituting "[reasoning redacted]";
//  3. the status PANE must render the rolling FIFO window.
//
// This test wires all three over a REAL DeepSeek-V4 reasoning stream: the real prompt
// builder + reasoning policy produce the request (asserting exclude:false), the real
// openai_compat client streams it, the chunks are wrapped into the same *agent.Event
// shape the live agent emits, the real agui.Translate(showReasoning=true) + Fanout pump
// it, and the real statusPane (ShowReasoning on) renders it into a fake bot. It then
// asserts the model's actual reasoning text surfaced in the pane — never the redacted
// marker — and that the pane collapses to "completato" on the final answer.
//
// PAID, OPERATOR-AUTHORIZED gate behind the live_e2e tag (never CI). Needs only
// OPENROUTER_API_KEY; with it unset the test t.Skips (the one legitimate local skip).
//
// REPRODUCE:
//
//	cp /d/Aura/.env ./.env; set -a; . ./.env; set +a
//	go test -tags live_e2e -run TestCoTLiveE2E -timeout 300s -v ./internal/channels/telegram/
package telegram

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
	tele "gopkg.in/telebot.v4"
)

func TestCoTLiveE2E_ReasoningReachesStatusPane(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("live_e2e: OPENROUTER_API_KEY unset — PAID gate, not CI. " +
			"Run: cp /d/Aura/.env ./.env; set -a; . ./.env; set +a; " +
			"go test -tags live_e2e -run TestCoTLiveE2E -timeout 300s -v ./internal/channels/telegram/")
	}
	model := os.Getenv("AURA_LLM_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-flash:nitro"
	}
	cfg := llm.Config{
		Provider:          "openrouter",
		BaseURL:           "https://openrouter.ai/api/v1",
		Model:             model,
		APIKey:            os.Getenv("OPENROUTER_API_KEY"),
		Temperature:       0,
		MaxTokens:         2048,
		AdaptiveReasoning: true,
		ShowReasoning:     true, // master switch ON: policy must request exclude:false
		ConnectTimeoutSec: 10,
		Headers:           map[string]string{"HTTP-Referer": "https://github.com/chetto1983/aura", "X-Title": "Aura"},
	}

	// Real prompt builder + reasoning policy. ShowReasoning ON must yield exclude:false,
	// otherwise the provider streams no reasoning and the feature is dead.
	builder := prompt.NewPromptBuilder()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "Rispondi in italiano. Ragiona passo-passo, poi una frase finale breve."},
		{Role: llm.RoleUser, Content: "Quanto fa 17 per 23 meno 12? Spiega il ragionamento, poi il risultato."},
	}
	req := builder.BuildWithReasoningTier(history, reg, cfg.Provider, cfg, prompt.Budget{}, prompt.ReasoningTierHigh, nil)
	if req.Reasoning.Exclude == nil || *req.Reasoning.Exclude {
		t.Fatalf("precondition: ShowReasoning ON must set Exclude=false, got %v", req.Reasoning.Exclude)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Stream the real reasoning turn and wrap each chunk into the *agent.Event shape
	// the live LlmAgent emits (reasoning → LLMResponse.Reasoning, content → Content,
	// terminal → FinishReason), accumulating the real reasoning text for the assertion.
	ch, err := openai_compat.New(cfg).Stream(ctx, req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var realReasoning strings.Builder
	seq := func(yield func(*agent.Event, error) bool) {
		for c := range ch {
			if c.Err != nil {
				yield(nil, c.Err)
				return
			}
			switch {
			case c.Reasoning != "":
				realReasoning.WriteString(c.Reasoning)
				if !yield(reasoningEvent(c.Reasoning), nil) {
					return
				}
			case c.Text != "":
				if !yield(contentEvent(c.Text, ""), nil) {
					return
				}
			case c.FinishReason != "":
				if !yield(contentEvent("", c.FinishReason), nil) {
					return
				}
			}
		}
	}

	// Real translate (showReasoning=true) → real fanout → real status pane (ShowReasoning).
	translated := agui.Translate("thread-cot", "run-cot", agui.NewIDGenerator(), seq, true)
	fo := agui.NewFanout(translated)
	statusCh := fo.Subscribe()
	fo.Run(ctx)

	bot := newFakeBot()
	pane := newStatusPane(bot, tele.ChatID(7), 0, true, 4096)
	pane.consume(ctx, statusCh)

	reasoned := strings.TrimSpace(realReasoning.String())
	if reasoned == "" {
		t.Fatal("model streamed no reasoning deltas — exclude:false did not take effect end-to-end")
	}

	// Every pane edit, concatenated + whitespace-collapsed, must (a) never carry the
	// redaction marker and (b) contain a recognizable slice of the model's real CoT.
	var allEdits strings.Builder
	for _, call := range bot.recorded() {
		allEdits.WriteString(call.text)
		allEdits.WriteString("\n")
	}
	haystack := collapseWhitespace(allEdits.String())
	if strings.Contains(haystack, "[reasoning redacted]") {
		t.Fatalf("status pane showed the redaction marker — translator did not pass the real delta:\n%s", haystack)
	}
	needle := firstReasoningProbe(reasoned)
	if needle != "" && !strings.Contains(haystack, needle) {
		t.Fatalf("real reasoning probe %q not found in any pane edit:\n%s", needle, haystack)
	}
	if last := lastText(bot); !strings.Contains(last, "completato") {
		t.Fatalf("final pane must collapse to completato, got %q", last)
	}
	t.Logf("CoT E2E ok: %d reasoning runes surfaced across %d pane edits; probe=%q",
		len([]rune(reasoned)), len(bot.recorded()), needle)
}

// reasoningEvent / contentEvent mirror the live agent's per-chunk Event emission.
func reasoningEvent(text string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Reasoning: text}}
}

func contentEvent(text, finish string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Content: text, FinishReason: finish}}
}

// firstReasoningProbe returns a short collapsed slice of the reasoning to search for in
// the pane edits — enough runes to be distinctive, few enough to survive the rolling
// window's tail-keep. Empty when the reasoning is too short to probe reliably.
func firstReasoningProbe(reasoning string) string {
	fields := strings.Fields(reasoning)
	if len(fields) < 3 {
		return ""
	}
	return strings.Join(fields[:3], " ")
}
