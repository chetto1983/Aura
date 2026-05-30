package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// defaultReadLimit is the byte window read_tool_output returns when limit is
// omitted, aligned to the preview cap (D-27/A4 — BYTES, not lines).
const defaultReadLimit = 2048

// ReadToolOutput pages byte ranges out of a sidecar file written by NewResult
// when a prior tool output exceeded the preview cap. Non-deferred builtin: the
// model calls it to fetch the full bytes a truncated preview pointed at (D-27).
type ReadToolOutput struct{}

type readToolOutputArgs struct {
	ToolCallID string `json:"tool_call_id"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
}

func (ReadToolOutput) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "tool_call_id": {"type": "string", "description": "The tool_call_id whose full output to page (from a truncated preview footer)."},
    "offset": {"type": "integer", "description": "Start byte offset into the full output (default 0). Units are BYTES, not lines."},
    "limit": {"type": "integer", "description": "Maximum number of BYTES to return (default 2048). Units are BYTES, not lines."}
  },
  "required": ["tool_call_id"]
}`)
	return Spec{
		Name:        "read_tool_output",
		Summary:     "Page a byte range from a previously truncated tool output.",
		Description: "When a tool output was truncated, its preview footer carries a tool_call_id. Call read_tool_output with that id plus a byte offset and limit to page through the full output. offset and limit are measured in BYTES (not lines); the footer reports 'showing bytes X-Y of Z, next offset Y' so you can page deterministically.",
		Parameters:  params,
		Deferred:    false,
	}
}

func (ReadToolOutput) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a readToolOutputArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("read_tool_output args: %w", err)
	}
	if a.ToolCallID == "" {
		return ToolResult{}, fmt.Errorf("read_tool_output: tool_call_id is required")
	}
	if a.Offset < 0 {
		return ToolResult{}, fmt.Errorf("read_tool_output: offset %d is negative", a.Offset)
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}

	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ToolResult{}, fmt.Errorf("read_tool_output: missing tool-call context")
	}
	// Reuse the result.go id-validation + path layout (T-03-07): the model
	// supplies tool_call_id, so it is validated exactly like a spill id before
	// filepath.Join. session_id comes from the agent's ctx, not the model.
	path, err := sidecarPath(tc.runDir, tc.sessionID, a.ToolCallID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("read_tool_output: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// Unknown / never-spilled id hard-fails (D-15/Req#7) — this becomes a
		// RoleTool error the model sees, never a panic or empty content.
		if os.IsNotExist(err) {
			return ToolResult{}, fmt.Errorf("read_tool_output: no output for tool_call_id %q", a.ToolCallID)
		}
		return ToolResult{}, fmt.Errorf("read_tool_output: read sidecar: %w", err)
	}

	total := len(data)
	start := a.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	slice := string(data[start:end])
	footer := fmt.Sprintf("\n\n[showing bytes %d-%d of %d, next offset %d]", start, end, total, end)
	// read_tool_output's own output is bounded by limit, so it never re-spills.
	return ToolResult{Preview: slice + footer, Bytes: len(slice)}, nil
}
