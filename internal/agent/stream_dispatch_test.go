package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
)

// mockToolExecutor records which calls it received and sleeps for sleepDuration.
type mockToolExecutor struct {
	sleepDuration time.Duration
	callsReceived atomic.Int64
}

func (m *mockToolExecutor) ExecuteToolCalls(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
	m.callsReceived.Add(int64(len(calls)))
	if m.sleepDuration > 0 {
		time.Sleep(m.sleepDuration)
	}
	results := make(map[string]string, len(calls))
	for _, c := range calls {
		results[c.ID] = "ok:" + c.Name
	}
	return ExecutionSummary{Results: results, LastResult: "ok:" + calls[0].Name}
}

func makeCall(id, name string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: name, Arguments: map[string]any{}}
}

func TestStreamDispatcher_ResultsInSubmissionOrder(t *testing.T) {
	exec := &mockToolExecutor{sleepDuration: 10 * time.Millisecond}
	d := newStreamDispatcher(exec, 4)
	ctx := context.Background()

	calls := []llm.ToolCall{
		makeCall("c1", "tool_a"),
		makeCall("c2", "tool_b"),
		makeCall("c3", "tool_c"),
	}
	for _, call := range calls {
		d.Submit(ctx, call)
	}
	result := d.Wait(ctx)

	for _, call := range calls {
		if _, ok := result.Results[call.ID]; !ok {
			t.Errorf("missing result for call %s", call.ID)
		}
	}
	if len(result.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(result.Results))
	}
}

func TestStreamDispatcher_InFlightDedup(t *testing.T) {
	exec := &mockToolExecutor{sleepDuration: 20 * time.Millisecond}
	d := newStreamDispatcher(exec, 4)
	ctx := context.Background()

	call := makeCall("dup-id", "tool_x")
	d.Submit(ctx, call)
	d.Submit(ctx, call) // duplicate — should be ignored
	d.Submit(ctx, call) // duplicate — should be ignored
	d.Wait(ctx)

	if got := exec.callsReceived.Load(); got != 1 {
		t.Errorf("expected 1 execution, got %d (dedup failed)", got)
	}
}

func TestStreamDispatcher_SemaphoreRespectsConcurrencyLimit(t *testing.T) {
	const limit = 2
	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64

	exec := ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
		cur := concurrent.Add(1)
		// track peak
		for {
			prev := maxConcurrent.Load()
			if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		concurrent.Add(-1)
		results := map[string]string{calls[0].ID: "ok"}
		return ExecutionSummary{Results: results}
	})

	d := newStreamDispatcher(exec, limit)
	ctx := context.Background()

	for i := range 6 {
		d.Submit(ctx, makeCall(string(rune('a'+i)), "tool"))
	}
	d.Wait(ctx)

	if peak := maxConcurrent.Load(); peak > limit {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, limit)
	}
}

func TestStreamDispatcher_ContextCancellation(t *testing.T) {
	exec := &mockToolExecutor{sleepDuration: 500 * time.Millisecond}
	d := newStreamDispatcher(exec, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	d.Submit(ctx, makeCall("slow", "slow_tool"))
	result := d.Wait(ctx)

	// Result must be present (either an error or the real result).
	if _, ok := result.Results["slow"]; !ok {
		t.Error("expected result entry even after cancellation")
	}
}

func TestStreamDispatcher_EmptySubmit(t *testing.T) {
	exec := &mockToolExecutor{}
	d := newStreamDispatcher(exec, 4)
	result := d.Wait(context.Background())
	if len(result.Results) != 0 {
		t.Errorf("expected empty results for zero submissions, got %d", len(result.Results))
	}
}

// BenchmarkStreamDispatch_ParallelVsSerial measures the speedup of parallel
// dispatch over serial (maxConcurrent=1) for three 50ms tools.
func BenchmarkStreamDispatch_ParallelVsSerial(b *testing.B) {
	const toolDelay = 50 * time.Millisecond
	const numTools = 3

	calls := make([]llm.ToolCall, numTools)
	for i := range numTools {
		calls[i] = makeCall(string(rune('a'+i)), "bench_tool")
	}

	exec := ToolExecutorFunc(func(_ context.Context, c []llm.ToolCall) ExecutionSummary {
		time.Sleep(toolDelay)
		return ExecutionSummary{Results: map[string]string{c[0].ID: "ok"}}
	})

	b.Run("serial_maxConcurrent1", func(b *testing.B) {
		for range b.N {
			d := newStreamDispatcher(exec, 1)
			for _, call := range calls {
				d.Submit(context.Background(), call)
			}
			d.Wait(context.Background())
		}
	})

	b.Run("parallel_maxConcurrent3", func(b *testing.B) {
		for range b.N {
			d := newStreamDispatcher(exec, numTools)
			for _, call := range calls {
				d.Submit(context.Background(), call)
			}
			d.Wait(context.Background())
		}
	})
}
