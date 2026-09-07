package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func addMemoryGraphTools(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "graph_diagnostics", Title: "Memory graph diagnostics",
		Description: "Read native graph summary, connected components, degree and k-core for memory entities. Choose facts, mentions or combined. Stored topology includes all validity windows; this is not current truth or personal importance. Node details are limited; totals cover the bounded graph.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input arcadedb.MemoryGraphDiagnosticsRequest) (*mcp.CallToolResult, arcadedb.MemoryGraphDiagnostics, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, arcadedb.MemoryGraphDiagnostics{}, err
		}
		out, err := client.MemoryGraphDiagnostics(ctx, input)
		return nil, out, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "graph_path", Title: "Memory entity connection",
		Description: "Find a native shortest path between two exact entity names, bounded to 1..6 hops. Only FACT and/or MENTIONS are followed. Optional as_of selects relationships whose facts are valid at that RFC3339 instant, with repeatable-read evidence; this covers stored relationships, not reconstruction of removed history. Without as_of returns topology across all validity windows. Returns original edge direction, fact sources and validity. A connection is not proof of causality; MENTIONS carries supporting_fact or support_missing and is not itself a fact.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input arcadedb.MemoryGraphPathRequest) (*mcp.CallToolResult, arcadedb.MemoryGraphPath, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, arcadedb.MemoryGraphPath{}, err
		}
		out, err := client.MemoryGraphPath(ctx, input)
		return nil, out, err
	})
}
