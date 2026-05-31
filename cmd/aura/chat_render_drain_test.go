package main

import (
	"io"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

// TestRenderRunnerTurn_DrainsPastFinalEvent guards the auto-title regression found
// in Phase-4 live UAT: renderRunnerTurn used to `return` on the first FinishReason
// Event. Under the iter.Seq2 contract that early return makes yield() report
// consumer-stop, so the Runner's post-round bookkeeping after its event loop
// (flushPause + the auto-title worker) never runs — auto-title silently never fired
// in a live `aura chat` session. The consumer MUST drain the iterator to completion.
//
// This test fails pre-fix (reachedAfterFinal stays false because yield(final)
// returns false and the producer returns) and passes post-fix.
func TestRenderRunnerTurn_DrainsPastFinalEvent(t *testing.T) {
	reachedAfterFinal := false
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "Par"}}, nil) {
			return
		}
		// Final Event: FinishReason set + the FULL message content (the real wire
		// shape — the final Event carries the complete answer; flushRemainder emits
		// only the not-yet-streamed tail) + usage in StateDelta.
		if !yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "Parigi", FinishReason: "stop"},
			Actions:     agent.Actions{StateDelta: map[string]any{"prompt_tokens": 100, "completion_tokens": 5}},
		}, nil) {
			return // pre-fix path: consumer returned early, post-round work is skipped
		}
		// This line stands in for the producer's post-loop bookkeeping (flushPause +
		// maybeAutoTitle). It is only reached if the consumer kept draining.
		reachedAfterFinal = true
		yield(&agent.Event{LLMResponse: &agent.LLMResponse{}}, nil)
	}

	answer, finish, usage, paused, err := renderRunnerTurn(io.Discard, seq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reachedAfterFinal {
		t.Fatal("renderRunnerTurn returned early on the final Event — the producer's post-round bookkeeping (auto-title worker) would be skipped")
	}
	if finish != "stop" {
		t.Errorf("finish = %q, want \"stop\"", finish)
	}
	if answer != "Parigi" {
		t.Errorf("answer = %q, want \"Parigi\"", answer)
	}
	if paused {
		t.Error("paused = true, want false")
	}
	if want := (llm.Usage{PromptTokens: 100, CompletionTokens: 5}); usage != want {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
}
