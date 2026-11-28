package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// failedRequestLine sends replayRequest at effort high to a server answering with handler,
// and returns the one log line the failed request left.
func failedRequestLine(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		handler(w, r)
	})
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	client.logger = func() *slog.Logger { return logger }
	req := replayRequest()
	req.Reasoning.Effort = llm.ReasoningEffortHigh
	if stream, err := client.Stream(context.Background(), req); err == nil {
		lastErr(t, drainStream(stream))
	}
	line := buf.String()
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("want one log line, got %q", line)
	}
	for _, leak := range []string{houseKey, "secret detail", "partial"} {
		if strings.Contains(line, leak) {
			t.Fatalf("log line leaked %q: %s", leak, line)
		}
	}
	return line
}

func drainStream(stream <-chan llm.Chunk) []llm.Chunk {
	var chunks []llm.Chunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func wantFields(t *testing.T, line string, fields ...string) {
	t.Helper()
	for _, want := range append([]string{`level=WARN`, `msg="anthropic request failed"`, "model=claude-sonnet-5-5", "effort=high", "replayed_state_messages=1"}, fields...) {
		if !strings.Contains(line, " "+want+" ") && !strings.HasSuffix(strings.TrimSpace(line), " "+want) {
			t.Fatalf("log line %q lacks %s", line, want)
		}
	}
}

func TestRefusedOpenLeavesALine(t *testing.T) {
	line := failedRequestLine(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "req_open_1")
		w.WriteHeader(529)
		fmt.Fprintf(w, `{"type":"error","error":{"type":"overloaded_error","message":"secret detail %s"}}`, houseKey)
	})
	wantFields(t, line, "dropped_blocks=0", "status=529", "error_class=unavailable", "request_id=req_open_1")
}

func TestMidStreamErrorLeavesALine(t *testing.T) {
	line := failedRequestLine(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("request-id", "req_mid_1")
		data := fmt.Sprintf(`{"type":"error","error":{"type":"rate_limit_error","message":"secret detail %s"}}`, houseKey)
		events := []sseEvent{messageStart(10, 0, 0, `[{"type":"thinking_dropped","path":"messages.1.content.0"}]`)}
		writeEvents(w, append(append(events, textBlock(0, "partial")...), sseEvent{"error", data})...)
	})
	wantFields(t, line, "dropped_blocks=1", "status=429", "error_class=unavailable", "request_id=req_mid_1")
}

func TestStreamCutBeforeStopLeavesALine(t *testing.T) {
	line := failedRequestLine(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, reply(textBlock(0, "partial"))...)
	})
	wantFields(t, line, "dropped_blocks=0", "status=0", "error_class=internal", `request_id=""`)
}

func TestUnassemblableEventLeavesALine(t *testing.T) {
	line := failedRequestLine(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, messageStart(10, 0, 0, "[]"), blockStart(3, `{"type":"text","text":""}`))
	})
	wantFields(t, line, "dropped_blocks=0", "status=0", "error_class=other", `request_id=""`)
}

func TestUnsupportedStopReasonLeavesALine(t *testing.T) {
	line := failedRequestLine(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w, append(reply(textBlock(0, "partial")), messageDelta("pause_turn", 5), messageStop())...)
	})
	wantFields(t, line, "dropped_blocks=0", "status=0", "error_class=other", `request_id=""`)
}
