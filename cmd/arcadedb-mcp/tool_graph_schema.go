package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// GraphSchemaInput is deliberately empty of everything but the identity: the
// schema of the connected database is the whole answer, and taking a database
// name here would let a caller read across tenants. The schema is per tenant
// because the database is.
type GraphSchemaInput struct {
	UserIdentifier string `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
}

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
		_ *mcp.CallToolRequest,
		in GraphSchemaInput,
	) (*mcp.CallToolResult, GraphSchemaOutput, error) {
		client, err := tenants.For(ctx, in.UserIdentifier)
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
