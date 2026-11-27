package openai_compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestSDKStreamAccumulatesSplitToolCallAndUsage(t *testing.T) {
	server := httptest.NewServer(fixtureHandler(t, "toolcall_multichunk.sse"))
	defer server.Close()
	stream, err := New(testConfig(server.URL)).Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(stream)
	var call *llm.ToolCall
	var usage *llm.Usage
	finish := ""
	for _, chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.ToolCall != nil {
			call = chunk.ToolCall
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
	}
	if call == nil || call.ID != "call_abc123" || call.Function.Name != "get_weather" {
		t.Fatalf("tool call = %+v", call)
	}
	if len(call.Function.Arguments) < 65_000 {
		t.Fatalf("tool arguments length = %d, want >64KiB", len(call.Function.Arguments))
	}
	if finish != "tool_calls" {
		t.Fatalf("finish = %q", finish)
	}
	if usage == nil || usage.PromptTokens != 15 || usage.CachedTokens != 10 || usage.Cost == nil || *usage.Cost != 0.0005 {
		t.Fatalf("usage = %+v", usage)
	}
}

// OpenRouter answers GET /api/v1/generation?id= with the provider that served a call, its
// latency and its cached tokens; the overview's sample response also names the provider
// inline. Measured on the lab VM 2026-11-27: neither reached the logs.
func TestSDKStreamNamesTheResponseAndItsProvider(t *testing.T) {
	raw := "data: {\"id\":\"gen-1764252000-abc\",\"provider\":\"Inkling\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"gen-1764252000-abc\",\"provider\":\"Inkling\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]," +
		"\"usage\":{\"prompt_tokens\":92578,\"completion_tokens\":48,\"prompt_tokens_details\":{\"cached_tokens\":2048}}}\n\n" +
		"data: [DONE]\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	stream, err := New(testConfig(server.URL)).Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var usage *llm.Usage
	for _, chunk := range drain(stream) {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if usage == nil || usage.ResponseID != "gen-1764252000-abc" || usage.Provider != "Inkling" || usage.CachedTokens != 2048 {
		t.Fatalf("usage = %+v, want the response id, its provider and the cached tokens", usage)
	}
}

func TestSDKStreamPreservesBothReasoningFieldNames(t *testing.T) {
	raw := "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"prima \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"seconda\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	stream, err := New(testConfig(server.URL)).Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var reasoning strings.Builder
	for _, chunk := range drain(stream) {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		reasoning.WriteString(chunk.Reasoning)
	}
	if reasoning.String() != "prima seconda" {
		t.Fatalf("reasoning = %q", reasoning.String())
	}
}
