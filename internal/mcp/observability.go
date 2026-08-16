package mcp

import "github.com/chetto1983/aura/internal/obs"

const mcpMeterScope = "github.com/chetto1983/aura/internal/mcp"

func newMCPBoundary(operation, transport string) *obs.Boundary {
	return obs.NewGlobalBoundary(mcpMeterScope, obs.BoundaryConfig{
		Operation: operation, ToolClass: obs.ToolClassMCP, Transport: transport,
		Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
	})
}

var (
	stdioInitializeBoundary = newMCPBoundary("mcp_initialize", "stdio")
	stdioListBoundary       = newMCPBoundary("mcp_list_tools", "stdio")
	stdioCallBoundary       = newMCPBoundary("mcp_call_tool", "stdio")
	stdioPingBoundary       = newMCPBoundary("mcp_ping", "stdio")
	httpInitializeBoundary  = newMCPBoundary("mcp_initialize", "http")
	httpListBoundary        = newMCPBoundary("mcp_list_tools", "http")
	httpCallBoundary        = newMCPBoundary("mcp_call_tool", "http")
	httpPingBoundary        = newMCPBoundary("mcp_ping", "http")

	// SDK-path boundaries. Built from the SAME factory and reusing the SAME transport
	// labels as the vars above: the metric names and dimension values a dashboard
	// already watches must not shift just because the client underneath is now the SDK.
	sdkStdioConnectBoundary = newMCPBoundary("mcp_initialize", "stdio")
	sdkHTTPConnectBoundary  = newMCPBoundary("mcp_initialize", "http")
	sdkStdioListBoundary    = newMCPBoundary("mcp_list_tools", "stdio")
	sdkHTTPListBoundary     = newMCPBoundary("mcp_list_tools", "http")
)
