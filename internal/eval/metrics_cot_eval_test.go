//go:build cot_eval

package eval

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

func TestCaptureTurnTTFTTracksToolCallBeforeContent(t *testing.T) {
	call := llm.ToolCall{ID: "call_1", Type: "function"}
	call.Function.Name = "current_time"
	call.Function.Arguments = `{}`
	c := captureTurn(func() func(func(*agent.Event, error) bool) {
		return func(yield func(*agent.Event, error) bool) {
			time.Sleep(2 * time.Millisecond)
			if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{ToolCalls: []llm.ToolCall{call}}}, nil) {
				return
			}
			time.Sleep(2 * time.Millisecond)
			yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "Fatto.", FinishReason: "stop"}}, nil)
		}
	})

	if c.firstEventMS <= 0 {
		t.Fatalf("firstEventMS = %.3f, want a tool-call TTFT measurement", c.firstEventMS)
	}
	if c.firstByteMS != 0 {
		t.Fatalf("firstByteMS = %.3f, want 0 when no streamed prose chunk preceded final", c.firstByteMS)
	}
	if len(c.toolCallMS) != 1 || c.toolCallMS[0] <= 0 {
		t.Fatalf("toolCallMS = %#v, want one measured current_time call", c.toolCallMS)
	}
}

func TestMetricHelpersComputeToolLoopAndTPS(t *testing.T) {
	c := &turnCapture{
		firstByteMS:  400,
		totalMS:      900,
		toolCallMS:   []float64{120, 150},
		toolResultMS: []float64{220, 310},
	}

	if got := toolLoopMS(c); got != 190 {
		t.Fatalf("toolLoopMS = %.0f, want 190", got)
	}
	if got := completionTPS(25, c.firstByteMS, c.totalMS); got != 50 {
		t.Fatalf("completionTPS = %.0f, want 50", got)
	}
}
