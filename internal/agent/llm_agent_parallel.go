package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

const (
	envMaxParallelTools     = "AURA_LOOP_MAX_PARALLEL_TOOLS"
	defaultMaxParallelTools = 4
)

// executeBatch runs a batch of tool calls' Execute() and returns one toolRunResult
// per call, index-aligned with calls (parity P1). A single call runs inline (zero
// goroutine overhead — the common case); two or more run CONCURRENTLY. This is the
// only concurrent step in dispatch: each goroutine writes its own results[k] slot
// and runTool is read-only on the agent, so there is no shared mutable state to
// guard here. The shared *Budget is safe (atomic step counter; the dedup ring is
// touched only by dispatch's serial Before/AfterToolResult, never inside runTool),
// and large outputs spill to per-tool_call_id sidecars, so concurrent writes never
// collide. The caller serializes the dedup gate, history appends, and yields.
func (a *LlmAgent) executeBatch(ctx context.Context, budget *Budget, calls []llm.ToolCall, startedAt time.Time) []toolRunResult {
	results := make([]toolRunResult, len(calls))
	if len(calls) <= 1 {
		if len(calls) == 1 {
			results[0] = a.runToolRecovering(ctx, budget, calls[0], startedAt)
		}
		return results
	}
	limit := min(maxParallelTools(), len(calls))
	// AG-064: a bounded WORKER POOL — spawn exactly `limit` workers that pull call
	// indices off a channel, instead of spawning one goroutine per call (each then
	// blocking on a semaphore). For a very wide tool batch this caps the live
	// goroutine count at `limit` rather than N. Each worker writes only its own
	// results[k] slot, so there is still no shared mutable state to guard.
	indices := make(chan int)
	var wg sync.WaitGroup
	wg.Add(limit)
	for range limit {
		go func() {
			defer wg.Done()
			for k := range indices {
				results[k] = a.runToolRecovering(ctx, budget, calls[k], startedAt)
			}
		}()
	}
	for k := range calls {
		indices <- k
	}
	close(indices)
	wg.Wait()
	return results
}

func (a *LlmAgent) runToolRecovering(ctx context.Context, budget *Budget, call llm.ToolCall, startedAt time.Time) (run toolRunResult) {
	// Resolve the Mutating bit BEFORE executing so a tool that panics after a
	// side effect is still classified as mutating in the recovery path (F-031):
	// otherwise the completion gate (a.sideEffected) would not arm and the turn
	// could finish without the post-mutation safeguard.
	mutating := false
	if tool, ok := a.registry.Get(call.Function.Name); ok {
		mutating = tool.Spec().Mutating
	}
	defer func() {
		if r := recover(); r != nil {
			recordRecoveredPanic("execute_batch")
			recordToolError(call.Function.Name)
			endedAt := time.Now().UTC()
			msg := fmt.Sprintf("panic: %v", r)
			preview := "error: " + msg
			run = toolRunResult{
				ToolCallID: call.ID,
				ToolName:   call.Function.Name,
				Arguments:  call.Function.Arguments,
				StartedAt:  startedAt,
				EndedAt:    endedAt,
				Mutating:   mutating,
				Preview:    preview,
				Result:     tools.ToolResult{Preview: preview, Bytes: len(preview)},
				Err:        msg,
			}
		}
	}()
	return a.runTool(ctx, budget, call, startedAt)
}

func maxParallelTools() int {
	v := strings.TrimSpace(os.Getenv(envMaxParallelTools))
	if v == "" {
		return defaultMaxParallelTools
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultMaxParallelTools
	}
	return n
}
