package arcadedb

// Filling in the body of a searched trace.
//
// GetReasoningTrace, which takes an exact trace_id, has always loaded the steps
// and the tool calls beneath them. SearchReasoningTraces, which is how the agent
// reaches a trace when it does NOT already know the id, returned only the header:
// summary, status, turn. Measured 2026-09-03 through the memory MCP, every one of
// five traces came back with steps:[] -- so the audit could say that Aura had
// reasoned about something and never what it did about it, which is the half a
// reader is looking for.
//
// The two statements are set-shaped on purpose. A search returns several traces
// at once and hydrating them one at a time would issue 2N round-trips to answer
// what two answer; the per-trace statements stay for the get-by-id path, whose
// single bind reads better than a one-element list.

import (
	"context"
	"fmt"
	"strings"
)

// hydrateReasoningBodies attaches steps and tool calls to traces, in place.
//
// A trace whose body fails to load is not silently returned bare: the reader
// cannot tell an empty trace from an unread one, and a header presented as a
// complete audit record is worse than an error.
func (c *Client) hydrateReasoningBodies(
	ctx context.Context,
	identityID string,
	traces []ReasoningTrace,
) ([]ReasoningTrace, error) {
	ids := make([]string, 0, len(traces))
	at := make(map[string]int, len(traces))
	for index, trace := range traces {
		id := strings.TrimSpace(trace.TraceID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
		at[id] = index
	}
	if len(ids) == 0 {
		return traces, nil
	}
	params := map[string]any{"identity_id": identityID, "trace_ids": ids}
	stepRows, err := c.Query(ctx, listReasoningStepsStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: list reasoning steps: %w", err)
	}
	toolRows, err := c.Query(ctx, listReasoningToolsStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: list reasoning tools: %w", err)
	}
	for id, index := range at {
		steps, stepErr := reasoningStepsFromRows(rowsForTrace(stepRows, id), identityID, id)
		if stepErr != nil {
			return nil, stepErr
		}
		traces[index].Steps = steps
		if err := attachReasoningTools(&traces[index], rowsForTrace(toolRows, id)); err != nil {
			return nil, err
		}
	}
	return traces, nil
}

// rowsForTrace partitions one multi-trace result set. Filtering in Go rather than
// re-querying per trace is the whole point of the set statements; the row count is
// already bounded by their LIMIT.
func rowsForTrace(rows []map[string]any, traceID string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if value, ok := row["trace_id"].(string); ok && value == traceID {
			out = append(out, row)
		}
	}
	return out
}
