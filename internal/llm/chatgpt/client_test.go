package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

type tokenSource struct {
	token string
	err   error
}

func (s tokenSource) AccessToken(context.Context) (string, error) { return s.token, s.err }

func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	client.Transport = transport
	t.Cleanup(client.CloseIdleConnections)
	return newClient(llm.Config{ShowReasoning: true}, tokenSource{token: "oauth-test-token"}, client, server.URL+"/v1")
}

func event(w http.ResponseWriter, name, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

const completed = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":40,"output_tokens":8,"total_tokens":48,"input_tokens_details":{"cached_tokens":17}}}}`

func drain(t *testing.T, client *Client, req llm.Request) []llm.Chunk {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := client.Stream(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []llm.Chunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func TestStreamingRequestAndHistory(t *testing.T) {
	var body map[string]any
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer oauth-test-token" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hello "}`)
		event(w, "response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","delta":"Checked context."}`)
		event(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"world"}`)
		event(w, "response.completed", completed)
	})
	call := llm.ToolCall{ID: "call_old", Type: "function"}
	call.Function.Name, call.Function.Arguments = "lookup", `{"query":"fact"}`
	tool := llm.ToolDef{Type: "function"}
	tool.Function.Name, tool.Function.Parameters = "lookup", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	req := llm.Request{Model: "account-model", Temperature: 0.9, MaxTokens: 999, Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "Be useful"}, {Role: llm.RoleUser, Content: "First"},
		{Role: llm.RoleAssistant, Content: "Checking", ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleTool, ToolCallID: "call_old", Content: "Found"}, {Role: llm.RoleUser, Content: "Next"},
	}, Tools: []llm.ToolDef{tool}, Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortLow}}
	chunks := drain(t, client, req)
	if len(chunks) != 5 || chunks[0].Text != "Hello " || chunks[1].Reasoning != "Checked context." || chunks[2].Text != "world" || chunks[3].FinishReason != "stop" {
		t.Fatalf("chunks: %#v", chunks)
	}
	if usage := chunks[4].Usage; usage == nil || usage.PromptTokens != 40 || usage.CompletionTokens != 8 || usage.CachedTokens != 17 || usage.Cost != nil {
		t.Fatalf("usage %#v", usage)
	}
	if body["store"] != false || body["stream"] != true || body["model"] != "account-model" {
		t.Fatalf("body %#v", body)
	}
	if reasoning := body["reasoning"].(map[string]any); reasoning["effort"] != "low" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning request %#v", reasoning)
	}
	for _, key := range []string{"max_output_tokens", "temperature", "top_p", "previous_response_id", "conversation", "metadata"} {
		if _, ok := body[key]; ok {
			t.Errorf("unsupported field %s", key)
		}
	}
	input := body["input"].([]any)
	if len(input) != 6 || input[0].(map[string]any)["role"] != "developer" || input[3].(map[string]any)["namespace"] != "aura" || input[4].(map[string]any)["call_id"] != "call_old" {
		t.Fatalf("history %#v", input)
	}
	tools := body["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "namespace" || tools[0].(map[string]any)["name"] != "aura" {
		t.Fatalf("tools %#v", tools)
	}
}

func TestToolsAssembledAfterCompletion(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, "response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","namespace":"aura","name":"lookup","arguments":""}}`)
		event(w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"query\":"}`)
		event(w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"weather\"}"}`)
		event(w, "response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"query\":\"weather\"}"}`)
		event(w, "response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","namespace":"aura","name":"lookup","arguments":"{\"query\":\"weather\"}"}}`)
		event(w, "response.completed", `{"type":"response.completed","response":{"output":[{"id":"fc_1","type":"function_call","call_id":"call_1","namespace":"aura","name":"lookup","arguments":"{\"query\":\"weather\"}"}]}}`)
	})
	chunks := drain(t, client, llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if len(chunks) != 2 || chunks[0].ToolCall == nil || chunks[0].ToolCall.ID != "call_1" || chunks[0].ToolCall.Function.Name != "lookup" || chunks[0].ToolCall.Function.Arguments != `{"query":"weather"}` || chunks[1].FinishReason != "tool_calls" {
		t.Fatalf("chunks %#v", chunks)
	}
}

func TestTerminalFailures(t *testing.T) {
	cases := []struct{ name, data, want string }{
		{"premature", `{"type":"response.output_text.delta","delta":"partial"}`, "without response.completed"},
		{"failed", `{"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"plan exhausted"}}}`, "subscription_sharing_usage_limit_exceeded"},
		{"incomplete", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"content_filter"}}}`, "content_filter"},
		{"error", `{"type":"error","code":"quota","message":"denied"}`, "denied"},
		{"bad-json", `{"type":`, ""},
		{"arguments-first", `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`, "before the function call"},
		{"done-first", `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{}"}`, "before the function call"},
		{"bad-call", `{"type":"response.completed","response":{"output":[{"type":"function_call","call_id":"call1","namespace":"other","name":"lookup","arguments":"{}"}]}}`, "invalid tool call"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				event(w, "fixture", tc.data)
			})
			chunks := drain(t, client, llm.Request{Model: "m"})
			if len(chunks) == 0 || chunks[len(chunks)-1].Err == nil || !strings.Contains(chunks[len(chunks)-1].Err.Error(), tc.want) {
				t.Fatalf("chunks %#v", chunks)
			}
			for _, chunk := range chunks {
				if chunk.FinishReason != "" || chunk.ToolCall != nil {
					t.Fatal("failed stream finished or executed a tool")
				}
			}
		})
	}
}

func TestHTTPFailureNoRetryAndRedacted(t *testing.T) {
	count := 0
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"oauth-test-token exhausted"}}`)
	})
	_, err := client.Stream(context.Background(), llm.Request{Model: "m"})
	httpErr, ok := errors.AsType[*openai_compat.HTTPError](err)
	if !ok || httpErr.StatusCode != 429 || httpErr.RetryAfterSec != 3 || strings.Contains(err.Error(), "oauth-test-token") || strings.Contains(httpErr.Body, "oauth-test-token") || count != 1 {
		t.Fatalf("error %v requests %d", err, count)
	}
}

func TestCancellationAndIdle(t *testing.T) {
	for _, idle := range []bool{false, true} {
		t.Run(fmt.Sprint(idle), func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			if idle {
				client.cfg.StreamIdleTimeoutSec = 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := client.Stream(ctx, llm.Request{Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			if !idle {
				cancel()
			}
			deadline := time.After(3 * time.Second)
			foundIdle := false
			for {
				select {
				case chunk, ok := <-stream:
					if !ok {
						if idle && !foundIdle {
							t.Fatal("missing idle error")
						}
						return
					}
					foundIdle = foundIdle || errors.Is(chunk.Err, openai_compat.ErrStreamIdleTimeout)
				case <-deadline:
					t.Fatal("stream did not close")
				}
			}
		})
	}
}

func TestMissingCredentialDoesNotUseEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "wrong-key")
	for _, source := range []TokenSource{nil, tokenSource{}, tokenSource{err: errors.New("disconnected")}} {
		client := New(llm.Config{BaseURL: "https://attacker.invalid", APIKey: "wrong-key"}, source)
		if client.baseURL != BaseURL {
			t.Fatal("base URL was overridden")
		}
		if _, err := client.Stream(context.Background(), llm.Request{}); err == nil {
			t.Fatal("missing credential accepted")
		}
	}
}
