package openai_compat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestBuildWireRequestReasoningTarget is the DAEMON-FREE, coverage-load-bearing proof
// of the target-aware wire projection: the OpenRouter shape stays byte-unchanged
// (spike 096) while the net-new llama.cpp branch (spike 095) emits enable_thinking:false
// for OFF and the exact fixed thinking_budget_tokens per graduated level — never the
// OpenRouter reasoning object. A container/live-gated test would contribute ZERO gate
// coverage (CLAUDE.md), so this pure table test is what actually covers the branch.
func TestBuildWireRequestReasoningTarget(t *testing.T) {
	t.Run("openrouter unchanged", func(t *testing.T) {
		c := New(llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
		efforts := []llm.ReasoningEffort{
			llm.ReasoningEffortNone,
			llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium,
			llm.ReasoningEffortHigh,
			llm.ReasoningEffortXHigh,
			llm.ReasoningEffortMax,
		}
		for _, eff := range efforts {
			t.Run(string(eff), func(t *testing.T) {
				wire := c.buildWireRequest(llm.Request{Reasoning: llm.ReasoningConfig{Effort: eff}})
				if wire.Reasoning == nil {
					t.Fatalf("Reasoning = nil, want the OpenRouter object for effort %q", eff)
				}
				if wire.Reasoning.Effort != string(eff) {
					t.Errorf("Reasoning.Effort = %q, want %q", wire.Reasoning.Effort, eff)
				}
				if wire.ChatTemplateKwargs != nil {
					t.Errorf("ChatTemplateKwargs = %v, want nil on the OpenRouter path", wire.ChatTemplateKwargs)
				}
				if wire.ThinkingBudgetTokens != nil {
					t.Errorf("ThinkingBudgetTokens = %d, want nil on the OpenRouter path", *wire.ThinkingBudgetTokens)
				}
				if wire.StreamOptions != nil {
					t.Errorf("StreamOptions = %+v, want nil on the OpenRouter path (deprecated no-op there)", wire.StreamOptions)
				}
			})
		}

		// Empty reasoning (auto) → no reasoning object at all — unchanged behavior.
		wire := c.buildWireRequest(llm.Request{})
		if wire.Reasoning != nil {
			t.Errorf("Reasoning = %+v, want nil for empty reasoning", wire.Reasoning)
		}
		if wire.StreamOptions != nil {
			t.Errorf("StreamOptions = %+v, want nil on the OpenRouter path", wire.StreamOptions)
		}
	})

	t.Run("llamacpp branch", func(t *testing.T) {
		c := New(llm.Config{Provider: "llamacpp", BaseURL: "http://localhost:8080/v1"})

		// assertStreamOptions proves stream_options:{include_usage:true} rides
		// EVERY llama.cpp request regardless of reasoning effort — it is the fix
		// for the cockpit context/cache gauges reading 0 (llama.cpp emits stream
		// usage only when asked), not a reasoning-effort-conditional knob.
		assertStreamOptions := func(t *testing.T, wire wireRequest) {
			t.Helper()
			if wire.StreamOptions == nil {
				t.Fatal("StreamOptions = nil, want &wireStreamOptions{IncludeUsage:true} on the llama.cpp target")
			}
			if !wire.StreamOptions.IncludeUsage {
				t.Errorf("StreamOptions.IncludeUsage = false, want true on the llama.cpp target")
			}
		}

		t.Run("off sets enable_thinking false", func(t *testing.T) {
			wire := c.buildWireRequest(llm.Request{Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortNone}})
			if wire.Reasoning != nil {
				t.Errorf("Reasoning = %+v, want nil on the llama.cpp branch", wire.Reasoning)
			}
			if wire.ThinkingBudgetTokens != nil {
				t.Errorf("ThinkingBudgetTokens = %d, want nil for OFF", *wire.ThinkingBudgetTokens)
			}
			v, ok := wire.ChatTemplateKwargs["enable_thinking"].(bool)
			if !ok || v {
				t.Errorf("ChatTemplateKwargs[enable_thinking] = %v (ok=%v), want false", wire.ChatTemplateKwargs["enable_thinking"], ok)
			}
			assertStreamOptions(t, wire)
		})

		budgets := []struct {
			effort llm.ReasoningEffort
			want   int
		}{
			{llm.ReasoningEffortLow, 512},
			{llm.ReasoningEffortMedium, 2048},
			{llm.ReasoningEffortHigh, 8192},
			{llm.ReasoningEffortXHigh, 16384},
			{llm.ReasoningEffortMax, -1},
		}
		for _, tc := range budgets {
			t.Run("budget "+string(tc.effort), func(t *testing.T) {
				wire := c.buildWireRequest(llm.Request{Reasoning: llm.ReasoningConfig{Effort: tc.effort}})
				if wire.Reasoning != nil {
					t.Errorf("Reasoning = %+v, want nil on the llama.cpp branch", wire.Reasoning)
				}
				if wire.ChatTemplateKwargs != nil {
					t.Errorf("ChatTemplateKwargs = %v, want nil for a graduated budget", wire.ChatTemplateKwargs)
				}
				if wire.ThinkingBudgetTokens == nil {
					t.Fatalf("ThinkingBudgetTokens = nil, want %d for effort %q", tc.want, tc.effort)
				}
				if *wire.ThinkingBudgetTokens != tc.want {
					t.Errorf("ThinkingBudgetTokens = %d, want %d for effort %q", *wire.ThinkingBudgetTokens, tc.want, tc.effort)
				}
				assertStreamOptions(t, wire)
			})
		}

		t.Run("auto emits no reasoning fields", func(t *testing.T) {
			wire := c.buildWireRequest(llm.Request{}) // empty reasoning
			if wire.Reasoning != nil || wire.ChatTemplateKwargs != nil || wire.ThinkingBudgetTokens != nil {
				t.Errorf("auto: reasoning=%+v kwargs=%v budget=%v, want all nil",
					wire.Reasoning, wire.ChatTemplateKwargs, wire.ThinkingBudgetTokens)
			}
			assertStreamOptions(t, wire)
		})
	})
}

// TestBuildWireRequestMiddleOutTransformGate is the wire-shape proof of the
// fix-plan 1.11 overflow belt's strict OpenRouter-target gate: the transforms
// key appears ONLY when BOTH the resolved target is OpenRouter AND the opt-in
// knob is on; the target gate wins over a perversely-configured llamacpp+ON
// combination, and OpenRouter-with-knob-OFF (today's default) stays
// byte-unchanged — no "transforms" key at all, not even an empty one.
func TestBuildWireRequestMiddleOutTransformGate(t *testing.T) {
	marshal := func(t *testing.T, wire wireRequest) string {
		t.Helper()
		body, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("json.Marshal(wire): %v", err)
		}
		return string(body)
	}

	t.Run("openrouter knob on emits transforms", func(t *testing.T) {
		c := New(llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", OpenRouterMiddleOut: true})
		wire := c.buildWireRequest(llm.Request{})
		if len(wire.Transforms) != 1 || wire.Transforms[0] != "middle-out" {
			t.Fatalf("Transforms = %v, want [middle-out]", wire.Transforms)
		}
		body := marshal(t, wire)
		if !strings.Contains(body, `"transforms":["middle-out"]`) {
			t.Errorf("wire body = %s, want it to contain \"transforms\":[\"middle-out\"]", body)
		}
	})

	t.Run("openrouter knob off omits transforms", func(t *testing.T) {
		c := New(llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", OpenRouterMiddleOut: false})
		wire := c.buildWireRequest(llm.Request{})
		if wire.Transforms != nil {
			t.Errorf("Transforms = %v, want nil (knob off)", wire.Transforms)
		}
		body := marshal(t, wire)
		if strings.Contains(body, "transforms") {
			t.Errorf("wire body = %s, want no transforms key when the knob is off", body)
		}
	})

	t.Run("llamacpp target wins over knob on", func(t *testing.T) {
		c := New(llm.Config{Provider: "llamacpp", BaseURL: "http://localhost:8080/v1", OpenRouterMiddleOut: true})
		wire := c.buildWireRequest(llm.Request{})
		if wire.Transforms != nil {
			t.Errorf("Transforms = %v, want nil — the OpenRouter-target gate must win over a perverse llamacpp+knob-ON config", wire.Transforms)
		}
		body := marshal(t, wire)
		if strings.Contains(body, "transforms") {
			t.Errorf("wire body = %s, want no transforms key on the llama.cpp target regardless of the knob", body)
		}
	})
}
