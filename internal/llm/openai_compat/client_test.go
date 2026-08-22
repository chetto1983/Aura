package openai_compat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

const sentinelKey = "sk-or-SENTINEL-DO-NOT-LEAK-0123456789"

// testConfig returns a Config pointing at srvURL with the sentinel key and the
// standard attribution headers.
func testConfig(srvURL string) llm.Config {
	return llm.Config{
		BaseURL:           srvURL,
		APIKey:            sentinelKey,
		Model:             "deepseek/deepseek-v4-flash:exacto",
		Temperature:       0.7,
		MaxTokens:         4096,
		ConnectTimeoutSec: 10,
		Headers: map[string]string{
			"HTTP-Referer": "https://github.com/chetto1983/aura",
			"X-Title":      "Aura",
		},
	}
}

// fixtureHandler serves a testdata fixture as text/event-stream.
func fixtureHandler(t *testing.T, name string) http.HandlerFunc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(data)
	}
}

// drain reads a chunk channel to completion, returning the chunks.
func drain(ch <-chan llm.Chunk) []llm.Chunk {
	var out []llm.Chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

// TestStream_EndToEnd replays text_stop.sse over httptest through the real
// Client.Stream goroutine path (not just parseSSE).
func TestStream_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(fixtureHandler(t, "text_stop.sse"))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(ch)

	var text, finish string
	for _, ck := range chunks {
		text += ck.Text
		if ck.FinishReason != "" {
			finish = ck.FinishReason
		}
	}
	if text != "Ciao, come stai?" || finish != "stop" {
		t.Errorf("text=%q finish=%q, want full text + stop", text, finish)
	}
}

func TestStreamOmitsAuthorizationWhenAPIKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.APIKey = ""
	c := New(cfg)
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_ = drain(ch)

	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want omitted for local keyless clients", gotAuth)
	}
}

// TestStream_CancelMidStream (Req#3): cancel ctx mid-stream → the chunk channel
// closes within ~100ms of cancel AND runtime.NumGoroutine returns to baseline.
// goleak (package TestMain) catches a leaked read goroutine or lingering
// persistConn. The server streams one delta then blocks (never sends [DONE]),
// modelling a slow/hung upstream; only ctx-cancel can end the read.
func TestStream_PrematureCloseEmitsErrBeforeClose(t *testing.T) {
	srv := httptest.NewServer(fixtureHandler(t, "premature_close.sse"))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(ch)

	var text strings.Builder
	errIndex := -1
	for i, ck := range chunks {
		text.WriteString(ck.Text)
		if ck.Err != nil {
			errIndex = i
		}
	}
	if text.String() == "" {
		t.Fatal("premature fixture emitted no partial text; regression is not exercising truncation")
	}
	if errIndex < 0 {
		t.Fatalf("stream emitted chunks %#v; want terminal Err chunk before close", chunks)
	}
	if errIndex != len(chunks)-1 {
		t.Fatalf("Err chunk index = %d, want final chunk index %d", errIndex, len(chunks)-1)
	}
	if !strings.Contains(chunks[errIndex].Err.Error(), "malformed SSE chunk") {
		t.Fatalf("Err = %v, want malformed SSE chunk error", chunks[errIndex].Err)
	}
}

func TestStream_EOFWithoutFinishReasonEmitsUsageThenErr(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(ch)

	usageIndex := -1
	errIndex := -1
	for i, ck := range chunks {
		if ck.Usage != nil {
			usageIndex = i
		}
		if ck.Err != nil {
			errIndex = i
		}
	}
	if usageIndex < 0 {
		t.Fatalf("chunks %#v; want trailing usage before terminal Err", chunks)
	}
	if errIndex < 0 {
		t.Fatalf("chunks %#v; want terminal Err for EOF without finish_reason", chunks)
	}
	if usageIndex >= errIndex {
		t.Fatalf("usage index = %d err index = %d, want usage before terminal Err", usageIndex, errIndex)
	}
	if !errors.Is(chunks[errIndex].Err, errStreamMissingFinishReason) {
		t.Fatalf("Err = %v, want errStreamMissingFinishReason", chunks[errIndex].Err)
	}
}

func TestStream_CancelMidStream(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"start\"},\"finish_reason\":null}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the client cancels (its request ctx fires) or the test
		// releases us, so the handler goroutine never lingers past the test.
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	defer srv.Close()
	defer close(released)

	baseline := runtime.NumGoroutine()

	c := New(testConfig(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Stream(ctx, llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Receive the first chunk to confirm the stream is live, then cancel.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("never received the first chunk")
	}
	cancel()

	// The channel must close within ~100ms of cancel.
	deadline := time.After(500 * time.Millisecond)
	start := time.Now()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				if d := time.Since(start); d > 200*time.Millisecond {
					t.Errorf("channel closed %v after cancel, want ~100ms", d)
				}
				goto closed
			}
		case <-deadline:
			t.Fatal("channel did not close within 500ms of cancel")
		}
	}
closed:
	// Goroutine count returns to baseline (poll briefly — teardown is async).
	for range 50 {
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines = %d, baseline %d — read goroutine leaked", runtime.NumGoroutine(), baseline)
}

// TestStream_RequestErrors covers Stream's two reachable pre-flight error returns
// (robustness): a malformed BaseURL that http.NewRequestWithContext rejects, and a
// syntactically valid but unsupported scheme that httpClient.Do rejects. Both must
// return (nil, err) synchronously — never a panic, never a leaked goroutine, and
// never the API key in the error string (D-28).
func TestStream_RequestErrors(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
	}{
		{"malformed_url_newrequest", "http://\x7f-control-char"},
		{"unsupported_scheme_do", "ftp://127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(testConfig(tc.baseURL))
			ch, err := c.Stream(context.Background(), llm.Request{
				Model:    "m",
				Messages: []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
			})
			if err == nil {
				t.Fatal("expected a synchronous error, got nil")
			}
			if ch != nil {
				t.Fatalf("channel must be nil on a pre-flight error, got %v", ch)
			}
			if strings.Contains(err.Error(), sentinelKey) {
				t.Fatalf("API key leaked into the error: %v", err)
			}
		})
	}
}

// TestStream_429NoRetry (Req#4): a 429 with Retry-After yields HTTPError and the
// server sees exactly 1 request (the wire does zero retries).
func TestStream_429NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if ch != nil {
		t.Error("Stream returned a non-nil channel on 429")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if herr.StatusCode != 429 || herr.RetryAfterSec != 5 {
		t.Errorf("HTTPError = {%d, %d}, want {429, 5}", herr.StatusCode, herr.RetryAfterSec)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server saw %d requests, want exactly 1 (no retry)", got)
	}
}

// TestRequestBody asserts the wire body sends tool_choice:auto +
// provider.data_collection:deny + stream:true, and sends NEITHER usage NOR
// stream_options — both deprecated no-ops on this OpenRouter-scoped contract
// (testConfig sets no Provider, so llm.ReasoningTarget resolves non-llamacpp,
// the same branch OpenRouter takes — see TestRequestBody_LlamaCppStreamOptions
// in client_llamacpp_stream_test.go for the llama.cpp counterpart, which DOES
// send stream_options). Attribution headers + the Bearer auth are present on
// the request.
func TestRequestBody(t *testing.T) {
	var gotBody []byte
	var gotAuth, gotReferer, gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{
		Model: "deepseek/deepseek-v4-flash:exacto", Temperature: 0.7, MaxTokens: 4096,
		SessionID: "conv-123",
		Messages:  []llm.Message{{Role: "user", Content: "ciao"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	drain(ch)

	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if body["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", body["tool_choice"])
	}
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
	if body["session_id"] != "conv-123" {
		t.Errorf("session_id = %v, want conv-123", body["session_id"])
	}
	prov, _ := body["provider"].(map[string]any)
	if prov == nil || prov["data_collection"] != "deny" {
		t.Errorf("provider = %v, want {data_collection: deny}", body["provider"])
	}
	if _, ok := body["usage"]; ok {
		t.Error("request body contains a `usage` key (deprecated no-op — must not be sent)")
	}
	if _, ok := body["stream_options"]; ok {
		t.Error("request body contains a `stream_options` key (deprecated no-op — must not be sent)")
	}
	if gotAuth != "Bearer "+sentinelKey {
		t.Errorf("Authorization header = %q, want Bearer <key>", gotAuth)
	}
	if gotReferer != "https://github.com/chetto1983/aura" || gotTitle != "Aura" {
		t.Errorf("attribution headers = (%q,%q), want repo URL + Aura", gotReferer, gotTitle)
	}
}

// captureBody runs one Stream against a body-capturing server and returns the
// raw request body bytes. The fixed [DONE] response keeps the call cheap.
func captureBody(t *testing.T, req llm.Request) []byte {
	t.Helper()
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer srv.Close()

	ch, err := New(testConfig(srv.URL)).Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	drain(ch)
	return gotBody
}

// TestRequestBody_ToolChoice (Req#1) asserts the wire-layer interpretation of
// llm.Request.ToolChoice: "none" sends tool_choice:"none" with NO tools key
// (forcing prose), and the empty default marshals byte-identically to an
// explicit "auto" — so existing callers are not perturbed (landmine #8).
func TestRequestBody_ToolChoice(t *testing.T) {
	tool := llm.ToolDef{Type: "function"}
	tool.Function.Name = "text_response"
	tool.Function.Description = "reply"
	base := llm.Request{
		Model:     "deepseek/deepseek-v4-flash:exacto",
		Messages:  []llm.Message{{Role: "user", Content: "ciao"}},
		Tools:     []llm.ToolDef{tool},
		SessionID: "conv-123",
	}

	t.Run("none_omits_tools", func(t *testing.T) {
		none := base
		none.ToolChoice = "none"
		var body map[string]any
		if err := json.Unmarshal(captureBody(t, none), &body); err != nil {
			t.Fatalf("request body not JSON: %v", err)
		}
		if body["tool_choice"] != "none" {
			t.Errorf("tool_choice = %v, want none", body["tool_choice"])
		}
		if _, ok := body["tools"]; ok {
			t.Error("tools key present on a ToolChoice:none request (must be omitted to force prose)")
		}
	})

	t.Run("empty_byte_identical_to_auto", func(t *testing.T) {
		empty := base // ToolChoice == ""
		explicit := base
		explicit.ToolChoice = "auto"
		emptyBody := captureBody(t, empty)
		autoBody := captureBody(t, explicit)
		if string(emptyBody) != string(autoBody) {
			t.Errorf("empty ToolChoice not byte-identical to explicit auto:\n empty: %s\n auto:  %s", emptyBody, autoBody)
		}
		var body map[string]any
		if err := json.Unmarshal(emptyBody, &body); err != nil {
			t.Fatalf("request body not JSON: %v", err)
		}
		if body["tool_choice"] != "auto" {
			t.Errorf("tool_choice = %v, want auto (empty default)", body["tool_choice"])
		}
		if _, ok := body["tools"]; !ok {
			t.Error("tools key absent on a default (auto) request — tools must be present")
		}
	})
}

// A model card's sampling instructions have to reach the wire, or they are only
// advice. Gemma 4 asks for top_p+top_k; Qwen3.5 adds min_p and presence_penalty.
// Measured against llama-server b9859 on 2026-08-22: top_k IS honoured on this
// endpoint (top_k=1 collapses two runs to identical text, top_k=64 does not), so
// sending them is not decoration.
func TestRequestBody_CardSampling(t *testing.T) {
	topP, topK, minP := 0.95, 64, 0.0
	presence, repetition := 1.5, 1.0
	bodyBytes := captureBody(t, llm.Request{
		Model:       "gemma-4-12b",
		Messages:    []llm.Message{{Role: "user", Content: "ciao"}},
		Temperature: 1.0,
		Sampling: llm.Sampling{
			TopP: &topP, TopK: &topK, MinP: &minP,
			PresencePenalty: &presence, RepetitionPenalty: &repetition,
		},
	})
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	for key, want := range map[string]float64{
		"top_p": 0.95, "top_k": 64, "min_p": 0,
		"presence_penalty": 1.5, "repetition_penalty": 1,
	} {
		got, ok := body[key].(float64)
		if !ok {
			t.Errorf("%s absent from wire: %s", key, bodyBytes)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// The counterpart that protects every existing deployment: an unconfigured
// Sampling puts NOTHING on the wire, so the OpenRouter request stays byte-identical
// to what it was before these knobs existed.
func TestRequestBody_CardSamplingOmittedWhenUnset(t *testing.T) {
	bodyBytes := captureBody(t, llm.Request{
		Model:       "deepseek/deepseek-v4-flash:exacto",
		Messages:    []llm.Message{{Role: "user", Content: "ciao"}},
		Temperature: 0.7,
	})
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	for _, key := range []string{"top_p", "top_k", "min_p", "presence_penalty", "repetition_penalty"} {
		if _, present := body[key]; present {
			t.Errorf("%s must be absent when unconfigured: %s", key, bodyBytes)
		}
	}
}

// TestRequestBody_Reasoning asserts the provider-neutral llm.Request.Reasoning
// field projects to OpenRouter's unified `reasoning` object, including explicit
// exclude:false (the default is false, but Aura sends it intentionally so the
// policy decision is observable on the wire).
func TestRequestBody_Reasoning(t *testing.T) {
	exclude := false
	bodyBytes := captureBody(t, llm.Request{
		Model:       "deepseek/deepseek-v4-flash:exacto",
		Messages:    []llm.Message{{Role: "user", Content: "scrivi uno script"}},
		MaxTokens:   4096,
		ToolChoice:  "auto",
		Reasoning:   llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh, Exclude: &exclude},
		Temperature: 0.2,
	})

	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning == nil {
		t.Fatalf("reasoning object absent from request body: %s", bodyBytes)
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning.effort = %v, want high", reasoning["effort"])
	}
	if reasoning["exclude"] != false {
		t.Errorf("reasoning.exclude = %v, want explicit false", reasoning["exclude"])
	}
}

// TestMessagesImmutable (Req#13 seed): Stream never mutates req.Messages.
func TestMessagesImmutable(t *testing.T) {
	srv := httptest.NewServer(fixtureHandler(t, "text_stop.sse"))
	defer srv.Close()

	msgs := []llm.Message{
		{Role: "system", Content: "you are Aura"},
		{Role: "user", Content: "ciao"},
	}
	before, _ := json.Marshal(msgs)

	c := New(testConfig(srv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m", Messages: msgs})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	drain(ch)

	after, _ := json.Marshal(msgs)
	if string(before) != string(after) {
		t.Errorf("Stream mutated req.Messages:\n before: %s\n after:  %s", before, after)
	}
}

// TestSecretRedaction (D-28, T-03-01, release-blocking): the sentinel API key
// must appear in NO Chunk, NO HTTPError, NO error string. The Authorization
// header is the ONLY place the key is written (TestRequestBody covers that it is
// the request, never a logged/returned struct).
func TestSecretRedaction(t *testing.T) {
	// Success path: assert no chunk carries the key.
	okSrv := httptest.NewServer(fixtureHandler(t, "toolcall_multichunk.sse"))
	defer okSrv.Close()
	c := New(testConfig(okSrv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for _, ck := range drain(ch) {
		blob, _ := json.Marshal(ck)
		if strings.Contains(string(blob), sentinelKey) {
			t.Fatalf("API key leaked into a Chunk: %s", blob)
		}
	}

	// Error path: assert the key is absent from HTTPError + its Error() string.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer errSrv.Close()
	_, herr := New(testConfig(errSrv.URL)).Stream(context.Background(), llm.Request{Model: "m"})
	if herr == nil {
		t.Fatal("want error on 401")
	}
	if strings.Contains(herr.Error(), sentinelKey) {
		t.Fatalf("API key leaked into HTTPError.Error(): %s", herr.Error())
	}
	blob, _ := json.Marshal(herr)
	if strings.Contains(string(blob), sentinelKey) {
		t.Fatalf("API key leaked into serialized HTTPError: %s", blob)
	}
}
