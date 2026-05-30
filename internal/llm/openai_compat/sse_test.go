package openai_compat

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// replayFixture parses a testdata/*.sse file through parseSSE, collecting every
// emitted Chunk and the captured parse result. It is the deterministic,
// network-free path the wire tests share.
func replayFixture(t *testing.T, name string) ([]llm.Chunk, parseResult) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var chunks []llm.Chunk
	res, perr := parseSSE(bytes.NewReader(data), func(c llm.Chunk) bool {
		chunks = append(chunks, c)
		return true
	})
	if perr != nil {
		t.Fatalf("parseSSE(%s): %v", name, perr)
	}
	return chunks, res
}

// TestStream_TextStop (Req#1): text_stop.sse — the `:` comment and `[DONE]`
// never reach json.Unmarshal; text deltas arrive in order; a final Chunk carries
// finish_reason "stop".
func TestStream_TextStop(t *testing.T) {
	chunks, res := replayFixture(t, "text_stop.sse")

	var text string
	var finish string
	for _, c := range chunks {
		switch {
		case c.Text != "":
			text += c.Text
		case c.FinishReason != "":
			finish = c.FinishReason
		case c.ToolCall != nil:
			t.Errorf("unexpected tool call in a text-only stream: %+v", c.ToolCall)
		}
	}
	if text != "Ciao, come stai?" {
		t.Errorf("assembled text = %q, want %q", text, "Ciao, come stai?")
	}
	if finish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finish)
	}
	// Last chunk must be the finish_reason chunk (terminal).
	last := chunks[len(chunks)-1]
	if last.FinishReason != "stop" {
		t.Errorf("last chunk = %+v, want trailing FinishReason=stop", last)
	}
	if !res.hasUsage {
		t.Error("usage chunk not captured")
	}
}

// TestAccumulate (Req#2): toolcall_multichunk.sse — one tool call split across
// ≥3 chunks, single accumulated line >64KiB, parses to exactly one ToolCall with
// JSON-valid arguments. Reaching here without error proves bufio.Reader (a
// bufio.Scanner would have returned ErrTooLong on the >64KiB line).
func TestAccumulate(t *testing.T) {
	chunks, res := replayFixture(t, "toolcall_multichunk.sse")

	var calls []*llm.ToolCall
	var finish string
	for _, c := range chunks {
		if c.ToolCall != nil {
			calls = append(calls, c.ToolCall)
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want exactly 1", len(calls))
	}
	tc := calls[0]
	if tc.Function.Name != "get_weather" || tc.ID != "call_abc123" {
		t.Errorf("call metadata = %+v, want name=get_weather id=call_abc123", tc)
	}
	if len(tc.Function.Arguments) <= 64<<10 {
		t.Errorf("accumulated arguments = %d bytes, want >64KiB (proves bufio.Reader)", len(tc.Function.Arguments))
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("accumulated arguments not valid JSON: %v", err)
	}
	if args["city"] != "Roma" {
		t.Errorf("args[city] = %v, want Roma", args["city"])
	}
	if finish != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", finish)
	}
	if !res.hasUsage {
		t.Error("usage chunk not captured for tool-call stream")
	}
}

// TestUsage (Req#12 wire half): the usage chunk yields prompt/completion/cached
// tokens and the provider cost; cached_tokens is distinct from cache_write_tokens.
func TestUsage(t *testing.T) {
	_, res := replayFixture(t, "text_stop.sse")
	if !res.hasUsage {
		t.Fatal("usage not captured")
	}
	u := res.usage
	if u.PromptTokens != 42 || u.CompletionTokens != 7 {
		t.Errorf("tokens = (%d,%d), want (42,7)", u.PromptTokens, u.CompletionTokens)
	}
	if u.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30 (cached_tokens, NOT cache_write_tokens)", u.CachedTokens)
	}
	if u.Cost == nil || *u.Cost != 0.000123 {
		t.Errorf("Cost = %v, want 0.000123 surfaced as-is from usage.cost", u.Cost)
	}

	// A fixture whose usage records cache writes must NOT leak into CachedTokens.
	_, lres := replayFixture(t, "length_truncation.sse")
	if lres.usage.CachedTokens != 0 {
		t.Errorf("length fixture CachedTokens = %d, want 0 (its only cache tokens are writes)", lres.usage.CachedTokens)
	}
}

// TestStream_LengthTruncation: the parser surfaces finish_reason "length"
// (the user-facing truncation notice is Plan 04).
func TestStream_LengthTruncation(t *testing.T) {
	chunks, _ := replayFixture(t, "length_truncation.sse")
	var finish, text string
	for _, c := range chunks {
		if c.Text != "" {
			text += c.Text
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
	}
	if finish != "length" {
		t.Errorf("finish_reason = %q, want length", finish)
	}
	if text == "" {
		t.Error("expected partial text before the length cutoff")
	}
}

// TestParseSSE_SkipsCommentAndDone asserts a comment line and the [DONE] sentinel
// never reach json.Unmarshal (a malformed-chunk error would otherwise surface).
func TestParseSSE_SkipsCommentAndDone(t *testing.T) {
	raw := ": keep-alive\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"
	var chunks []llm.Chunk
	_, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		chunks = append(chunks, c)
		return true
	})
	if err != nil {
		t.Fatalf("parseSSE errored on a comment/[DONE] stream: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks emitted")
	}
}

// TestParseSSE_MalformedChunk asserts a non-[DONE] data: payload that is not
// valid JSON surfaces a clear error (defensive parse).
func TestParseSSE_MalformedChunk(t *testing.T) {
	raw := "data: {not json}\n"
	_, err := parseSSE(bytes.NewReader([]byte(raw)), func(llm.Chunk) bool { return true })
	if err == nil {
		t.Fatal("want error on malformed SSE chunk, got nil")
	}
}

// TestParseSSE_EOFWithoutDone asserts a stream that ends at EOF (no `data: [DONE]`
// sentinel) still finalizes cleanly — the trailing finish_reason chunk is emitted
// on the io.EOF branch, not only on the explicit [DONE].
func TestParseSSE_EOFWithoutDone(t *testing.T) {
	// No [DONE]; the reader just ends. The terminal finish_reason must still emit.
	raw := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n"
	var chunks []llm.Chunk
	res, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		chunks = append(chunks, c)
		return true
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if res.finishReason != "stop" {
		t.Errorf("finishReason = %q, want stop (captured even without [DONE])", res.finishReason)
	}
	last := chunks[len(chunks)-1]
	if last.FinishReason != "stop" {
		t.Errorf("last chunk = %+v, want a trailing FinishReason=stop emitted at EOF", last)
	}
}

// TestParseSSE_DataNoChoices asserts a `data:` chunk with an empty choices array
// (and a usage-only final message) parses without error and still captures usage —
// the handleChunk choices loop is a no-op but the usage capture runs.
func TestParseSSE_DataNoChoices(t *testing.T) {
	raw := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n" +
		"data: [DONE]\n"
	var chunks []llm.Chunk
	res, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		chunks = append(chunks, c)
		return true
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if !res.hasUsage || res.usage.PromptTokens != 3 || res.usage.CompletionTokens != 1 {
		t.Errorf("usage = %+v (hasUsage=%v), want prompt=3 completion=1", res.usage, res.hasUsage)
	}
	for _, c := range chunks {
		if c.Text != "" || c.ToolCall != nil {
			t.Errorf("no-choices chunk produced content/toolcall: %+v", c)
		}
	}
}

// TestParseSSE_NonDataLineIgnored asserts an SSE field line that is NOT a
// `data: ` line (e.g. an `event:` field) is silently ignored — it neither errors
// nor reaches json.Unmarshal.
func TestParseSSE_NonDataLineIgnored(t *testing.T) {
	raw := "event: message\n" +
		"id: 42\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n" +
		"data: [DONE]\n"
	var text string
	_, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		text += c.Text
		return true
	})
	if err != nil {
		t.Fatalf("parseSSE errored on non-data field lines: %v", err)
	}
	if text != "ok" {
		t.Errorf("assembled text = %q, want ok (event:/id: lines ignored)", text)
	}
}

// TestParseSSE_ConsumerStopsOnText asserts that when the consumer's emit returns
// false on a text delta, parseSSE returns immediately (the stale-emit guard) and
// does not error.
func TestParseSSE_ConsumerStopsOnText(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"second\"}}]}\n" +
		"data: [DONE]\n"
	var seen []string
	_, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		if c.Text != "" {
			seen = append(seen, c.Text)
			return false // stop after the first text delta
		}
		return true
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(seen) != 1 || seen[0] != "first" {
		t.Errorf("emitted text = %v, want exactly [first] then immediate return", seen)
	}
}

// TestParseSSE_ConsumerStopsOnFinalize asserts that when the consumer stops
// draining exactly as the terminal finish_reason chunk is emitted (the finalize
// path at [DONE]), parseSSE returns cleanly without error.
func TestParseSSE_ConsumerStopsOnFinalize(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n" +
		"data: [DONE]\n"
	_, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		// Refuse the trailing finish_reason chunk -> finalize() returns false.
		return c.FinishReason == ""
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
}

// TestParseSSE_ConsumerStopsOnToolCall asserts that when the consumer stops
// draining as the finalized tool-call chunk is emitted (inside finalize, before
// the finish_reason), parseSSE returns cleanly — the tool-call emit-false branch.
func TestParseSSE_ConsumerStopsOnToolCall(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"echo\",\"arguments\":\"{}\"}}]}}]}\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n" +
		"data: [DONE]\n"
	var sawToolCall bool
	_, err := parseSSE(bytes.NewReader([]byte(raw)), func(c llm.Chunk) bool {
		if c.ToolCall != nil {
			sawToolCall = true
			return false // stop draining at the finalized tool call
		}
		return true
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if !sawToolCall {
		t.Fatal("the finalized tool call was never emitted")
	}
}

// errReader returns data once then a non-EOF error, exercising the readErr != EOF
// branch of parseSSE (a real transport failure, not a clean stream end).
type errReader struct {
	data []byte
	done bool
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.done {
		return 0, errBoom
	}
	e.done = true
	n := copy(p, e.data)
	return n, nil
}

var errBoom = errReadBoom{}

type errReadBoom struct{}

func (errReadBoom) Error() string { return "boom: transport died" }

// TestParseSSE_ReadErrorSurfaces asserts a non-EOF reader error is returned to the
// caller (a real transport failure, distinct from a clean io.EOF end).
func TestParseSSE_ReadErrorSurfaces(t *testing.T) {
	r := &errReader{data: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n")}
	_, err := parseSSE(r, func(llm.Chunk) bool { return true })
	if err == nil {
		t.Fatal("want the non-EOF read error surfaced, got nil")
	}
}
