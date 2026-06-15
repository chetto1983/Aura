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
	limit := maxParallelTools()
	if limit > len(calls) {
		limit = len(calls)
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for k := range calls {
		go func(k int) {
			sem <- struct{}{}
			defer func() { <-sem }()
			defer wg.Done()
			results[k] = a.runToolRecovering(ctx, budget, calls[k], startedAt)
		}(k)
	}
	wg.Wait()
	return results
}

func (a *LlmAgent) runToolRecovering(ctx context.Context, budget *Budget, call llm.ToolCall, startedAt time.Time) (run toolRunResult) {
	defer func() {
		if r := recover(); r != nil {
			recordRecoveredPanic("execute_batch")
			endedAt := time.Now().UTC()
			msg := fmt.Sprintf("panic: %v", r)
			preview := "error: " + msg
			run = toolRunResult{
				ToolCallID: call.ID,
				ToolName:   call.Function.Name,
				Arguments:  call.Function.Arguments,
				StartedAt:  startedAt,
				EndedAt:    endedAt,
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
