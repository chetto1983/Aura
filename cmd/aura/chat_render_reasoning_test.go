package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
)

// TestRenderRunnerTurnReasoning: a turn that streams reasoning deltas followed by a
// content answer renders the live dim 💭 CoT to the writer (amendment #57e) but
// keeps reasoning OUT of the returned answer (stream-only invariant, mirror of
// Plan 12-05's persistence guard).
func TestRenderRunnerTurnReasoning(t *testing.T) {
	var buf bytes.Buffer
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Reasoning: "let me "}}, nil) {
			return
		}
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Reasoning: "think"}}, nil) {
			return
		}
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "Paris"}}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "Paris", FinishReason: "stop"},
		}, nil)
	}

	answer, finish, _, paused, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paused {
		t.Error("paused = true, want false")
	}
	if finish != "stop" {
		t.Errorf("finish = %q, want \"stop\"", finish)
	}

	out := buf.String()
	if !strings.Contains(out, "💭") {
		t.Errorf("writer output missing 💭 reasoning prefix: %q", out)
	}
	// The bounded renderer redraws the rolling window per delta and collapses
	// whitespace, so the accumulated CoT surfaces as one window — assert the joined
	// (collapsed) reasoning, not the raw per-delta fragments.
	if !strings.Contains(out, "let me think") {
		t.Errorf("writer output missing rolling reasoning window %q: %q", "let me think", out)
	}
	if !strings.Contains(out, "\x1b[2m") {
		t.Errorf("writer output missing dim ANSI escape \\x1b[2m: %q", out)
	}
	// The reasoning line is cleared (carriage-return + clear-to-eol) when the answer
	// arrives, so the bounded region never lingers under the prose.
	if !strings.Contains(out, "\r\x1b[K") {
		t.Errorf("writer output missing the reasoning clear sequence \\r\\x1b[K: %q", out)
	}

	// Stream-only invariant: reasoning text must NOT be part of the returned answer.
	if strings.Contains(answer, "let me") || strings.Contains(answer, "think") {
		t.Errorf("answer leaked reasoning text (must be stream-only): answer=%q", answer)
	}
	if answer != "Paris" {
		t.Errorf("answer = %q, want \"Paris\"", answer)
	}
}

// TestCLIReasoningRollingWindowBounded: the bounded renderer keeps only the most-recent
// cliReasoningWidth runes (tail-keep, front-evict) so a long CoT never grows without
// bound, and clear() erases the line + resets the window. This replaces the old
// "prefix once" invariant — the in-place renderer redraws (and re-prefixes) per delta
// by design, trading the single-prefix property for a bounded footprint.
func TestCLIReasoningRollingWindowBounded(t *testing.T) {
	var buf bytes.Buffer
	r := newCLIReasoning()
	r.push(&buf, "OLDEST ")
	r.push(&buf, strings.Repeat("x", cliReasoningWidth))
	r.push(&buf, " NEWEST")

	if r.fifo.Len() > cliReasoningWidth {
		t.Fatalf("window grew unbounded: %d runes > cap %d", r.fifo.Len(), cliReasoningWidth)
	}
	if strings.Contains(r.fifo.String(), "OLDEST") {
		t.Errorf("front-evicted reasoning must not survive in the window: %q", r.fifo.String())
	}
	if !strings.Contains(r.fifo.String(), "NEWEST") {
		t.Errorf("newest reasoning must stay in the window (tail-keep): %q", r.fifo.String())
	}

	r.clear(&buf)
	if r.active {
		t.Error("clear() must mark the line inactive")
	}
	if r.fifo.Len() != 0 {
		t.Errorf("clear() must reset the window, got %d runes", r.fifo.Len())
	}
	if !strings.Contains(buf.String(), "\r\x1b[K") {
		t.Errorf("clear() must emit the clear sequence \\r\\x1b[K: %q", buf.String())
	}
}

// TestCLIReasoningInactiveClearIsNoOp: clear() before any push writes nothing (so the
// default redacted REPL, where reasoning never streams, is byte-for-byte unchanged).
func TestCLIReasoningInactiveClearIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	newCLIReasoning().clear(&buf)
	if buf.Len() != 0 {
		t.Fatalf("clear() on an inactive renderer wrote %q, want nothing", buf.String())
	}
}
