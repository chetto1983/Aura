package tools

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/mcp"
)

// MCPTool adapts a single MCP-server tool to the Aura Tool interface so the
// LLM can call it like any built-in. Names are scoped per server as
// `mcp_<server>_<tool>` to avoid collisions across servers and with native
// tools.
type MCPTool struct {
	client     *mcp.Client
	serverName string
	tool       mcp.Tool
}

// NewMCPTool wraps one server tool. Caller is responsible for keeping the
// client alive (closing it on shutdown via mcp.Client.Close).
func NewMCPTool(client *mcp.Client, serverName string, tool mcp.Tool) *MCPTool {
	return &MCPTool{client: client, serverName: serverName, tool: tool}
}

func (t *MCPTool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.serverName, t.tool.Name)
}

func (t *MCPTool) Description() string {
	desc := t.tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", t.tool.Name, t.serverName)
	}
	return fmt.Sprintf("[MCP: %s] %s", t.serverName, desc)
}

// Parameters returns the upstream JSON Schema unchanged when present;
// otherwise a permissive empty object so providers that require a schema
// don't reject the tool definition.
func (t *MCPTool) Parameters() map[string]any {
	if t.tool.InputSchema != nil {
		return t.tool.InputSchema
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("%s: mcp client unavailable", t.Name())
	}
	return t.client.CallTool(ctx, t.tool.Name, args)
}
