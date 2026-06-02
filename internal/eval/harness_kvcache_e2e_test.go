//go:build live_e2e

// harness_kvcache_e2e_test.go is the AUTONOMOUS, real-LLM E2E replacement for the
// Phase-6 (KV Cache Builder) human UAT items SC#2 (cache warming) and SC#4 (cache
// hit rate >= 80%). Instead of an operator eyeballing `aura chat send` three times,
// this drives the REAL agent.LlmAgent over the REAL openai_compat client against
// DeepSeek-V4 Flash (via OpenRouter) for a realistic multi-turn conversation and
// ASSERTS the cache behaviour programmatically off the per-turn llm.Usage the
// production wire reports — the same stable-prefix path `aura chat` uses.
//
// Like the cot_eval harness this is a PAID, OPERATOR-AUTHORIZED gate behind a build
// tag so it never runs in default CI/Makefile quality. With OPENROUTER_API_KEY unset
// it t.Skips (the one legitimate local skip); when the key is present it runs and
// asserts — there is no human-in-the-loop. To wire it into a dedicated paid CI lane,
// run it with the key exported and the tag set (it fails the build on a real miss).
//
// REPRODUCE:
//
//	set -a; . ./.env; set +a
//	go test -tags live_e2e -run TestKVCacheWarmingE2E -timeout 600s -v ./internal/eval/
package eval

import (
	"context"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

// hitRateTarget is the PRD §Slice 4 performance target (SC#4): cache hit rate on a
// warmed DeepSeek-V4 Flash session. This is a HARD gate — do not lower it to make a
// real miss pass; a genuine miss is a finding about the provider/prefix, not a knob.
const hitRateTarget = 0.80

// kvTurn is one captured conversation turn's cache accounting.
type kvTurn struct {
	idx      int
	prompt   int
	cached   int
	ratio    float64
	proseLen int
}

// e2eRegistry mirrors cmd/aura/main.go's default (non-deferred) tool manifest — the
// alphabetically-sorted set that becomes the byte-stable messages[0] prefix. Deferred
// tools intentionally stay out of the manifest, so this IS the production prefix shape.
func e2eRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{})
	return reg
}

// TestKVCacheWarmingE2E asserts SC#2 + SC#4 against the live provider.
func TestKVCacheWarmingE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("live_e2e: OPENROUTER_API_KEY unset — PAID gate, not CI. " +
			"Run: set -a; . ./.env; set +a; go test -tags live_e2e -run TestKVCacheWarmingE2E -timeout 600s -v ./internal/eval/")
	}

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	client := openai_compat.New(*cfg)
	reg := e2eRegistry()
	sessionID := uuid.NewString()
	runDir := t.TempDir()
	ctx := context.Background()

	// A realistic, sequence-dependent multi-turn session. messages[0] (system + tool
	// manifest) is byte-stable; the cacheable prefix grows turn-over-turn as prior
	// turns are appended, so a warmed provider should report rising cached tokens and
	// a hit rate climbing toward the PRD target on the later turns.
	prompts := []string{
		"Ciao! Mi chiamo Davide. Ti va di aiutarmi a organizzare la settimana?",
		"Per cominciare, che giorno e che ora sono adesso?",
		"Elenca tre attività utili che potrei pianificare questa settimana.",
		"Della lista che hai appena fatto, quale metteresti per prima e perché?",
		"Perfetto. Riassumi in una frase il piano e ricordami come mi chiamo.",
		"Grazie! Salutami in modo amichevole prima di chiudere.",
	}

	var history []llm.Message
	var turns []kvTurn

	for ti, p := range prompts {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: p})

		la := agent.NewLlmAgent(agent.LlmAgentConfig{
			Client:     client,
			LLM:        *cfg,
			Registry:   reg,
			PreviewCap: 2048,
			RunDir:     runDir,
			SessionID:  sessionID,
			UserTurns:  history,
		})

		bud, err := agent.NewBudget(agent.BudgetOptions{})
		if err != nil {
			t.Fatalf("turn %d: NewBudget: %v", ti+1, err)
		}
		ic := agent.InvocationContext{
			Ctx:       ctx,
			Agent:     la,
			RequestID: uuid.New(),
			Branch:    "root",
			Budget:    bud,
		}

		cap := captureTurn(func() func(func(*agent.Event, error) bool) { return la.Run(ic) })
		if cap.runErr != nil {
			t.Fatalf("turn %d: live run error: %v", ti+1, cap.runErr)
		}
		if cap.usage.PromptTokens == 0 && cap.prose == "" {
			t.Fatalf("turn %d: provider returned no usage and no prose — cannot evaluate cache (infra stall)", ti+1)
		}
		if cap.prose != "" {
			history = append(history, llm.Message{Role: llm.RoleAssistant, Content: cap.prose})
		}

		ratio := 0.0
		if cap.usage.PromptTokens > 0 {
			ratio = float64(cap.usage.CachedTokens) / float64(cap.usage.PromptTokens)
		}
		turns = append(turns, kvTurn{
			idx: ti + 1, prompt: cap.usage.PromptTokens, cached: cap.usage.CachedTokens,
			ratio: ratio, proseLen: len(cap.prose),
		})
		t.Logf("turn %2d: prompt=%5d cached=%5d ratio=%5.1f%% prose=%dB",
			ti+1, cap.usage.PromptTokens, cap.usage.CachedTokens, ratio*100, len(cap.prose))
	}

	if len(turns) < 3 {
		t.Fatalf("need >=3 turns to evaluate warming, captured %d", len(turns))
	}

	// ---- SC#2: cache warming — cached tokens rise once the prefix is warm ----
	// Turn 1 is cold (provider has not seen the prefix). From turn 2 onward the
	// cached count must be non-decreasing AND end strictly above the cold turn.
	var peakRatio float64
	for _, tn := range turns {
		if tn.ratio > peakRatio {
			peakRatio = tn.ratio
		}
	}
	for i := 1; i < len(turns); i++ { // compare turn i+1 vs turn i, starting at the 2nd→3rd boundary
		if i >= 2 && turns[i].cached < turns[i-1].cached {
			t.Errorf("SC#2 cache warming: cached tokens dropped turn %d (%d) -> turn %d (%d); expected non-decreasing once warm",
				turns[i-1].idx, turns[i-1].cached, turns[i].idx, turns[i].cached)
		}
	}
	lastCached := turns[len(turns)-1].cached
	coldCached := turns[0].cached
	if lastCached <= coldCached {
		t.Errorf("SC#2 cache warming: final-turn cached=%d not above cold turn-1 cached=%d — prefix never warmed",
			lastCached, coldCached)
	}

	// ---- SC#4: hit rate >= 80% on the warmed session (peak over the session) ----
	if peakRatio < hitRateTarget {
		t.Errorf("SC#4 cache hit rate: peak ratio %.1f%% < target %.1f%% — DeepSeek-V4 Flash prefix cache not meeting PRD performance target",
			peakRatio*100, hitRateTarget*100)
	}

	t.Logf("KV-cache E2E: peak hit rate=%.1f%% (target %.1f%%), cold cached=%d, final cached=%d over %d turns",
		peakRatio*100, hitRateTarget*100, coldCached, lastCached, len(turns))
}
