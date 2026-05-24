package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadToolResultTool lets the LLM retrieve the full content of a spilled tool
// result from disk using the path returned in a spill envelope.
//
// It accepts the /workspace/tool-results/... path embedded in the envelope and
// maps it to the actual filesystem location under baseDir.
type ReadToolResultTool struct {
	baseDir string
}

// NewReadToolResultTool creates the tool. baseDir is the runtime-workspace
// directory (AURA_RUNTIME_WORKSPACE_PATH). When baseDir is empty the tool
// returns an error on every call.
func NewReadToolResultTool(baseDir string) *ReadToolResultTool {
	return &ReadToolResultTool{baseDir: baseDir}
}

func (t *ReadToolResultTool) Name() string { return "read_tool_result" }

func (t *ReadToolResultTool) Description() string {
	return "Read the full content of a spilled tool result. Use the path from a [tool result spilled to disk] envelope. Required: path (string, /workspace/tool-results/... form)."
}

func (t *ReadToolResultTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"path"},
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The /workspace/tool-results/<session>/<call>.txt path from the spill envelope.",
			},
		},
		"examples": []any{
			map[string]any{"path": "/workspace/tool-results/sess-123/call-abc.txt"},
		},
	}
}

func (t *ReadToolResultTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t.baseDir == "" {
		return "", fmt.Errorf("read_tool_result: spill storage not configured")
	}
	llmPath, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}

	const prefix = "/workspace/tool-results/"
	if !strings.HasPrefix(llmPath, prefix) {
		return "", fmt.Errorf("read_tool_result: path must start with %s", prefix)
	}
	rel := strings.TrimPrefix(llmPath, prefix)

	// Guard path traversal.
	if strings.Contains(rel, "..") {
		return "", fmt.Errorf("read_tool_result: path traversal not allowed")
	}

	abs := filepath.Join(t.baseDir, "tool-results", rel)
	data, readErr := os.ReadFile(abs)
	if readErr != nil {
		return "", fmt.Errorf("read_tool_result: %w", readErr)
	}
	return string(data), nil
}
