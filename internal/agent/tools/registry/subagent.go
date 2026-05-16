package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/aura/aura/internal/identity"
)

const (
	subagentMaxNodes      = 3
	subagentDefaultBudget = 60
)

// SubagentNodeSpec is the LLM-facing spec for one child dispatch.
// Mirrors swarm.NodeSpec (Phase 8 read-only subset) without importing
// internal/swarm, which would create an import cycle via
// internal/swarm → internal/agent → internal/agent/tools/registry.
type SubagentNodeSpec struct {
	Goal          string
	Instruction   string
	ToolAllowlist []string
	BudgetSecs    int
	ParentRunID   string
}

// SubagentResult is the collected output of a completed child run.
// Mirrors chat.Result without importing internal/chat (same cycle reason).
type SubagentResult struct {
	ReplyText      string
	ToolCallsCount int
	TokensTotal    int
	ElapsedMs      int64
}

// subagentDispatcher is the dispatch half.
// *swarm.HubBridge satisfies this via an adapter at the composition root
// (cmd/aura) which converts SubagentNodeSpec → swarm.NodeSpec.
type subagentDispatcher interface {
	Dispatch(ctx context.Context, spec SubagentNodeSpec, principalID string) (string, error)
}

// subagentWaiter is the collect half.
// *chat.Hub satisfies this via an adapter at the composition root that
// converts chat.Result → SubagentResult.
type subagentWaiter interface {
	WaitForRun(ctx context.Context, runID string) (SubagentResult, error)
}

// SubagentDispatchTool exposes the Phase 8 read-only fanout primitive to the
// LLM. action=spawn dispatches up to 3 child agents in parallel and returns
// their run IDs. action=collect blocks until all specified children finish and
// returns aggregated markdown.
type SubagentDispatchTool struct {
	dispatcher subagentDispatcher
	waiter     subagentWaiter
}

// NewSubagentDispatchTool returns a SubagentDispatchTool backed by dispatcher
// and waiter. Returns nil when either dep is nil so the caller can nil-gate
// the registration.
func NewSubagentDispatchTool(dispatcher subagentDispatcher, waiter subagentWaiter) *SubagentDispatchTool {
	if dispatcher == nil || waiter == nil {
		return nil
	}
	return &SubagentDispatchTool{dispatcher: dispatcher, waiter: waiter}
}

func (t *SubagentDispatchTool) Name() string { return "subagent_dispatch" }

func (t *SubagentDispatchTool) Description() string {
	return `Spawn up to 3 read-only subagents in parallel and collect their results. Each child runs in isolation with its own context window, tool allowlist (subset of yours), and budget. Cannot perform write operations (wiki_page write, source store, task schedule etc. — all blocked). Returns concise summaries on collect. Use for: parallel web search across topics, multi-source synthesis, divergent reasoning paths.

REQUIRED PARAMETERS BY ACTION (you MUST send all listed fields):
  • action="spawn":   nodes (array, max 3, each with required 'goal')
  • action="collect": child_run_ids (array of strings from a prior spawn)`
}

var subagentActions = []string{"spawn", "collect"}

var subagentHints = []ActionHint{
	{Name: "spawn", RequiredKeys: []string{"nodes"}},
	{Name: "collect", RequiredKeys: []string{"child_run_ids"}},
}

func (t *SubagentDispatchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        subagentActions,
				"description": fmt.Sprintf("spawn: dispatch up to %d read-only subagents in parallel, returns child_run_ids. collect: block until all children complete, returns aggregated markdown.", subagentMaxNodes),
			},
			"nodes": map[string]any{
				"type":        "array",
				"description": fmt.Sprintf("Array of child node specs for action=spawn (max %d).", subagentMaxNodes),
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"goal": map[string]any{
							"type":        "string",
							"description": "Goal for this child agent (required).",
						},
						"instruction": map[string]any{
							"type":        "string",
							"description": "Optional additional instruction context for the child.",
						},
						"tool_allowlist": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Optional subset of read-only tools available to this child.",
						},
						"budget_secs": map[string]any{
							"type":        "integer",
							"description": fmt.Sprintf("Max seconds for this child (default %d, max 300).", subagentDefaultBudget),
							"minimum":     1,
							"maximum":     300,
						},
					},
					"required": []string{"goal"},
				},
				"maxItems": subagentMaxNodes,
			},
			"child_run_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Array of child run IDs returned by a previous action=spawn (action=collect only).",
			},
		},
		"required": []string{"action"},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "spawn", RequiredKeys: []string{"nodes"}},
			{Name: "collect", RequiredKeys: []string{"child_run_ids"}},
		}),
	}
}

func (t *SubagentDispatchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("subagent_dispatch: tool unavailable")
	}
	action := stringArg(args, "action")
	if action == "" {
		return "", ActionRequiredError("subagent_dispatch", subagentActions, args, subagentHints, "spawn")
	}
	switch action {
	case "spawn":
		return t.executeSpawn(ctx, args)
	case "collect":
		return t.executeCollect(ctx, args)
	default:
		return "", UnknownActionError("subagent_dispatch", action, subagentActions, args)
	}
}

type subagentNodeArg struct {
	goal          string
	instruction   string
	toolAllowlist []string
	budgetSecs    int
}

func parseSubagentNodes(args map[string]any) ([]subagentNodeArg, error) {
	raw, ok := args["nodes"]
	if !ok || raw == nil {
		return nil, errors.New("subagent_dispatch: nodes is required for action=spawn")
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("subagent_dispatch: nodes must be an array")
	}
	if len(items) == 0 {
		return nil, errors.New("subagent_dispatch: nodes must not be empty")
	}
	if len(items) > subagentMaxNodes {
		return nil, fmt.Errorf("subagent_dispatch: cap %d read-only subagents per spawn; got %d", subagentMaxNodes, len(items))
	}
	nodes := make([]subagentNodeArg, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("subagent_dispatch: nodes[%d] must be an object", i)
		}
		goal, _ := m["goal"].(string)
		goal = strings.TrimSpace(goal)
		if goal == "" {
			return nil, fmt.Errorf("subagent_dispatch: nodes[%d].goal is required", i)
		}
		instruction, _ := m["instruction"].(string)
		toolAllowlist := subagentStringSlice(m["tool_allowlist"])
		budgetSecs := subagentDefaultBudget
		if v, ok := m["budget_secs"].(float64); ok && v >= 1 {
			budgetSecs = int(v)
		}
		nodes = append(nodes, subagentNodeArg{
			goal:          goal,
			instruction:   strings.TrimSpace(instruction),
			toolAllowlist: toolAllowlist,
			budgetSecs:    budgetSecs,
		})
	}
	return nodes, nil
}

func subagentStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				str = strings.TrimSpace(str)
				if str != "" {
					out = append(out, str)
				}
			}
		}
		return out
	}
	return nil
}

type subagentSpawnResult struct {
	runID string
	err   error
}

func (t *SubagentDispatchTool) executeSpawn(ctx context.Context, args map[string]any) (string, error) {
	nodes, err := parseSubagentNodes(args)
	if err != nil {
		return "", err
	}

	parentRunID := identity.RunIDFromContext(ctx)
	userID := UserIDFromContext(ctx)

	results := make([]subagentSpawnResult, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n subagentNodeArg) {
			defer wg.Done()
			spec := SubagentNodeSpec{
				Goal:          n.goal,
				Instruction:   n.instruction,
				ToolAllowlist: n.toolAllowlist,
				BudgetSecs:    n.budgetSecs,
				ParentRunID:   parentRunID,
			}
			runID, dispErr := t.dispatcher.Dispatch(ctx, spec, userID)
			results[idx] = subagentSpawnResult{runID: runID, err: dispErr}
		}(i, node)
	}
	wg.Wait()

	runIDs := make([]string, 0, len(results))
	var errs []string
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("node[%d]: %v", i, r.err))
		} else {
			runIDs = append(runIDs, r.runID)
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("subagent_dispatch spawn: %s", strings.Join(errs, "; "))
	}
	return strings.Join(runIDs, "\n"), nil
}

type subagentCollectResult struct {
	result SubagentResult
	err    error
}

func (t *SubagentDispatchTool) executeCollect(ctx context.Context, args map[string]any) (string, error) {
	runIDs := stringSliceArg(args, "child_run_ids")
	if len(runIDs) == 0 {
		return "", errors.New("subagent_dispatch: child_run_ids is required for action=collect")
	}

	results := make([]subagentCollectResult, len(runIDs))
	var wg sync.WaitGroup
	for i, runID := range runIDs {
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()
			res, waitErr := t.waiter.WaitForRun(ctx, id)
			results[idx] = subagentCollectResult{result: res, err: waitErr}
		}(i, runID)
	}
	wg.Wait()

	return subagentFormatCollect(runIDs, results), nil
}

func subagentFormatCollect(runIDs []string, results []subagentCollectResult) string {
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "## Subagent %d: %s\n\n", i+1, runIDs[i])
		if r.err != nil {
			fmt.Fprintf(&sb, "_Error: %v_\n\nTokens: 0 | Tool calls: 0 | Elapsed: 0 ms\n", r.err)
		} else {
			fmt.Fprintf(&sb, "%s\n\nTokens: %d | Tool calls: %d | Elapsed: %d ms\n",
				r.result.ReplyText, r.result.TokensTotal, r.result.ToolCallsCount, r.result.ElapsedMs)
		}
		if i < len(results)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
