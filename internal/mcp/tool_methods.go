package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/idempotency"
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
	params := map[string]any{"name": toolName, "arguments": args}
	if operation, ok := idempotency.OperationFromContext(ctx); ok {
		params["_meta"] = map[string]any{
			"aura": map[string]any{
				"operation_key":         operation.Key.Key,
				"operation_scope":       string(operation.Key.Scope),
				"operation_fingerprint": idempotency.FingerprintHex(operation.Fingerprint),
			},
		}
	}
	res, err := roundtrip(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("mcp %q: call %s: %w", serverName, toolName, err)
	}
	text, isErr, derr := decodeToolResult(res)
	if derr != nil {
		return "", fmt.Errorf("mcp %q: call %s: %w", serverName, toolName, derr)
	}
	if isErr {
		return "", DecodeToolCallError(serverName, toolName, text)
	}
	return text, nil
}
