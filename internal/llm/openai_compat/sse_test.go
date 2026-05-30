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
