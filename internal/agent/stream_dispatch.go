// stream_dispatch.go provides FuturesOrdered parallel tool dispatch.
// Tools are submitted as they arrive from the LLM stream and executed
// concurrently (up to maxConcurrent at once). Wait() returns results
// merged in submission order so the conversation state stays coherent.
package agent

import (
	"context"
	"maps"
	"sync"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/stringx"
)

// StreamDispatcher executes tool calls in parallel while preserving
// FuturesOrdered semantics: results are merged in LLM-emission (submission)
// order, not completion order. maxConcurrent caps simultaneous executions via
// a semaphore. Calls with the same ID (in-flight dedup) are silently ignored.
type StreamDispatcher struct {
	executor      ToolExecutor
	maxConcurrent int
	sem           chan struct{}
	mu            sync.Mutex
	entries       []*dispatchedEntry
	inflightIDs   map[string]bool
	wg            sync.WaitGroup
}

type dispatchedEntry struct {
	call   llm.ToolCall
	result ExecutionSummary
	done   chan struct{}
}

func newStreamDispatcher(executor ToolExecutor, maxConcurrent int) *StreamDispatcher {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &StreamDispatcher{
		executor:      executor,
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
		inflightIDs:   make(map[string]bool),
	}
}

// Submit enqueues a tool call for parallel execution. If a call with the same
// ID is already in flight it is silently ignored (in-flight dedup).
func (d *StreamDispatcher) Submit(ctx context.Context, call llm.ToolCall) {
	d.mu.Lock()
	if d.inflightIDs[call.ID] {
		d.mu.Unlock()
		return
	}
	d.inflightIDs[call.ID] = true
	entry := &dispatchedEntry{call: call, done: make(chan struct{})}
	d.entries = append(d.entries, entry)
	d.mu.Unlock()

	d.wg.Go(func() {
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			entry.result = ExecutionSummary{
				Results: map[string]string{call.ID: "Error: " + ctx.Err().Error()},
			}
			close(entry.done)
			return
		}
		defer func() { <-d.sem }()
		entry.result = d.executor.ExecuteToolCalls(ctx, []llm.ToolCall{call})
		close(entry.done)
	})
}

// Wait blocks until all submitted calls finish (or ctx is cancelled) and
// returns a merged ExecutionSummary in submission order. Callers MUST NOT
// Submit after calling Wait.
func (d *StreamDispatcher) Wait(ctx context.Context) ExecutionSummary {
	waitDone := make(chan struct{})
	go func() { d.wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-ctx.Done():
	}

	d.mu.Lock()
	entries := append([]*dispatchedEntry(nil), d.entries...)
	d.mu.Unlock()

	merged := ExecutionSummary{Results: make(map[string]string, len(entries))}
	for _, e := range entries {
		var s ExecutionSummary
		select {
		case <-e.done:
			s = e.result
		default:
			// ctx cancelled before this entry finished
			errMsg := "Error: context cancelled"
			if ctx.Err() != nil {
				errMsg = "Error: " + ctx.Err().Error()
			}
			s = ExecutionSummary{Results: map[string]string{e.call.ID: errMsg}}
		}
		maps.Copy(merged.Results, s.Results)
		if s.LastResult != "" {
			merged.LastResult = s.LastResult
		}
		if s.FatalResult != "" && merged.FatalResult == "" {
			merged.FatalResult = s.FatalResult
		}
		if s.TerminalTool != "" && merged.TerminalTool == "" {
			merged.TerminalTool = s.TerminalTool
		}
		merged.ReadSkillNames = stringx.AppendUnique(merged.ReadSkillNames, s.ReadSkillNames...)
		if merged.AwaitingUserInput == nil && s.AwaitingUserInput != nil {
			merged.AwaitingUserInput = s.AwaitingUserInput
		}
	}
	return merged
}
