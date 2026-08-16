package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// GraphSchemaInput is deliberately empty of everything, including the identity
// (D-108: it travels in _meta.aura.user_identifier now, not here) — the schema
// of the connected database is the whole answer, and taking a database name
// here would let a caller read across tenants. The schema is per tenant because
// the database is. Keeping the type (rather than deleting it) is deliberate:
// the "taking a database name here would let a caller read across tenants"
// reasoning is still exactly right, and now it is enforced structurally by an
// empty struct rather than merely by convention.
type GraphSchemaInput struct{}

// GraphSchemaOutput is an alias, not a copy. The statement and its projection now
// live in internal/arcadedb because the cockpit's schema-only graph view reads the
// same catalogue: one shape, one parser, no drift between what the model is told
// and what the REST route serves.
type GraphSchemaOutput = arcadedb.Schema

func addGraphSchemaTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "graph_schema",
		Title: "Graph schema",
		Description: "List the vertex, edge and document types in the graph with their " +
			"properties, indexes and live record counts. Call this before writing a " +
			"query so the type and property names are the real ones.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, graphSchemaHandler(tenants))
}

func graphSchemaHandler(
	tenants *tenants,
) mcp.ToolHandlerFor[GraphSchemaInput, GraphSchemaOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		_ GraphSchemaInput,
	) (*mcp.CallToolResult, GraphSchemaOutput, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, GraphSchemaOutput{}, err
		}
		schema, err := client.Schema(ctx)
		if err != nil {
			return nil, GraphSchemaOutput{}, fmt.Errorf("graph_schema: %w", err)
		}
		return nil, schema, nil
	}
}
