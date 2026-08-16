package mcp

import "github.com/chetto1983/aura/internal/obs"

const mcpMeterScope = "github.com/chetto1983/aura/internal/mcp"

func newMCPBoundary(operation, transport string) *obs.Boundary {
	return obs.NewGlobalBoundary(mcpMeterScope, obs.BoundaryConfig{
		Operation: operation, ToolClass: obs.ToolClassMCP, Transport: transport,
		Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
	})
}

// SDK-path boundaries. Built from the SAME factory and reusing the SAME
// "mcp_initialize"/"mcp_list_tools" operation names and "stdio"/"http" transport
// labels the pre-SDK bespoke client used for its own initialize/list boundaries
// (deleted with client.go/http_client.go in plan 45.1-03): the metric names and
// dimension values a dashboard already watches must not shift just because the
// client underneath is now the SDK.
//
// There is no SDK-path "call"/"ping" boundary pair: the bespoke client's
// stdioCallBoundary/httpCallBoundary/stdioPingBoundary/httpPingBoundary died with
// it and nothing in bridge_supervisor.go's CallToolText re-attached an equivalent —
// a gap noted for a later plan, not silently carried forward as dead vars here.
var (
	sdkStdioConnectBoundary = newMCPBoundary("mcp_initialize", "stdio")
	sdkHTTPConnectBoundary  = newMCPBoundary("mcp_initialize", "http")
	sdkStdioListBoundary    = newMCPBoundary("mcp_list_tools", "stdio")
	sdkHTTPListBoundary     = newMCPBoundary("mcp_list_tools", "http")
)
