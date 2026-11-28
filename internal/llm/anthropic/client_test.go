package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

const houseKey = "sk-ant-api03-house-key-for-tests-0123456789"

func staticKey(key string) KeySource {
	return func(context.Context) (string, error) { return key, nil }
}

func testConfig() llm.Config {
	return llm.Config{Provider: llm.AnthropicProvider, Model: "claude-sonnet-5-5", MaxTokens: 32_768, MaxOutputTokens: 64_000, ShowReasoning: true, ConnectTimeoutSec: 5}
}

func fixtureClient(t *testing.T, cfg llm.Config, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	hc := server.Client()
	transport := hc.Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	hc.Transport = transport
	t.Cleanup(hc.CloseIdleConnections)
	client := newClient(cfg, staticKey(houseKey), hc, server.URL)
	client.logger = func() *slog.Logger { return slog.New(slog.DiscardHandler) }
	return client
}

type captured struct {
	body   map[string]any
	header http.Header
}

// recordingClient answers every request with events and hands each decoded body and its
// headers to the test through a channel, which is also what orders the handler's writes
// before the test's reads.
func recordingClient(t *testing.T, cfg llm.Config, events []sseEvent) (*Client, <-chan captured) {
	t.Helper()
	sent := make(chan captured, 8)
	client := fixtureClient(t, cfg, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body: %v", err)
		}
		sent <- captured{body: body, header: r.Header.Clone()}
		writeEvents(w, events...)
	})
	return client, sent
}

type sseEvent struct{ name, data string }

func writeEvents(w http.ResponseWriter, events ...sseEvent) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, e.data)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func messageStart(input, read, write int64, transformations string) sseEvent {
	return sseEvent{"message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"output_tokens":1},"input_transformations":%s}}`, input, read, write, transformations)}
}

func blockStart(index int, block string) sseEvent {
	return sseEvent{"content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, index, block)}
}

func delta(index int, kind, field, value string) sseEvent {
	return sseEvent{"content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":%q,%q:%s}}`, index, kind, field, quote(value))}
}

func blockStop(index int) sseEvent {
	return sseEvent{"content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index)}
}

func messageDelta(stop string, output int64) sseEvent {
	return sseEvent{"message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"output_tokens":%d}}`, stop, output)}
}

func messageStop() sseEvent { return sseEvent{"message_stop", `{"type":"message_stop"}`} }

func textBlock(index int, text string) []sseEvent {
	return []sseEvent{blockStart(index, `{"type":"text","text":""}`), delta(index, "text_delta", "text", text), blockStop(index)}
}

func thinkingBlock(index int, thinking, signature string) []sseEvent {
	events := []sseEvent{blockStart(index, `{"type":"thinking","thinking":"","signature":""}`)}
	if thinking != "" {
		events = append(events, delta(index, "thinking_delta", "thinking", thinking))
	}
	return append(events, delta(index, "signature_delta", "signature", signature), blockStop(index))
}

func toolBlock(index int, id, name string, parts ...string) []sseEvent {
	events := []sseEvent{blockStart(index, fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":{}}`, id, name))}
	for _, part := range parts {
		events = append(events, delta(index, "input_json_delta", "partial_json", part))
	}
	return append(events, blockStop(index))
}

func reply(blocks ...[]sseEvent) []sseEvent {
	events := []sseEvent{messageStart(10, 0, 0, "[]")}
	for _, block := range blocks {
		events = append(events, block...)
	}
	return events
}

func textReply(text string) []sseEvent {
	return append(reply(textBlock(0, text)), messageDelta("end_turn", 5), messageStop())
}

func drain(t *testing.T, client *Client, req llm.Request) []llm.Chunk {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func userRequest(text string) llm.Request {
	return llm.Request{Model: "claude-sonnet-5-5", Messages: []llm.Message{{Role: llm.RoleUser, Content: text}}}
}

func lastErr(t *testing.T, chunks []llm.Chunk) error {
	t.Helper()
	if len(chunks) == 0 || chunks[len(chunks)-1].Err == nil {
		t.Fatalf("stream did not end on an error: %#v", chunks)
	}
	for _, chunk := range chunks {
		if chunk.ToolCall != nil || chunk.FinishReason != "" || chunk.ProviderState != nil {
			t.Fatalf("failed stream still finished: %#v", chunk)
		}
	}
	return chunks[len(chunks)-1].Err
}

func TestSDKIgnoresTheEnvironment(t *testing.T) {
	var decoyHits atomic.Int32
	decoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		decoyHits.Add(1)
		writeEvents(w, textReply("decoy")...)
	}))
	t.Cleanup(decoy.Close)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-env-decoy-key-0000000000")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-decoy-token")
	t.Setenv("ANTHROPIC_BASE_URL", decoy.URL)
	// Explicit options outrank the other variables anyway; only WithoutEnvironmentDefaults
	// keeps the SDK from adding these headers to every request.
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Env-Decoy: leaked")
	client, sent := recordingClient(t, testConfig(), textReply("ok"))
	chunks := drain(t, client, userRequest("hi"))
	if chunks[0].Text != "ok" || decoyHits.Load() != 0 {
		t.Fatalf("chunks %#v decoy hits %d", chunks, decoyHits.Load())
	}
	header := (<-sent).header
	if decoyHeader := header.Get("X-Env-Decoy"); decoyHeader != "" {
		t.Fatalf("ANTHROPIC_CUSTOM_HEADERS reached the wire: %q", decoyHeader)
	}
	if auth, apiKey := header.Get("Authorization"), header.Get("X-Api-Key"); auth != "Bearer "+houseKey || apiKey != "" {
		t.Fatalf("credential headers: authorization %q x-api-key %q", auth, apiKey)
	}
	if New(testConfig(), staticKey(houseKey)).baseURL != llm.AnthropicBaseURL {
		t.Fatal("New must pin the public endpoint")
	}
}

func TestKeyIsReadOnEveryCall(t *testing.T) {
	var calls atomic.Int32
	client, sent := recordingClient(t, testConfig(), textReply("ok"))
	client.key = func(context.Context) (string, error) {
		return fmt.Sprintf("sk-ant-rotated-%d", calls.Add(1)), nil
	}
	for i := 1; i <= 2; i++ {
		drain(t, client, userRequest("hi"))
		if auth := (<-sent).header.Get("Authorization"); auth != fmt.Sprintf("Bearer sk-ant-rotated-%d", i) || calls.Load() != int32(i) {
			t.Fatalf("call %d: key reads %d, sent %q", i, calls.Load(), auth)
		}
	}
}

func TestNoKeyNeverDials(t *testing.T) {
	var hits atomic.Int32
	readFailed := errors.New("store sealed")
	for _, tc := range []struct {
		name string
		key  KeySource
		want error
	}{
		{"nil source", nil, ErrNoKey},
		{"empty", staticKey(""), ErrNoKey},
		{"blank", staticKey("  \n"), ErrNoKey},
		{"read error", func(context.Context) (string, error) { return "", readFailed }, readFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, testConfig(), func(http.ResponseWriter, *http.Request) { hits.Add(1) })
			client.key = tc.key
			stream, err := client.Stream(context.Background(), userRequest("hi"))
			if stream != nil || !errors.Is(err, tc.want) {
				t.Fatalf("stream %v err %v, want %v", stream, err, tc.want)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("a keyless call dialled %d times", hits.Load())
	}
}

func TestStreamIdleTimeoutIsRetryable(t *testing.T) {
	cfg := testConfig()
	cfg.StreamIdleTimeoutSec = 1
	client := fixtureClient(t, cfg, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeEvents(w, messageStart(10, 0, 0, "[]"))
		<-r.Context().Done()
	})
	err := lastErr(t, drain(t, client, userRequest("hi")))
	if !errors.Is(err, openai_compat.ErrStreamIdleTimeout) || !agent.IsTransientLLMError(err) {
		t.Fatalf("idle stall: %v", err)
	}
}

func TestHTTPErrorsKeepTheirClass(t *testing.T) {
	for _, tc := range []struct {
		status            int
		errorType, header string
		retryable         bool
		mentions          []string
		retryAfter        int
	}{
		{401, "authentication_error", "", false, []string{"Settings"}, 0},
		{403, "permission_error", "", false, []string{"refused the request", "Console"}, 0},
		{402, "billing_error", "", false, []string{"Settings", "Console"}, 0},
		{400, "invalid_request_error", "", false, nil, 0},
		{429, "rate_limit_error", "2", true, nil, 2},
		{529, "overloaded_error", "", true, nil, 0},
	} {
		t.Run(tc.errorType, func(t *testing.T) {
			var hits atomic.Int32
			client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("request-id", "req_"+tc.errorType)
				if tc.header != "" {
					w.Header().Set("retry-after", tc.header)
				}
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"type":"error","error":{"type":%q,"message":"refused %s"}}`, tc.errorType, houseKey)
			})
			stream, err := client.Stream(context.Background(), userRequest("hi"))
			httpErr, ok := errors.AsType[*openai_compat.HTTPError](err)
			if stream != nil || !ok || httpErr.StatusCode != tc.status || hits.Load() != 1 {
				t.Fatalf("err %v hits %d", err, hits.Load())
			}
			if agent.IsTransientLLMError(err) != tc.retryable || httpErr.RetryAfterSec != tc.retryAfter || httpErr.RequestID != "req_"+tc.errorType {
				t.Fatalf("class of %v: retry-after %d request id %q", err, httpErr.RetryAfterSec, httpErr.RequestID)
			}
			if strings.Contains(err.Error(), houseKey) || strings.Contains(httpErr.Body, houseKey) {
				t.Fatalf("the house key leaked: %v", err)
			}
			for _, word := range tc.mentions {
				if !strings.Contains(err.Error(), word) {
					t.Fatalf("%v does not name %s", err, word)
				}
			}
			if tc.status == http.StatusForbidden && strings.Contains(err.Error(), "API key") {
				t.Fatalf("a permission error blamed the key: %v", err)
			}
		})
	}
}

func midStreamError(errorType string) []sseEvent {
	data := fmt.Sprintf(`{"type":"error","error":{"type":%q,"message":"stream failed %s"}}`, errorType, houseKey)
	return append(reply(textBlock(0, "partial")), sseEvent{"error", data})
}

func TestMidStreamOverloadIsRetryable(t *testing.T) {
	client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeEvents(w, midStreamError("overloaded_error")...)
	})
	err := lastErr(t, drain(t, client, userRequest("hi")))
	httpErr, ok := errors.AsType[*openai_compat.HTTPError](err)
	if !ok || httpErr.StatusCode != 529 || !agent.IsTransientLLMError(err) || strings.Contains(err.Error(), houseKey) {
		t.Fatalf("mid-stream overload: %v", err)
	}
}

func TestMidStreamErrorTypesMapToStatus(t *testing.T) {
	for errorType, status := range map[string]int{
		"rate_limit_error": 429, "api_error": 500, "timeout_error": 500, "authentication_error": 401,
		"permission_error": 403, "billing_error": 402, "invalid_request_error": 400, "some_future_error": 500,
	} {
		t.Run(errorType, func(t *testing.T) {
			client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				writeEvents(w, midStreamError(errorType)...)
			})
			err := lastErr(t, drain(t, client, userRequest("hi")))
			httpErr, ok := errors.AsType[*openai_compat.HTTPError](err)
			if !ok || httpErr.StatusCode != status || strings.Contains(httpErr.Body, houseKey) {
				t.Fatalf("%s: %v", errorType, err)
			}
			if agent.IsTransientLLMError(err) != (status == 429 || status >= 500) {
				t.Fatalf("%s retry class wrong: %v", errorType, err)
			}
		})
	}
}

func TestRequestLogLineCarriesNoContentOrKey(t *testing.T) {
	client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		events := []sseEvent{messageStart(1000, 16002, 300, `[{"type":"thinking_dropped","path":"messages.1.content.0"},{"type":"thinking_mismatch_allowed","path":"messages.1.content.1"},{"type":"thinking_dropped","path":"messages.1.content.2"}]`)}
		events = append(events, thinkingBlock(0, "secret thinking text", "sig-1")...)
		events = append(events, toolBlock(1, "toolu_2", "lookup", `{"query":"secret tool argument"}`)...)
		writeEvents(w, append(events, messageDelta("tool_use", 50), messageStop())...)
	})
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	client.logger = func() *slog.Logger { return logger }
	client.cfg.Prices = map[string]llm.Price{"claude-sonnet-5-5": {InputPer1M: 2, OutputPer1M: 10, CacheReadPer1M: 0.1, CacheWritePer1M: 2.5}}
	req := replayRequest()
	req.Reasoning.Effort = llm.ReasoningEffortHigh
	drain(t, client, req)
	line := buf.String()
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("want one log line, got %q", line)
	}
	for _, want := range []string{"model=claude-sonnet-5-5", "effort=high", "replayed_state_messages=1", "dropped_blocks=2", "input_tokens=1000", "cache_read_tokens=16002", "cache_write_tokens=300", "output_tokens=50", "cost_usd="} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q lacks %s", line, want)
		}
	}
	for _, leak := range []string{houseKey, "secret", "Be careful", "earlier thought"} {
		if strings.Contains(line, leak) {
			t.Fatalf("log line leaked %q: %s", leak, line)
		}
	}
}

func TestNewRefusesRedirectsAndKeepAlives(t *testing.T) {
	hc := New(testConfig(), staticKey(houseKey)).httpClient
	if err := hc.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy %v", err)
	}
	if !hc.Transport.(*http.Transport).DisableKeepAlives {
		t.Fatal("keep-alives on")
	}
}

func TestErrorsAreScrubbedAndBounded(t *testing.T) {
	dial := errors.New("dial tcp: " + houseKey)
	if err := providerError(dial, houseKey); strings.Contains(err.Error(), houseKey) || !strings.Contains(err.Error(), "dial tcp") {
		t.Fatalf("transport error %v", err)
	}
	if plain := errors.New("plain"); providerError(plain, houseKey) != plain || providerError(plain, "") != plain {
		t.Fatal("an error with no key in it was rewritten")
	}
	apiErr := &anthropic.Error{StatusCode: http.StatusOK, RequestID: "req " + strings.Repeat("x", 200)}
	if err := apiErr.UnmarshalJSON([]byte(`{"type":"error","error":{"type":"api_error","message":"` + strings.Repeat("y", 70<<10) + `"}}`)); err != nil {
		t.Fatal(err)
	}
	result, ok := errors.AsType[*openai_compat.HTTPError](providerError(apiErr, houseKey))
	if !ok || result.StatusCode != 500 || len(result.Body) != maxErrorBodyBytes || len(result.RequestID) != maxRequestIDBytes {
		t.Fatalf("unbounded error: %#v", result)
	}
}

// keyAcrossTheCap is an error body whose house key starts 10 bytes before the 64 KiB cap.
func keyAcrossTheCap(errorType string) string {
	head := `{"type":"error","error":{"type":"` + errorType + `","message":"`
	return head + strings.Repeat("y", maxErrorBodyBytes-len(head)-10) + houseKey + `"}}`
}

func TestKeyAcrossTheBodyCapIsScrubbed(t *testing.T) {
	apiErr := &anthropic.Error{StatusCode: http.StatusOK}
	if err := apiErr.UnmarshalJSON([]byte(keyAcrossTheCap("api_error"))); err != nil {
		t.Fatal(err)
	}
	streamed, _ := errors.AsType[*openai_compat.HTTPError](providerError(apiErr, houseKey))
	client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, keyAcrossTheCap("api_error"))
	})
	_, err := client.Stream(context.Background(), userRequest("hi"))
	opened, _ := errors.AsType[*openai_compat.HTTPError](err)
	for name, result := range map[string]*openai_compat.HTTPError{"mid-stream": streamed, "non-2xx": opened} {
		if result == nil || len(result.Body) > maxErrorBodyBytes || strings.Contains(result.Body, houseKey[:6]) || !strings.Contains(result.Body, redacted) {
			t.Fatalf("%s: key prefix left in a bounded body: %v", name, result)
		}
	}
}
