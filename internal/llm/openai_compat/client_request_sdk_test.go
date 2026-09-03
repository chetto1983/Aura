package openai_compat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func captureSDKBody(t *testing.T, cfg llm.Config, request llm.Request) map[string]any {
	t.Helper()
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	cfg.BaseURL = server.URL
	client := New(cfg)
	// Keep transport pointed at httptest while preserving the logical OpenRouter
	// target used by the production gate (which additionally requires openrouter.ai).
	if cfg.Provider == "openrouter" {
		client.cfg.BaseURL = "https://openrouter.ai/api/v1"
	}
	stream, err := client.Stream(context.Background(), request)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_ = drain(stream)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("request body: %v\n%s", err, raw)
	}
	return body
}

func TestSDKRequestCapabilityEnvelope(t *testing.T) {
	for _, provider := range []string{"openrouter", "llamacpp", "ollama", "vllm"} {
		t.Run(provider, func(t *testing.T) {
			body := captureSDKBody(t, llm.Config{Provider: provider}, llm.Request{
				Model: "m", SessionID: "session-1",
				Messages: []llm.Message{{Role: llm.RoleTool, ToolCallID: "c", Content: "ok"}},
			})
			messages, _ := body["messages"].([]any)
			message, _ := messages[0].(map[string]any)
			if message["tool_call_id"] != "c" {
				t.Fatalf("body = %#v", body)
			}
			providerBody, hasProvider := body["provider"].(map[string]any)
			_, hasSession := body["session_id"]
			if provider == "openrouter" {
				if !hasProvider || providerBody["data_collection"] != "deny" || !hasSession {
					t.Fatalf("OpenRouter envelope = %#v", body)
				}
			} else if hasProvider || hasSession {
				t.Fatalf("%s received OpenRouter-only fields: %#v", provider, body)
			}
		})
	}
}

func TestSDKRequestMaxTokensContract(t *testing.T) {
	without := captureSDKBody(t, llm.Config{Provider: "openrouter"}, llm.Request{
		Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Content: "summarize"}},
	})
	if _, present := without["max_tokens"]; present {
		t.Fatalf("uncapped request sent max_tokens: %#v", without)
	}
	with := captureSDKBody(t, llm.Config{Provider: "openrouter"}, llm.Request{Model: "m", MaxTokens: 4096})
	if with["max_tokens"] != float64(4096) {
		t.Fatalf("max_tokens = %#v", with["max_tokens"])
	}
}

func TestSDKRequestReasoningTargets(t *testing.T) {
	efforts := []llm.ReasoningEffort{
		llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
		llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax,
	}
	for _, effort := range efforts {
		t.Run("openrouter_"+string(effort), func(t *testing.T) {
			body := captureSDKBody(t, llm.Config{Provider: "openrouter"}, llm.Request{
				Model: "m", Reasoning: llm.ReasoningConfig{Effort: effort},
			})
			reasoning, _ := body["reasoning"].(map[string]any)
			if reasoning["effort"] != string(effort) {
				t.Fatalf("reasoning = %#v", reasoning)
			}
			for _, forbidden := range []string{"thinking_budget_tokens", "chat_template_kwargs", "stream_options"} {
				if _, present := body[forbidden]; present {
					t.Fatalf("OpenRouter body contains %q: %#v", forbidden, body)
				}
			}
		})
	}

	llamaCases := []struct {
		effort llm.ReasoningEffort
		budget *int
		off    bool
	}{
		{effort: llm.ReasoningEffortNone, off: true},
		{effort: llm.ReasoningEffortLow, budget: new(512)},
		{effort: llm.ReasoningEffortMedium, budget: new(2048)},
		{effort: llm.ReasoningEffortHigh, budget: new(8192)},
		{effort: llm.ReasoningEffortXHigh, budget: new(16384)},
		{effort: llm.ReasoningEffortMax, budget: new(-1)},
	}
	for _, testCase := range llamaCases {
		t.Run("llamacpp_"+string(testCase.effort), func(t *testing.T) {
			body := captureSDKBody(t, llm.Config{Provider: "llamacpp"}, llm.Request{
				Model: "m", Reasoning: llm.ReasoningConfig{Effort: testCase.effort},
			})
			if _, present := body["reasoning"]; present {
				t.Fatalf("llama.cpp received OpenRouter reasoning: %#v", body)
			}
			streamOptions, _ := body["stream_options"].(map[string]any)
			if streamOptions["include_usage"] != true {
				t.Fatalf("stream_options = %#v", streamOptions)
			}
			if testCase.off {
				kwargs, _ := body["chat_template_kwargs"].(map[string]any)
				if kwargs["enable_thinking"] != false {
					t.Fatalf("kwargs = %#v", kwargs)
				}
			} else if body["thinking_budget_tokens"] != float64(*testCase.budget) {
				t.Fatalf("budget = %#v want %d", body["thinking_budget_tokens"], *testCase.budget)
			}
		})
	}
	auto := captureSDKBody(t, llm.Config{Provider: "llamacpp"}, llm.Request{Model: "m"})
	if _, present := auto["reasoning"]; present {
		t.Fatalf("auto reasoning body = %#v", auto)
	}
	if auto["stream_options"].(map[string]any)["include_usage"] != true {
		t.Fatalf("auto stream_options = %#v", auto["stream_options"])
	}

	ollamaCases := []struct {
		effort llm.ReasoningEffort
		wire   string
	}{
		{effort: llm.ReasoningEffortNone, wire: "none"},
		{effort: llm.ReasoningEffortLow, wire: "low"},
		{effort: llm.ReasoningEffortMedium, wire: "medium"},
		{effort: llm.ReasoningEffortHigh, wire: "high"},
		{effort: llm.ReasoningEffortXHigh, wire: "high"},
		{effort: llm.ReasoningEffortMax, wire: "high"},
	}
	for _, testCase := range ollamaCases {
		t.Run("ollama_"+string(testCase.effort), func(t *testing.T) {
			body := captureSDKBody(t, llm.Config{Provider: "ollama", OpenRouterMiddleOut: true}, llm.Request{
				Model: "gemma4:31b-cloud", SessionID: "must-not-leak",
				Reasoning: llm.ReasoningConfig{Effort: testCase.effort},
			})
			if body["reasoning_effort"] != testCase.wire {
				t.Fatalf("reasoning_effort = %#v, want %q", body["reasoning_effort"], testCase.wire)
			}
			streamOptions, _ := body["stream_options"].(map[string]any)
			if streamOptions["include_usage"] != true {
				t.Fatalf("stream_options = %#v", streamOptions)
			}
			for _, forbidden := range []string{
				"reasoning", "thinking_budget_tokens", "chat_template_kwargs",
				"provider", "session_id", "transforms",
			} {
				if _, present := body[forbidden]; present {
					t.Fatalf("Ollama received provider-specific %q: %#v", forbidden, body)
				}
			}
		})
	}
	ollamaAuto := captureSDKBody(t, llm.Config{Provider: "ollama"}, llm.Request{Model: "gemma4:31b-cloud"})
	if _, present := ollamaAuto["reasoning_effort"]; present {
		t.Fatalf("Ollama auto request forced reasoning_effort: %#v", ollamaAuto)
	}
	if ollamaAuto["stream_options"].(map[string]any)["include_usage"] != true {
		t.Fatalf("Ollama auto stream_options = %#v", ollamaAuto["stream_options"])
	}
}

func TestSDKRequestMiddleOutGate(t *testing.T) {
	on := captureSDKBody(t, llm.Config{Provider: "openrouter", OpenRouterMiddleOut: true}, llm.Request{Model: "m"})
	transforms, _ := on["transforms"].([]any)
	if len(transforms) != 1 || transforms[0] != "middle-out" {
		t.Fatalf("transforms = %#v", on["transforms"])
	}
	off := captureSDKBody(t, llm.Config{Provider: "openrouter"}, llm.Request{Model: "m"})
	if _, present := off["transforms"]; present {
		t.Fatalf("knob-off body = %#v", off)
	}
	llama := captureSDKBody(t, llm.Config{Provider: "llamacpp", OpenRouterMiddleOut: true}, llm.Request{Model: "m"})
	if _, present := llama["transforms"]; present {
		t.Fatalf("llama body = %#v", llama)
	}
}

// The repetition penalty is not a standard OpenAI field, so each backend spells it
// differently and one that does not recognise the name it is given ignores it in silence.
// Measured on a live llama-server by reading /slots back: repetition_penalty 3.0 left
// repeat_penalty at its 1.0 default, while repeat_penalty 3.0 was applied. A setting the
// operator believes is in force and which never reaches the sampler is worse than one
// that fails.
func TestSDKRequestSpellsTheRepetitionPenaltyPerBackend(t *testing.T) {
	penalty := 1.15
	for _, tc := range []struct {
		name     string
		cfg      llm.Config
		wantKey  string
		otherKey string
	}{
		{
			name:     "llamacpp takes repeat_penalty",
			cfg:      llm.Config{Provider: "llamacpp", BaseURL: "http://127.0.0.1:8090/v1"},
			wantKey:  "repeat_penalty",
			otherKey: "repetition_penalty",
		},
		{
			name:     "openrouter keeps repetition_penalty",
			cfg:      llm.Config{Provider: "openrouter"},
			wantKey:  "repetition_penalty",
			otherKey: "repeat_penalty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := captureSDKBody(t, tc.cfg, llm.Request{
				Model: "m", Sampling: llm.Sampling{RepetitionPenalty: &penalty},
			})
			if body[tc.wantKey] != penalty {
				t.Fatalf("%s = %#v, want %v", tc.wantKey, body[tc.wantKey], penalty)
			}
			if _, present := body[tc.otherKey]; present {
				t.Fatalf("body also carries %s, which this backend ignores: %#v", tc.otherKey, body)
			}
		})
	}
}
