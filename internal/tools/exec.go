package tools

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/sandbox"
)

// ExecuteCodeTool lets the LLM run Python code in Aura's isolated runtime.
type ExecuteCodeTool struct {
	manager *sandbox.Manager
}

// NewExecuteCodeTool creates the execute_code tool. Returns nil if manager
// is nil (sandbox not available).
func NewExecuteCodeTool(manager *sandbox.Manager) *ExecuteCodeTool {
	if manager == nil {
		return nil
	}
	return &ExecuteCodeTool{manager: manager}
}

func (t *ExecuteCodeTool) Name() string { return "execute_code" }

func (t *ExecuteCodeTool) Description() string {
	return "Execute Python code in an isolated WASM sandbox. " +
		"Use this for calculations, data processing, simulations, or any task that requires running code. " +
		"The sandbox is ephemeral — no state persists between executions. " +
		"Stdlib only by default. Set allow_network=true if the code needs to make HTTP requests. " +
		"Timeout is configurable up to the server limit (default 15s)."
}

func (t *ExecuteCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "Python code to execute in the sandbox",
			},
			"allow_network": map[string]any{
				"type":        "boolean",
				"description": "Allow network access from the sandbox. Default false.",
			},
		},
		"required": []string{"code"},
	}
}

func (t *ExecuteCodeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code is required and must be a string")
	}

	allowNetwork := false
	if v, ok := args["allow_network"].(bool); ok {
		allowNetwork = v
	}

	result, err := t.manager.Execute(ctx, code, allowNetwork)
	if err != nil {
		return "", fmt.Errorf("sandbox execution failed: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("execution failed (exit=%d): %s", result.ExitCode, result.Stderr)
	}

	out := fmt.Sprintf("exit_code: %d\nelapsed_ms: %d\n\n%s", result.ExitCode, result.ElapsedMs, result.Stdout)
	if result.Stderr != "" {
		out += fmt.Sprintf("\n--- stderr ---\n%s", result.Stderr)
	}
	return out, nil
}
