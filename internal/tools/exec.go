package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/sandbox"
)

// ExecuteCodeTool lets the LLM run Python code in Aura's isolated runtime.
type ExecuteCodeTool struct {
	manager *sandbox.Manager
	sender  DocumentSender
}

// NewExecuteCodeTool creates the execute_code tool. Returns nil if manager
// is nil (sandbox not available).
func NewExecuteCodeTool(manager *sandbox.Manager) *ExecuteCodeTool {
	return NewExecuteCodeToolWithSender(manager, nil)
}

// NewExecuteCodeToolWithSender creates execute_code with optional artifact
// delivery. The sender is used only when sandbox code emits artifacts.
func NewExecuteCodeToolWithSender(manager *sandbox.Manager, sender DocumentSender) *ExecuteCodeTool {
	if manager == nil {
		return nil
	}
	return &ExecuteCodeTool{manager: manager, sender: sender}
}

func (t *ExecuteCodeTool) Name() string { return "execute_code" }

func (t *ExecuteCodeTool) Description() string {
	return "Execute Python code in an isolated WASM sandbox. " +
		"Use this for calculations, data processing, simulations, or any task that requires running code. " +
		"The sandbox is ephemeral; no state persists between executions. " +
		"To return files, write them under /tmp/aura_out; Aura collects plain files from that directory and delivers them to Telegram when possible. " +
		"Packages are limited to Aura's bundled runtime profile. Set allow_network=true only when HTTP access is explicitly needed. " +
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
	if len(result.Artifacts) > 0 {
		delivered, err := t.deliverArtifacts(ctx, result.Artifacts)
		if err != nil {
			return "", err
		}
		out += "\n\nartifacts:"
		for i, artifact := range result.Artifacts {
			out += fmt.Sprintf("\n- %s (%d bytes, %s, delivered=%t)", artifact.Name, artifact.SizeBytes, artifact.MimeType, delivered[i])
		}
	}
	return out, nil
}

func (t *ExecuteCodeTool) deliverArtifacts(ctx context.Context, artifacts []sandbox.Artifact) ([]bool, error) {
	delivered := make([]bool, len(artifacts))
	if t.sender == nil || UserIDFromContext(ctx) == "" {
		return delivered, nil
	}
	userID := UserIDFromContext(ctx)
	for i, artifact := range artifacts {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = fmt.Sprintf("artifact-%d.bin", i+1)
		}
		caption := "Aura sandbox artifact: " + name
		if err := t.sender.SendDocumentToUser(userID, name, artifact.Bytes, caption); err != nil {
			return delivered, fmt.Errorf("execute_code: artifact %s delivery failed: %w", name, err)
		}
		delivered[i] = true
	}
	return delivered, nil
}
