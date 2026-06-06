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
	// Each reasoning delta is rendered (the dim style resets between deltas so the
	// text is interspersed with ANSI escapes — assert the deltas, not a joined run).
	for _, want := range []string{"let me ", "think"} {
		if !strings.Contains(out, want) {
			t.Errorf("writer output missing reasoning delta %q: %q", want, out)
		}
	}
	if !strings.Contains(out, "\x1b[2m") {
		t.Errorf("writer output missing dim ANSI escape \\x1b[2m: %q", out)
	}

	// Stream-only invariant: reasoning text must NOT be part of the returned answer.
	if strings.Contains(answer, "let me") || strings.Contains(answer, "think") {
		t.Errorf("answer leaked reasoning text (must be stream-only): answer=%q", answer)
	}
	if answer != "Paris" {
		t.Errorf("answer = %q, want \"Paris\"", answer)
	}
}

// TestRenderReasoningPrefixOnce: the 💭 prefix is emitted exactly once across a
// multi-delta reasoning run; each subsequent delta continues the same dim line.
func TestRenderReasoningPrefixOnce(t *testing.T) {
	var buf bytes.Buffer
	started := false
	renderReasoning(&buf, "a", &started)
	renderReasoning(&buf, "b", &started)
	renderReasoning(&buf, "c", &started)
	if got := strings.Count(buf.String(), "💭"); got != 1 {
		t.Fatalf("💭 prefix count = %d, want 1 (%q)", got, buf.String())
	}
	// The dim style resets after each delta, so the deltas are interspersed with
	// ANSI escapes rather than joined — assert each delta is present.
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("reasoning delta %q not rendered: %q", want, buf.String())
		}
	}
}
