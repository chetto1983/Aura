package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// fakeSubagentDispatcher records Dispatch calls and returns synthetic run IDs.
type fakeSubagentDispatcher struct {
	mu    sync.Mutex
	calls int
	specs []SubagentNodeSpec // captured for per-call assertions
	errFn func(idx int) error
}

func (f *fakeSubagentDispatcher) Dispatch(_ context.Context, spec SubagentNodeSpec, _ string) (string, error) {
	f.mu.Lock()
	f.calls++
	idx := f.calls - 1
	id := strings.Repeat("a", 8+idx) // distinct IDs: "aaaaaaaa", "aaaaaaaaa", ...
	f.specs = append(f.specs, spec)
	f.mu.Unlock()
	if f.errFn != nil {
		if err := f.errFn(idx); err != nil {
			return "", err
		}
	}
	return id, nil
}

// fakeSubagentWaiter returns synthetic SubagentResult values.
type fakeSubagentWaiter struct {
	resultFn func(runID string) (SubagentResult, error)
}

func (f *fakeSubagentWaiter) WaitForRun(_ context.Context, runID string) (SubagentResult, error) {
	if f.resultFn != nil {
		return f.resultFn(runID)
	}
	return SubagentResult{ReplyText: "done"}, nil
}

func newSubagentTool(d *fakeSubagentDispatcher, w *fakeSubagentWaiter) *SubagentDispatchTool {
	return &SubagentDispatchTool{dispatcher: d, waiter: w}
}

// TestSubagentDispatch_Spawn1Node verifies spawn with 1 node returns 1 run ID.
func TestSubagentDispatch_Spawn1Node(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{}
	tool := newSubagentTool(d, w)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "spawn",
		"nodes":  []any{map[string]any{"goal": "count to 3"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty run ID in result")
	}
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", calls)
	}
}

// TestSubagentDispatch_Spawn3NodeParallel verifies 3 nodes each get dispatched
// and 3 distinct run IDs are returned.
func TestSubagentDispatch_Spawn3NodeParallel(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{}
	tool := newSubagentTool(d, w)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "spawn",
		"nodes": []any{
			map[string]any{"goal": "task A"},
			map[string]any{"goal": "task B"},
			map[string]any{"goal": "task C"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 run IDs (newline-separated), got %d: %q", len(lines), result)
	}
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if calls != 3 {
		t.Fatalf("expected 3 dispatch calls, got %d", calls)
	}
}

// TestSubagentDispatch_Spawn4NodeRejected verifies that 4 nodes are rejected
// with an error (cap is 3) and no dispatches are made.
func TestSubagentDispatch_Spawn4NodeRejected(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{}
	tool := newSubagentTool(d, w)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "spawn",
		"nodes": []any{
			map[string]any{"goal": "A"},
			map[string]any{"goal": "B"},
			map[string]any{"goal": "C"},
			map[string]any{"goal": "D"},
		},
	})
	if err == nil {
		t.Fatal("expected error for 4 nodes (cap 3), got nil")
	}
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 dispatch calls, got %d", calls)
	}
}

// TestSubagentDispatch_Collect1Child verifies collect returns a well-formed
// markdown section containing the child's reply and stats.
func TestSubagentDispatch_Collect1Child(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{
		resultFn: func(_ string) (SubagentResult, error) {
			return SubagentResult{
				ReplyText:      "found 3 results",
				ToolCallsCount: 2,
				TokensTotal:    100,
				ElapsedMs:      500,
			}, nil
		},
	}
	tool := newSubagentTool(d, w)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":        "collect",
		"child_run_ids": []any{"run-1"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "found 3 results") {
		t.Errorf("result missing child reply text: %q", result)
	}
	if !strings.Contains(result, "Tool calls: 2") {
		t.Errorf("result missing tool call count: %q", result)
	}
	if !strings.Contains(result, "Tokens: 100") {
		t.Errorf("result missing token count: %q", result)
	}
}

// TestSubagentDispatch_SpawnMixedTier verifies that 2 nodes with different
// risk tiers (read_only + write_proposal) are both dispatched successfully
// and both run IDs are returned.
func TestSubagentDispatch_SpawnMixedTier(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{}
	tool := newSubagentTool(d, w)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "spawn",
		"nodes": []any{
			map[string]any{"goal": "read task", "risk_tier": "read_only"},
			map[string]any{
				"goal":           "propose update",
				"risk_tier":      "write_proposal",
				"tool_allowlist": []any{"propose_patch", "web_search"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 run IDs, got %d: %q", len(lines), result)
	}
	d.mu.Lock()
	calls := d.calls
	specs := d.specs
	d.mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected 2 dispatch calls, got %d", calls)
	}
	// verify RiskTier propagated per node
	tiers := map[string]bool{}
	for _, s := range specs {
		tiers[s.RiskTier] = true
	}
	if !tiers["read_only"] || !tiers["write_proposal"] {
		t.Errorf("expected both read_only and write_proposal tiers dispatched; got: %v", tiers)
	}
}

// TestSubagentDispatch_SpawnWriteProposalForbiddenTool verifies that a
// write_proposal node with an explicit forbidden tool (wiki_page) returns a
// validation error before any dispatch is made.
func TestSubagentDispatch_SpawnWriteProposalForbiddenTool(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{}
	tool := newSubagentTool(d, w)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "spawn",
		"nodes": []any{
			map[string]any{
				"goal":           "try to write",
				"risk_tier":      "write_proposal",
				"tool_allowlist": []any{"wiki_page"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected validation error for write_proposal allowlist containing wiki_page, got nil")
	}
	if !strings.Contains(err.Error(), "wiki_page") {
		t.Errorf("expected error to mention wiki_page, got: %v", err)
	}
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 dispatch calls (validation fails before dispatch), got %d", calls)
	}
}

// TestSubagentDispatch_SpawnWriteProposalDefaultAllowlist verifies that a
// write_proposal node with no explicit tool_allowlist is accepted: the server
// applies a safe default allowlist containing propose_patch.
func TestSubagentDispatch_SpawnWriteProposalDefaultAllowlist(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{}
	tool := newSubagentTool(d, w)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "spawn",
		"nodes": []any{
			map[string]any{"goal": "propose something", "risk_tier": "write_proposal"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error with default write_proposal allowlist, got: %v", err)
	}
	d.mu.Lock()
	calls := d.calls
	specs := d.specs
	d.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", calls)
	}
	dispatched := specs[0]
	hasPatch := false
	for _, name := range dispatched.ToolAllowlist {
		if name == "propose_patch" {
			hasPatch = true
		}
	}
	if !hasPatch {
		t.Errorf("expected default allowlist to contain propose_patch; got: %v", dispatched.ToolAllowlist)
	}
	if dispatched.RiskTier != "write_proposal" {
		t.Errorf("expected RiskTier=write_proposal, got %q", dispatched.RiskTier)
	}
}

// TestSubagentDispatch_CollectMixed verifies that when one child succeeds and
// one times out, collect returns partial results with per-slot error annotation
// for the failed slot, without returning an outer error.
func TestSubagentDispatch_CollectMixed(t *testing.T) {
	d := &fakeSubagentDispatcher{}
	w := &fakeSubagentWaiter{
		resultFn: func(runID string) (SubagentResult, error) {
			if runID == "run-ok" {
				return SubagentResult{ReplyText: "success output", TokensTotal: 50}, nil
			}
			return SubagentResult{}, context.DeadlineExceeded
		},
	}
	tool := newSubagentTool(d, w)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":        "collect",
		"child_run_ids": []any{"run-ok", "run-timeout"},
	})
	if err != nil {
		t.Fatalf("Execute: %v (mixed collect should not fail the outer call)", err)
	}
	if !strings.Contains(result, "success output") {
		t.Errorf("result missing successful child output: %q", result)
	}
	if !strings.Contains(result, "Error:") {
		t.Errorf("result missing per-slot error annotation for failed child: %q", result)
	}
	if !strings.Contains(result, "Subagent 1:") {
		t.Errorf("result missing Subagent 1 section header: %q", result)
	}
	if !strings.Contains(result, "Subagent 2:") {
		t.Errorf("result missing Subagent 2 section header: %q", result)
	}
}
