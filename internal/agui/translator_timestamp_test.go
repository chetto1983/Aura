package agui

import (
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
)

func TestRetainedToolFramesPreserveExecutionDuration(t *testing.T) {
	start := time.Date(2026, 9, 8, 6, 0, 0, 0, time.UTC)
	end := start.Add(20 * time.Second)
	frames := collect(t, "thread", "run", &fixedIDGen{}, []*agent.Event{
		{Timestamp: start, Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationStart, ToolCallID: "call", ToolName: "shell_exec", Arguments: `{"command":"sleep 75"}`,
		}}},
		{Timestamp: end, Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationEnd, ToolCallID: "call", ToolName: "shell_exec", Status: "canceled", ResultPreview: "[command cancelled]",
		}}},
	})
	want := map[events.EventType]int64{
		events.EventTypeToolCallStart:  start.UnixMilli(),
		events.EventTypeToolCallEnd:    end.UnixMilli(),
		events.EventTypeToolCallResult: end.UnixMilli(),
	}
	for _, frame := range frames {
		if timestamp, ok := want[frame.Type()]; ok {
			if got := frame.Timestamp(); got == nil || *got != timestamp {
				t.Fatalf("%s replay timestamp = %v, want original %d", frame.Type(), got, timestamp)
			}
			delete(want, frame.Type())
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing tool frames: %v", want)
	}
}

func TestRetainedReasoningFramesPreserveOriginalClock(t *testing.T) {
	start := time.Date(2026, 9, 8, 6, 0, 0, 0, time.UTC)
	end := start.Add(9 * time.Second)
	reasoning := &agent.Event{Timestamp: start, LLMResponse: &agent.LLMResponse{Reasoning: "checking"}}
	finish := &agent.Event{Timestamp: end, LLMResponse: &agent.LLMResponse{Content: "answer", FinishReason: "stop"}}
	frames := collect(t, "thread", "run", &fixedIDGen{}, []*agent.Event{reasoning, finish})
	var opened, closed bool
	for _, frame := range frames {
		switch frame.Type() {
		case events.EventTypeReasoningStart:
			opened = true
			if got := frame.Timestamp(); got == nil || *got != start.UnixMilli() {
				t.Fatal("reasoning start lost original time")
			}
		case events.EventTypeReasoningEnd:
			closed = true
			if got := frame.Timestamp(); got == nil || *got != end.UnixMilli() {
				t.Fatal("reasoning end lost original time")
			}
		}
	}
	if !opened || !closed {
		t.Fatal("missing reasoning lifecycle")
	}
}
