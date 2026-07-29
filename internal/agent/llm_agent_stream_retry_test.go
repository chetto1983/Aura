package agent_test

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// TestLlmAgent_NoRetryPathStreamsIncrementally guards against a streaming
// regression (B-12): the COMMON no-retry path must still yield each text delta as
// its OWN chunk Event as it arrives — never buffered into one blob at the end — and
// must NOT emit the discard-streamed signal (that fires only on a mid-stream retry).
func TestLlmAgent_NoRetryPathStreamsIncrementally(t *testing.T) {
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", "Ciao ", "mondo", "!"))
	a := newAgent(t, fc, llm.Config{})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(2)})))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	var chunkCount int
	for _, ev := range evs {
		if ev.Actions.DiscardStreamed {
			t.Fatalf("no-retry path must NOT emit a discard-streamed signal: %+v", ev.Actions)
		}
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason == "" && ev.LLMResponse.Content != "" {
			chunkCount++
		}
	}
	if chunkCount < 3 {
		t.Fatalf("expected >=3 incremental chunk events (live streaming), got %d — chunks were buffered", chunkCount)
	}
}

// TestLlmAgent_MidStreamRetryEmitsDiscardSignal is the B-12 RED test: when a stream
// fails mid-way and the loop retries, the partial chunk Events the failed attempt
// already streamed must be repudiated by a discard-streamed signal Event BEFORE the
// retry streams its clean chunks — otherwise the renderer shows partial+answer. The
// discard Event must sit AFTER the partial chunk and BEFORE the clean retry chunk.
func TestLlmAgent_MidStreamRetryEmitsDiscardSignal(t *testing.T) {
	streamErr := errors.New("connection reset by peer")
	fc := agenttest.NewFakeClient(
		agenttest.TextThenErr(streamErr, "partial"),
		agenttest.TextChunks("stop", "clean answer"),
	)
	a := newAgent(t, fc, llm.Config{})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(3)})))
	if err != nil {
		t.Fatalf("Run error = %v, want retry success", err)
	}

	discardIdx := -1
	partialIdx := -1
	cleanIdx := -1
	for i, ev := range evs {
		if ev.Actions.DiscardStreamed && discardIdx < 0 {
			discardIdx = i
		}
		if ev.LLMResponse == nil {
			continue
		}
		if ev.LLMResponse.Content == "partial" && partialIdx < 0 {
			partialIdx = i
		}
		if ev.LLMResponse.Content == "clean answer" && ev.LLMResponse.FinishReason == "" && cleanIdx < 0 {
			cleanIdx = i
		}
	}
	if discardIdx < 0 {
		t.Fatal("mid-stream retry must emit a discard-streamed signal so the renderer drops the partial")
	}
	if partialIdx < 0 || cleanIdx < 0 {
		t.Fatalf("expected both the partial (idx=%d) and clean (idx=%d) chunk events", partialIdx, cleanIdx)
	}
	if partialIdx >= discardIdx || discardIdx >= cleanIdx {
		t.Fatalf("discard signal must sit between the partial and clean chunks: partial=%d discard=%d clean=%d", partialIdx, discardIdx, cleanIdx)
	}
}
