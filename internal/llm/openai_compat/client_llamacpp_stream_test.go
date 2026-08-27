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

// TestRequestBody_LlamaCppStreamOptions is the llama.cpp-target counterpart of
// TestRequestBody: stream_options:{include_usage:true} MUST be on the wire when
// llm.ReasoningTarget resolves to llamacpp — llama.cpp only emits a stream usage
// object (the cockpit CONTESTO/CACHE gauges' sole data source) when asked. The
// llamacpp-only reasoning fields (already covered by the full effort matrix in
// TestBuildWireRequestReasoningTarget) are sampled once here to prove the two
// coexist on the same request rather than re-testing the whole matrix.
func TestRequestBody_LlamaCppStreamOptions(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer srv.Close()

	cfg := llm.Config{
		BaseURL:           srv.URL,
		Provider:          "llamacpp",
		Model:             "qwythos-9b",
		MaxTokens:         4096,
		ConnectTimeoutSec: 10,
	}
	c := New(cfg)
	ch, err := c.Stream(context.Background(), llm.Request{
		Model:     "qwythos-9b",
		Messages:  []llm.Message{{Role: "user", Content: "ciao"}},
		Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortLow},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	drain(ch)

	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	streamOpts, _ := body["stream_options"].(map[string]any)
	if streamOpts == nil {
		t.Fatalf("stream_options absent from llama.cpp request body: %s", gotBody)
	}
	if streamOpts["include_usage"] != true {
		t.Errorf("stream_options.include_usage = %v, want true", streamOpts["include_usage"])
	}
	if _, ok := body["thinking_budget_tokens"]; !ok {
		t.Errorf("thinking_budget_tokens absent — stream_options must coexist with the llama.cpp reasoning fields, not replace them: %s", gotBody)
	}
	if _, ok := body["reasoning"]; ok {
		t.Errorf("reasoning key present on the llama.cpp path (must stay OpenRouter-only): %s", gotBody)
	}
}

// TestStream_LlamaCppUsageChunk proves the fix end-to-end through the real
// Client.Stream goroutine (not just parseSSE): replaying the EXACT shape a live
// llama.cpp server (build b9859) emits when stream_options.include_usage is set
// — a finish_reason chunk, THEN a separate final chunk with empty choices and a
// usage object carrying prompt_tokens_details.cached_tokens — yields a trailing
// llm.Chunk{Usage} with CachedTokens intact. This is the data the cockpit
// CONTESTO/CACHE gauges read (RuntimeFooter.tsx); a regression here reproduces
// the stuck-at-0 bug.
func TestStream_LlamaCppUsageChunk(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"completion_tokens\":10,\"prompt_tokens\":42,\"total_tokens\":52,\"prompt_tokens_details\":{\"cached_tokens\":30}}}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Provider = "llamacpp"
	c := New(cfg)
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(ch)

	var usage *llm.Usage
	for _, ck := range chunks {
		if ck.Usage != nil {
			usage = ck.Usage
		}
		if ck.Err != nil {
			t.Fatalf("unexpected Err chunk: %v", ck.Err)
		}
	}
	if usage == nil {
		t.Fatalf("chunks %#v; want a trailing Usage chunk", chunks)
	}
	if usage.PromptTokens != 42 || usage.CompletionTokens != 10 {
		t.Errorf("tokens = (%d,%d), want (42,10)", usage.PromptTokens, usage.CompletionTokens)
	}
	if usage.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30 — this is the CACHE gauge's data source", usage.CachedTokens)
	}
}
