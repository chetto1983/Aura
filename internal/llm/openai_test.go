package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIClientSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "Hello from AI"}}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	result, err := client.Send(context.Background(), Request{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.Content != "Hello from AI" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello from AI")
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestOpenAIClientSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	_, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
}

func TestOpenAIClientSendModelOverride(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedModel = req.Model

		w.Write([]byte(`{"choices": [{"message": {"role": "assistant", "content": "ok"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	// Request with model override
	_, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
		Model:    "gpt-3.5-turbo",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if receivedModel != "gpt-3.5-turbo" {
		t.Errorf("model = %q, want %q", receivedModel, "gpt-3.5-turbo")
	}

	// Request without model override (should use default)
	_, err = client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if receivedModel != "gpt-4" {
		t.Errorf("model = %q, want %q", receivedModel, "gpt-4")
	}
}

func TestOpenAIClientSendTools(t *testing.T) {
	var received chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "search_files",
							"arguments": "{\"query\":\"aura\"}"
						}
					}]
				},
				"finish_reason": "tool_calls"
			}]
		}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	result, err := client.Send(context.Background(), Request{
		Messages: []Message{
			{Role: "user", Content: "search memory"},
		},
		Tools: []ToolDefinition{
			{
				Name:        "search_files",
				Description: "Search wiki",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"query": map[string]any{"type": "string"}},
					"required":   []string{"query"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(received.Tools) != 1 || received.Tools[0].Function.Name != "search_files" {
		t.Fatalf("tools not sent correctly: %+v", received.Tools)
	}
	if !result.HasToolCalls || len(result.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got has=%v len=%d", result.HasToolCalls, len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "search_files" {
		t.Errorf("tool name = %q, want search_files", result.ToolCalls[0].Name)
	}
	if result.ToolCalls[0].Arguments["query"] != "aura" {
		t.Errorf("query arg = %v, want aura", result.ToolCalls[0].Arguments["query"])
	}
}

func TestOpenAIClientStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send SSE events
		flusher, _ := w.(http.Flusher)

		// Wire JSON written as strings rather than marshaling streamChunk
		// so the test isn't coupled to the internal struct shape — slice
		// 11s added a tool_calls delta field and used to break this.
		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" world"}}]}`,
			`[DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
		}

	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	ch, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var result string
	for token := range ch {
		if token.Err != nil {
			t.Fatalf("stream error: %v", token.Err)
		}
		if token.Done {
			break
		}
		result += token.Content
	}

	if result != "Hello world" {
		t.Errorf("stream result = %q, want %q", result, "Hello world")
	}
}

// TestOpenAIClientStreamWithToolCalls verifies slice 11s: tool-call
// argument fragments arriving across multiple SSE chunks are accumulated
// per-index and emitted as fully-formed ToolCall objects on the final
// Done=true token. Stream consumers should never have to reassemble
// partial JSON themselves.
func TestOpenAIClientStreamWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// Realistic OpenAI streaming shape: id+name in first chunk,
		// arguments built up in fragments, finish_reason on terminator.
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"search_files","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"que"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ry\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			w.Write([]byte("data: " + c + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "gpt-4"})
	ch, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "search hi"}},
		Tools:    []ToolDefinition{{Name: "search_files", Description: "search"}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var content string
	var toolCalls []ToolCall
	for tok := range ch {
		if tok.Err != nil {
			t.Fatalf("token error: %v", tok.Err)
		}
		content += tok.Content
		if tok.Done {
			toolCalls = tok.ToolCalls
			break
		}
	}

	if content != "" {
		t.Errorf("expected no streamed text content with tool call, got %q", content)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool call ID = %q, want call_abc", tc.ID)
	}
	if tc.Name != "search_files" {
		t.Errorf("tool call name = %q, want search_files", tc.Name)
	}
	if got, want := tc.Arguments["query"], "hi"; got != want {
		t.Errorf("tool call arguments[query] = %v, want %q (full args: %#v)", got, want, tc.Arguments)
	}
}

func TestOpenAIClientStreamRecoversToolCallArgumentsWithTrailingComma(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_doc","type":"function","function":{"name":"create_docx","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"filename\":\"report.docx\",\"blocks\":[]}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":","}}]}}]}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			w.Write([]byte("data: " + c + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "gpt-4"})
	ch, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "make doc"}},
		Tools:    []ToolDefinition{{Name: "create_docx", Description: "create doc"}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var toolCalls []ToolCall
	for tok := range ch {
		if tok.Err != nil {
			t.Fatalf("token error: %v", tok.Err)
		}
		if tok.Done {
			toolCalls = tok.ToolCalls
			break
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls len = %d, want 1", len(toolCalls))
	}
	if got := toolCalls[0].Arguments["filename"]; got != "report.docx" {
		t.Fatalf("filename arg = %v, want report.docx", got)
	}
}

func TestOpenAIClientStreamRecoversFirstToolCallArgumentObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_doc","type":"function","function":{"name":"create_docx","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"filename\":\"report.docx\",\"blocks\":[]}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":" this trailing prose should be ignored"}}]}}]}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			w.Write([]byte("data: " + c + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "gpt-4"})
	ch, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "make doc"}},
		Tools:    []ToolDefinition{{Name: "create_docx", Description: "create doc"}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var toolCalls []ToolCall
	for tok := range ch {
		if tok.Err != nil {
			t.Fatalf("token error: %v", tok.Err)
		}
		if tok.Done {
			toolCalls = tok.ToolCalls
			break
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls len = %d, want 1", len(toolCalls))
	}
	if got := toolCalls[0].Arguments["filename"]; got != "report.docx" {
		t.Fatalf("filename arg = %v, want report.docx", got)
	}
}

func TestParseToolCallArgumentsRepairsMissingArrayCloser(t *testing.T) {
	args, err := parseToolCallArguments("create_docx", `{"filename":"report.docx","blocks":[{"kind":"paragraph","text":"ok"}}`)
	if err != nil {
		t.Fatalf("parseToolCallArguments() error = %v", err)
	}
	if got := args["filename"]; got != "report.docx" {
		t.Fatalf("filename = %v, want report.docx", got)
	}
	blocks, ok := args["blocks"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("blocks = %#v, want one block", args["blocks"])
	}
}

func TestOpenAIClientStreamKeepsUsageAfterFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"ok"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13}}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			w.Write([]byte("data: " + c + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "gpt-4"})
	ch, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var content string
	var usage TokenUsage
	for tok := range ch {
		if tok.Err != nil {
			t.Fatalf("token error: %v", tok.Err)
		}
		if tok.Done {
			usage = tok.Usage
			break
		}
		content += tok.Content
	}

	if content != "ok" {
		t.Fatalf("content = %q, want ok", content)
	}
	if usage.PromptTokens != 11 || usage.CompletionTokens != 2 || usage.TotalTokens != 13 {
		t.Fatalf("usage = %+v, want 11/2/13", usage)
	}
}

func TestOpenAIClientStreamAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "overloaded"}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	_, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 503 response")
	}
}

func TestOpenAIClientSendNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices": []}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	_, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestRetryClientWithOpenAIClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"choices": [{"message": {"role": "assistant", "content": "Success"}}]}`))
	}))
	defer server.Close()

	openai := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	retry := NewRetryClient(openai, RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	})

	result, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.Content != "Success" {
		t.Errorf("Content = %q, want %q", result.Content, "Success")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

// TestOpenAIClientSendParsesReasoningResponse verifies that the response
// parser lifts the provider-side reasoning fields onto Response and
// TokenUsage. Tested against the OpenRouter wire shape observed in a
// real probe against DeepSeek V4 Flash on 2026-05-11.
func TestOpenAIClientSendParsesReasoningResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices":[{"message":{
				"role":"assistant",
				"content":"Final answer.",
				"reasoning":"thinking out loud about the answer",
				"reasoning_details":[{"type":"reasoning.text","text":"thinking out loud","format":"unknown","index":0}]
			}}],
			"usage":{
				"prompt_tokens":100,
				"completion_tokens":50,
				"total_tokens":150,
				"completion_tokens_details":{"reasoning_tokens":20}
			}
		}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "deepseek/deepseek-v4-flash"})
	resp, err := client.Send(context.Background(), Request{
		Messages:        []Message{{Role: "user", Content: "hi"}},
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.Content != "Final answer." {
		t.Errorf("Content = %q, want %q", resp.Content, "Final answer.")
	}
	if resp.Reasoning != "thinking out loud about the answer" {
		t.Errorf("Reasoning = %q", resp.Reasoning)
	}
	if !strings.Contains(string(resp.ReasoningDetails), "reasoning.text") {
		t.Errorf("ReasoningDetails missing structured type: %s", resp.ReasoningDetails)
	}
	if resp.Usage.ReasoningTokens != 20 {
		t.Errorf("ReasoningTokens = %d, want 20", resp.Usage.ReasoningTokens)
	}
	if resp.Usage.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", resp.Usage.TotalTokens)
	}
}

func TestOpenAIClientSendOmitsReasoningTokensWhenAbsent(t *testing.T) {
	// Vanilla (non-reasoning) provider response — no completion_tokens_details
	// block, no reasoning field. Response.Reasoning empty and ReasoningTokens=0.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"hi"}}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "gpt-4o"})
	resp, err := client.Send(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.Reasoning != "" {
		t.Errorf("Reasoning unexpectedly populated: %q", resp.Reasoning)
	}
	if len(resp.ReasoningDetails) != 0 {
		t.Errorf("ReasoningDetails unexpectedly populated: %s", resp.ReasoningDetails)
	}
	if resp.Usage.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %d, want 0", resp.Usage.ReasoningTokens)
	}
}

// TestOpenAIClientSendReasoningWireFormat verifies that ReasoningEffort
// reaches the JSON request body in the two shapes providers expect:
// the top-level reasoning_effort string (OpenAI o-series / gpt-5*) and
// the nested reasoning object (OpenRouter / DeepSeek V4 Flash / Anthropic
// passthroughs). Providers without reasoning support ignore unknown fields.
// TestPromptCacheKeySerialization verifies that a non-empty PromptCacheKey on
// the Request is forwarded as "prompt_cache_key" in the JSON body for both
// Send and Stream, and that an empty key produces no field in the body.
func TestPromptCacheKeySerialization(t *testing.T) {
	cases := []struct {
		name           string
		promptCacheKey string
		wantPresent    bool
	}{
		{name: "non-empty key forwarded", promptCacheKey: "thread-abc123", wantPresent: true},
		{name: "empty key omitted", promptCacheKey: "", wantPresent: false},
	}

	for _, tc := range cases {
		t.Run("Send/"+tc.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`))
			}))
			defer server.Close()

			client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "test"})
			_, err := client.Send(context.Background(), Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				PromptCacheKey: tc.promptCacheKey,
			})
			if err != nil {
				t.Fatalf("Send error: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("body not JSON: %v\n%s", err, body)
			}
			got, present := parsed["prompt_cache_key"].(string)
			if present != tc.wantPresent {
				t.Errorf("prompt_cache_key present = %t, want %t\nbody=%s", present, tc.wantPresent, body)
			}
			if tc.wantPresent && got != tc.promptCacheKey {
				t.Errorf("prompt_cache_key = %q, want %q\nbody=%s", got, tc.promptCacheKey, body)
			}
		})

		t.Run("Stream/"+tc.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				flusher, _ := w.(http.Flusher)
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
				flusher.Flush()
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
			}))
			defer server.Close()

			client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "test"})
			ch, err := client.Stream(context.Background(), Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				PromptCacheKey: tc.promptCacheKey,
			})
			if err != nil {
				t.Fatalf("Stream error: %v", err)
			}
			for tok := range ch {
				if tok.Done {
					break
				}
			}
			var parsed map[string]any
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("body not JSON: %v\n%s", err, body)
			}
			got, present := parsed["prompt_cache_key"].(string)
			if present != tc.wantPresent {
				t.Errorf("prompt_cache_key present = %t, want %t\nbody=%s", present, tc.wantPresent, body)
			}
			if tc.wantPresent && got != tc.promptCacheKey {
				t.Errorf("prompt_cache_key = %q, want %q\nbody=%s", got, tc.promptCacheKey, body)
			}
		})
	}
}

func TestOpenAIClientSendReasoningWireFormat(t *testing.T) {
	cases := []struct {
		name        string
		effort      string
		wantTop     string // value of reasoning_effort top-level field, "" means absent
		wantNested  bool   // whether reasoning object is present
		wantEnabled bool   // whether reasoning.enabled is true
		wantEffort  string // value of reasoning.effort, "" means absent
	}{
		{name: "empty disables both shapes", effort: ""},
		{name: "none disables both shapes", effort: "none"},
		{name: "off disables both shapes", effort: "off"},
		{name: "true emits enabled-only", effort: "true", wantNested: true, wantEnabled: true},
		{name: "enabled emits enabled-only", effort: "enabled", wantNested: true, wantEnabled: true},
		{name: "high emits both shapes", effort: "high", wantTop: "high", wantNested: true, wantEnabled: true, wantEffort: "high"},
		{name: "xhigh emits both shapes", effort: "xhigh", wantTop: "xhigh", wantNested: true, wantEnabled: true, wantEffort: "xhigh"},
		{name: "minimal emits both shapes", effort: "minimal", wantTop: "minimal", wantNested: true, wantEnabled: true, wantEffort: "minimal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`))
			}))
			defer server.Close()

			client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "deepseek/deepseek-v4-flash"})
			_, err := client.Send(context.Background(), Request{
				Messages:        []Message{{Role: "user", Content: "hi"}},
				ReasoningEffort: tc.effort,
			})
			if err != nil {
				t.Fatalf("Send error: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("body not JSON: %v\n%s", err, body)
			}
			got, _ := parsed["reasoning_effort"].(string)
			if got != tc.wantTop {
				t.Errorf("reasoning_effort = %q, want %q\nbody=%s", got, tc.wantTop, body)
			}
			nested, hasNested := parsed["reasoning"].(map[string]any)
			if hasNested != tc.wantNested {
				t.Errorf("reasoning nested present = %t, want %t\nbody=%s", hasNested, tc.wantNested, body)
			}
			if tc.wantNested {
				if enabled, _ := nested["enabled"].(bool); enabled != tc.wantEnabled {
					t.Errorf("reasoning.enabled = %t, want %t\nbody=%s", enabled, tc.wantEnabled, body)
				}
				if eff, _ := nested["effort"].(string); eff != tc.wantEffort {
					t.Errorf("reasoning.effort = %q, want %q\nbody=%s", eff, tc.wantEffort, body)
				}
			}
		})
	}
}
