package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/aura/aura/internal/identity"
)

const (
	subagentMaxNodes      = 3
	subagentDefaultBudget = 60
)

// SubagentNodeSpec is the LLM-facing spec for one child dispatch.
// Mirrors swarm.NodeSpec (Phase 8 subset) without importing internal/swarm,
// which would create an import cycle via
// internal/swarm → internal/agent → internal/agent/tools/registry.
type SubagentNodeSpec struct {
	Goal          string
	Instruction   string
	ToolAllowlist []string
	BudgetSecs    int
	ParentRunID   string
	RiskTier      string // "read_only" | "write_proposal"
}

// subagentDirectWriteTools mirrors swarm.DirectWriteToolNames without
// importing internal/swarm (import-cycle constraint).
var subagentDirectWriteTools = []string{
	"wiki_page",
	"source",
	"task",
	"file",
	"agent_note",
}

// writeProposalDefaultAllowlist is applied when a write_proposal node
// provides no explicit tool_allowlist. Contains propose_patch + common
// read-only tools.
var writeProposalDefaultAllowlist = []string{
	"propose_patch",
	"web",
	"search_memory",
	"recall_operational",
	"recall_user_memory",
	"wiki_subgraph",
}

// enforceWriteProposalNodeAllowlist returns the effective allowlist for a
// write_proposal node. An empty explicit list is replaced with
// writeProposalDefaultAllowlist. A non-empty explicit list must contain
// "propose_patch" and must not contain any direct-write tool name.
func enforceWriteProposalNodeAllowlist(explicit []string) ([]string, error) {
	if len(explicit) == 0 {
		return writeProposalDefaultAllowlist, nil
	}
	for _, name := range explicit {
		if slices.Contains(subagentDirectWriteTools, name) {
			return nil, fmt.Errorf("write_proposal allowlist must not contain direct-write tool %q", name)
		}
	}
	if !slices.Contains(explicit, "propose_patch") {
		return nil, fmt.Errorf("write_proposal allowlist must contain %q", "propose_patch")
	}
	return explicit, nil
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
	return `Spawn up to 3 subagents in parallel (read_only or write_proposal tier) and collect their results. Each child runs in isolation with its own context window, tool allowlist, and budget. read_only children cannot write (wiki_page, source, task, file, agent_note — all blocked). Returns concise summaries on collect. Use for: parallel web search, multi-source synthesis, divergent reasoning paths.

You can also dispatch write_proposal subagents. They can call propose_patch to suggest reviewer-gated changes (wiki, user_memory, operational) but CANNOT directly write to wiki, sources, scheduled tasks, files, or agent_note. Approval flows through the human reviewer.

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
							"description": "Optional subset of tools available to this child. For write_proposal tier, must include propose_patch and must not include direct-write tools.",
						},
						"budget_secs": map[string]any{
							"type":        "integer",
							"description": fmt.Sprintf("Max seconds for this child (default %d, max 300).", subagentDefaultBudget),
							"minimum":     1,
							"maximum":     300,
						},
						"risk_tier": map[string]any{
							"type":        "string",
							"enum":        []string{"read_only", "write_proposal"},
							"description": "Risk tier: read_only (default) blocks all writes; write_proposal allows propose_patch only — all direct writes still blocked.",
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
	riskTier      string
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
		riskTier, _ := m["risk_tier"].(string)
		riskTier = strings.TrimSpace(riskTier)
		if riskTier == "" {
			riskTier = "read_only"
		}
		if riskTier != "read_only" && riskTier != "write_proposal" {
			return nil, fmt.Errorf("subagent_dispatch: nodes[%d].risk_tier %q not allowed (must be read_only or write_proposal)", i, riskTier)
		}
		if riskTier == "write_proposal" {
			al, alErr := enforceWriteProposalNodeAllowlist(toolAllowlist)
			if alErr != nil {
				return nil, fmt.Errorf("subagent_dispatch: nodes[%d]: %w", i, alErr)
			}
			toolAllowlist = al
		}
		nodes = append(nodes, subagentNodeArg{
			goal:          goal,
			instruction:   strings.TrimSpace(instruction),
			toolAllowlist: toolAllowlist,
			budgetSecs:    budgetSecs,
			riskTier:      riskTier,
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
				RiskTier:      n.riskTier,
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
