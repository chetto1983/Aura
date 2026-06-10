package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

type roundtripFunc func(context.Context, string, any) (json.RawMessage, error)

func listToolsWith(ctx context.Context, serverName string, roundtrip roundtripFunc) ([]ToolDef, error) {
	res, err := roundtrip(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp %q: tools/list: %w", serverName, err)
	}
	var env struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(res, &env); err != nil {
		return nil, fmt.Errorf("mcp %q: decode tools/list: %w", serverName, err)
	}
	return env.Tools, nil
}

func callToolWith(ctx context.Context, serverName, toolName string, args map[string]any, roundtrip roundtripFunc) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := roundtrip(ctx, "tools/call", map[string]any{"name": toolName, "arguments": args})
	if err != nil {
		return "", fmt.Errorf("mcp %q: call %s: %w", serverName, toolName, err)
	}
	text, isErr, derr := decodeToolResult(res)
	if derr != nil {
		return "", fmt.Errorf("mcp %q: call %s: %w", serverName, toolName, derr)
	}
	if isErr {
		return "", fmt.Errorf("mcp %q: tool %s reported error: %s", serverName, toolName, text)
	}
	return text, nil
}
