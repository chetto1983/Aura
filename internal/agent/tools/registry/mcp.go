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
	client     mcp.ToolCaller
	server     mcp.Server
	serverName string
	toolName   string
	tool       mcp.Tool
}

// NewMCPTool wraps one server tool. Caller is responsible for keeping the
// client alive (closing it on shutdown via mcp.Client.Close).
func NewMCPTool(client mcp.ToolCaller, serverName string, tool mcp.Tool) *MCPTool {
	server, _ := client.(mcp.Server)
	return &MCPTool{client: client, server: server, serverName: serverName, toolName: tool.Name, tool: tool}
}

func (t *MCPTool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.serverName, t.toolName)
}

func (t *MCPTool) Category() string {
	return CategoryMCP
}

func (t *MCPTool) Available() bool {
	if t.server == nil {
		return true
	}
	for _, tool := range t.server.Tools() {
		if tool.Name == t.toolName {
			return true
		}
	}
	return false
}

func (t *MCPTool) Description() string {
	tool := t.currentTool()
	desc := tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", tool.Name, t.serverName)
	}
	out := fmt.Sprintf("[MCP: %s] %s", t.serverName, desc)
	// Inject a discovery hint for tools that take an account_id. Without
	// this the model defaults to account_id="default" on the first try,
	// fails, then waste a turn discovering the right value.
	if t.requiresAccountID() {
		out += fmt.Sprintf(" If account_id is required, call mcp_%s_list_all_accounts first to enumerate valid IDs — there is no implicit \"default\".", t.serverName)
	}
	return out
}

func (t *MCPTool) requiresAccountID() bool {
	tool := t.currentTool()
	if tool.InputSchema == nil {
		return false
	}
	props, _ := tool.InputSchema["properties"].(map[string]any)
	if _, ok := props["account_id"]; !ok {
		return false
	}
	required, _ := tool.InputSchema["required"].([]any)
	for _, name := range required {
		if s, _ := name.(string); s == "account_id" {
			return true
		}
	}
	return false
}

// Parameters returns the upstream JSON Schema unchanged when present;
// otherwise a permissive empty object so providers that require a schema
// don't reject the tool definition.
func (t *MCPTool) Parameters() map[string]any {
	tool := t.currentTool()
	if tool.InputSchema != nil {
		return tool.InputSchema
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
	return t.client.CallTool(ctx, t.toolName, args)
}

func (t *MCPTool) currentTool() mcp.Tool {
	if t.server != nil {
		for _, tool := range t.server.Tools() {
			if tool.Name == t.toolName {
				return tool
			}
		}
	}
	return t.tool
}
