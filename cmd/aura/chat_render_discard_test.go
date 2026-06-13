package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
)

// TestRenderTurn_MidStreamRetryDiscardsPartial is the B-12 render guard: when a
// stream fails mid-way and the agent retries, it streams a partial chunk, then a
// discard-streamed signal Event, then the clean retry chunks + final. The renderer
// must honor the discard signal by dropping the already-shown partial so the final
// user-visible content is the clean answer ONLY — never partial+answer.
func TestRenderTurn_MidStreamRetryDiscardsPartial(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		// Attempt 1 streams a partial chunk before the mid-stream error.
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "PARTIAL-garbled"}}, nil) {
			return
		}
		// The agent repudiates the partial before retrying.
		if !yield(&agent.Event{Actions: agent.Actions{DiscardStreamed: true}}, nil) {
			return
		}
		// Retry streams the clean answer incrementally then the final Event.
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "clean "}}, nil) {
			return
		}
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "answer"}}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "clean answer", FinishReason: "stop"},
		}, nil)
	}
	var buf bytes.Buffer
	answer, finish, _, _, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("renderRunnerTurn: %v", err)
	}
	if finish != "stop" {
		t.Errorf("finish = %q, want stop", finish)
	}
	if answer != "clean answer" {
		t.Errorf("answer = %q, want %q (partial must be discarded)", answer, "clean answer")
	}
	if strings.Contains(answer, "PARTIAL") {
		t.Errorf("returned answer still carries the discarded partial: %q", answer)
	}
	// The discarded partial must not survive in the returned prose. (The terminal
	// bytes carry ANSI erase sequences after the discard, so we assert on the
	// renderer's accumulated answer, which is what persistence/auto-title consume.)
}

// TestRenderTurn_NoDiscardOnNormalPath proves the discard handling is inert on the
// common no-retry path: a normal multi-chunk stream renders incrementally and the
// answer is the streamed content, unaffected by the new discard branch.
func TestRenderTurn_NoDiscardOnNormalPath(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "Ciao "}}, nil) {
			return
		}
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "mondo"}}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "Ciao mondo", FinishReason: "stop"},
		}, nil)
	}
	var buf bytes.Buffer
	answer, _, _, _, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("renderRunnerTurn: %v", err)
	}
	if answer != "Ciao mondo" {
		t.Errorf("answer = %q, want Ciao mondo", answer)
	}
	if n := strings.Count(buf.String(), "Ciao"); n != 1 {
		t.Errorf("'Ciao' rendered %d times, want 1 (incremental stream, no double-print)", n)
	}
}
