package webadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/llm"
)

func TestStreamRouterEmitsUIMessageStreamFrames(t *testing.T) {
	router := NewStreamRouter()
	var buf bytes.Buffer
	flushes := 0
	end, err := router.Begin("web:alice:live", &buf, func() { flushes++ })
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer end()

	if err := router.Deliver(context.Background(), chat.OutboundEvent{
		RunID:    "run-1",
		ThreadID: "web:alice:live",
		Type:     chat.EventRunStarted,
	}); err != nil {
		t.Fatalf("RunStarted: %v", err)
	}
	tokens := make(chan llm.Token, 3)
	tokens <- llm.Token{Content: "Hel"}
	tokens <- llm.Token{Content: "lo"}
	tokens <- llm.Token{Done: true, Usage: llm.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}}
	close(tokens)

	resp, delivered, err := router.ConsumeTokens(context.Background(), "web:alice:live", tokens)
	if err != nil {
		t.Fatalf("ConsumeTokens: %v", err)
	}
	if !delivered || resp.Content != "Hello" || resp.Usage.TotalTokens != 5 {
		t.Fatalf("response delivered=%v resp=%+v", delivered, resp)
	}
	if err := router.Deliver(context.Background(), chat.OutboundEvent{
		RunID:    "run-1",
		ThreadID: "web:alice:live",
		Type:     chat.EventMessageDelta,
		Content:  "Hello",
	}); err != nil {
		t.Fatalf("duplicate MessageDelta: %v", err)
	}
	if err := router.Deliver(context.Background(), chat.OutboundEvent{
		RunID:    "run-1",
		ThreadID: "web:alice:live",
		Type:     chat.EventMessageDone,
		Content:  "Hello",
	}); err != nil {
		t.Fatalf("MessageDone: %v", err)
	}
	if err := router.Deliver(context.Background(), chat.OutboundEvent{
		RunID:    "run-1",
		ThreadID: "web:alice:live",
		Type:     chat.EventUsage,
		Payload: map[string]any{
			"tokens_prompt":     3,
			"tokens_completion": 2,
			"stop_reason":       "stop",
		},
	}); err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if err := router.Deliver(context.Background(), chat.OutboundEvent{
		RunID:    "run-1",
		ThreadID: "web:alice:live",
		Type:     chat.EventDone,
		Payload:  map[string]any{"status": string(chat.RunStatusCompleted)},
	}); err != nil {
		t.Fatalf("Done: %v", err)
	}

	frames := parseUIMessageFrames(t, buf.String())
	if flushes < 5 {
		t.Fatalf("flushes = %d, want >=5", flushes)
	}
	assertFrameType(t, frames, 0, "start")
	assertFrameType(t, frames, 1, "text-start")
	assertFrameTextDelta(t, frames, 2, "Hel")
	assertFrameTextDelta(t, frames, 3, "lo")
	assertFrameType(t, frames, 4, "text-end")
	assertFrameType(t, frames, 5, "finish")
	if got := countFrameType(frames, "text-delta"); got != 2 {
		t.Fatalf("text-delta frames = %d, want 2; frames=%v", got, frames)
	}
	if !strings.HasSuffix(buf.String(), "data: [DONE]\n\n") {
		t.Fatalf("stream missing [DONE] terminator:\n%s", buf.String())
	}
}

func TestStreamRouterEmitsToolFrames(t *testing.T) {
	router := NewStreamRouter()
	var buf bytes.Buffer
	end, err := router.Begin("web:alice:tools", &buf, nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer end()

	events := []chat.OutboundEvent{
		{RunID: "run-tools", ThreadID: "web:alice:tools", Type: chat.EventRunStarted},
		{
			RunID:    "run-tools",
			ThreadID: "web:alice:tools",
			Type:     chat.EventToolStart,
			Payload:  map[string]any{"tool": "search", "tool_call_id": "call-1"},
		},
		{
			RunID:    "run-tools",
			ThreadID: "web:alice:tools",
			Type:     chat.EventToolEnd,
			Payload:  map[string]any{"tool": "search", "tool_call_id": "call-1", "success": true, "elapsed_ms": 12, "preview": "ok"},
		},
		{RunID: "run-tools", ThreadID: "web:alice:tools", Type: chat.EventDone, Payload: map[string]any{"status": string(chat.RunStatusCompleted)}},
	}
	for _, ev := range events {
		if err := router.Deliver(context.Background(), ev); err != nil {
			t.Fatalf("Deliver %s: %v", ev.Type, err)
		}
	}

	frames := parseUIMessageFrames(t, buf.String())
	assertFrameType(t, frames, 1, "tool-call-start")
	assertFrameType(t, frames, 2, "tool-call-end")
	assertFrameType(t, frames, 3, "tool-result")
	if frames[3]["toolCallId"] != "call-1" {
		t.Fatalf("toolCallId = %v", frames[3]["toolCallId"])
	}
}

func TestStreamRouterEmitsPendingQuestionFrame(t *testing.T) {
	router := NewStreamRouter()
	var buf bytes.Buffer
	end, err := router.Begin("web:alice:question", &buf, nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer end()

	events := []chat.OutboundEvent{
		{RunID: "run-question", ThreadID: "web:alice:question", Type: chat.EventRunStarted},
		{
			ID:       "question-1",
			RunID:    "run-question",
			ThreadID: "web:alice:question",
			Type:     chat.EventQuestionRequested,
			Payload:  map[string]any{"question": "Continue?", "options": []string{"yes", "no"}, "kind": "approval"},
		},
		{RunID: "run-question", ThreadID: "web:alice:question", Type: chat.EventDone, Payload: map[string]any{"status": string(chat.RunStatusWaitingForUser)}},
	}
	for _, ev := range events {
		if err := router.Deliver(context.Background(), ev); err != nil {
			t.Fatalf("Deliver %s: %v", ev.Type, err)
		}
	}

	frames := parseUIMessageFrames(t, buf.String())
	assertFrameType(t, frames, 1, "data-pending-question")
	data, ok := frames[1]["data"].(map[string]any)
	if !ok {
		t.Fatalf("data frame payload = %#v", frames[1]["data"])
	}
	if data["id"] != "question-1" || data["question"] != "Continue?" || data["kind"] != "approval" {
		t.Fatalf("pending question data = %#v", data)
	}
}

func TestStreamRouterRejectsActiveThread(t *testing.T) {
	router := NewStreamRouter()
	var buf bytes.Buffer
	end, err := router.Begin("web:alice:live", &buf, nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer end()
	if _, err := router.Begin("web:alice:live", &buf, nil); !errors.Is(err, ErrStreamAlreadyActive) {
		t.Fatalf("second Begin err = %v, want ErrStreamAlreadyActive", err)
	}
}

func parseUIMessageFrames(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, event := range strings.Split(raw, "\n\n") {
		event = strings.TrimSpace(event)
		if event == "" || event == "data: [DONE]" {
			continue
		}
		payload := strings.TrimPrefix(event, "data: ")
		var frame map[string]any
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("decode frame %q: %v\nstream:\n%s", payload, err, raw)
		}
		frames = append(frames, frame)
	}
	return frames
}

func assertFrameType(t *testing.T, frames []map[string]any, idx int, want string) {
	t.Helper()
	if idx >= len(frames) {
		t.Fatalf("missing frame %d in %v", idx, frames)
	}
	if got, _ := frames[idx]["type"].(string); got != want {
		t.Fatalf("frame[%d].type = %q, want %q; frame=%v", idx, got, want, frames[idx])
	}
}

func assertFrameTextDelta(t *testing.T, frames []map[string]any, idx int, want string) {
	t.Helper()
	assertFrameType(t, frames, idx, "text-delta")
	if got, _ := frames[idx]["textDelta"].(string); got != want {
		t.Fatalf("frame[%d].textDelta = %q, want %q", idx, got, want)
	}
	if got, _ := frames[idx]["delta"].(string); got != want {
		t.Fatalf("frame[%d].delta = %q, want %q", idx, got, want)
	}
}

func countFrameType(frames []map[string]any, typ string) int {
	var n int
	for _, frame := range frames {
		if frame["type"] == typ {
			n++
		}
	}
	return n
}
