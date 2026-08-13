package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// defaultReadLimit is the byte window read_tool_output returns when limit is
// omitted, aligned to the preview cap (D-27/A4 — BYTES, not lines). Paging in
// 2 KB windows out of a sidecar that only exists because the output was LARGE
// meant the model paid a round-trip per window; the window now matches what a
// first-class result is allowed to be.
const defaultReadLimit = 30000

const maxReadToolOutputLimit = defaultReadLimit * 8

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
    "limit": {"type": "integer", "description": "Maximum number of BYTES to return. Units are BYTES, not lines."}
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
	clamped := false
	if limit > maxReadToolOutputLimit {
		limit = maxReadToolOutputLimit
		clamped = true
	}

	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ToolResult{}, fmt.Errorf("read_tool_output: missing tool-call context")
	}
	// Assert the sidecar runDir invariant (AG-050): WithToolCallContext must carry
	// an absolute run root. A relative runDir would resolve the sidecar against the
	// process cwd, escaping the intended run root — reject it rather than trust it.
	if !filepath.IsAbs(tc.runDir) {
		return ToolResult{}, fmt.Errorf("read_tool_output: sidecar runDir %q is not absolute", tc.runDir)
	}
	// Reuse the result.go id-validation + path layout (T-03-07): the public
	// argument is named tool_call_id for compatibility, but new footers pass an
	// opaque sidecar spill id. session_id comes from the agent's ctx, not the model.
	path, err := sidecarPath(tc.runDir, tc.sessionID, a.ToolCallID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("read_tool_output: %w", err)
	}

	// #nosec G304 -- path is not attacker-controlled: sidecarPath runs both ids
	// through validateID's [A-Za-z0-9_-] allowlist BEFORE filepath.Join, so no
	// separator or dot can survive, and runDir is asserted absolute above.
	f, err := os.Open(path)
	if err != nil && os.IsNotExist(err) && a.ToolCallID == tc.toolCallID && tc.sidecarID != "" && tc.sidecarID != a.ToolCallID {
		if altPath, altErr := sidecarPath(tc.runDir, tc.sessionID, tc.sidecarID); altErr == nil {
			// #nosec G304 -- altPath comes from the same validated sidecarPath.
			alt, altOpenErr := os.Open(altPath)
			if altOpenErr == nil || !os.IsNotExist(altOpenErr) {
				path, f, err = altPath, alt, altOpenErr
			}
		}
	}
	if err != nil {
		// Unknown / never-spilled id hard-fails (D-15/Req#7) — this becomes a
		// RoleTool error the model sees, never a panic or empty content.
		if os.IsNotExist(err) {
			return ToolResult{}, fmt.Errorf("read_tool_output: no output for tool_call_id %q", a.ToolCallID)
		}
		return ToolResult{}, fmt.Errorf("read_tool_output: read sidecar: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ToolResult{}, fmt.Errorf("read_tool_output: read sidecar: %w", err)
	}
	if info.IsDir() {
		return ToolResult{}, fmt.Errorf("read_tool_output: read sidecar: %s is a directory", path)
	}

	total := info.Size()
	start := min(int64(a.Offset), total)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ToolResult{}, fmt.Errorf("read_tool_output: read sidecar: %w", err)
	}

	buf := make([]byte, limit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ToolResult{}, fmt.Errorf("read_tool_output: read sidecar: %w", err)
	}
	end := start + int64(n)
	slice := string(buf[:n])
	clampNote := ""
	if clamped {
		clampNote = fmt.Sprintf("; limit clamped to %d", maxReadToolOutputLimit)
	}
	footer := fmt.Sprintf("\n\n[showing bytes %d-%d of %d, next offset %d%s]", start, end, total, end, clampNote)
	// read_tool_output's own output is bounded by limit, so it never re-spills.
	return ToolResult{Preview: slice + footer, Bytes: n}, nil
}
