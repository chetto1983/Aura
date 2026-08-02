package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// schemaStatement reads ArcadeDB's schema pseudo-type. `records` is the live row
// count per type, which is what makes this answer "is anything actually in
// there" as well as "what shape is it".
const schemaStatement = "SELECT name, type, records, properties, indexes FROM schema:types"

// GraphSchemaInput is deliberately empty: the schema of the connected database
// is the whole answer, and taking a database name here would let a caller read
// across tenants.
// GraphSchemaInput carries only the identity: the schema is per tenant because
// the database is.
type GraphSchemaInput struct {
	UserIdentifier string `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
}

// GraphSchemaType is one vertex, edge or document type.
type GraphSchemaType struct {
	Name       string   `json:"name" jsonschema:"the type name, e.g. Chunk"`
	Kind       string   `json:"kind" jsonschema:"vertex, edge or document"`
	Records    int64    `json:"records" jsonschema:"live row count for this type"`
	Properties []string `json:"properties,omitempty" jsonschema:"property names, sorted"`
	Indexes    []string `json:"indexes,omitempty" jsonschema:"index names covering this type"`
}

// GraphSchemaOutput is split by kind because a caller writing a traversal needs
// to know which names are legal on either side of an arrow.
type GraphSchemaOutput struct {
	Vertices  []GraphSchemaType `json:"vertices"`
	Edges     []GraphSchemaType `json:"edges"`
	Documents []GraphSchemaType `json:"documents,omitempty"`
}

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
		rows, err := client.Query(ctx, schemaStatement, nil)
		if err != nil {
			return nil, GraphSchemaOutput{}, fmt.Errorf("graph_schema: %w", err)
		}
		return nil, buildGraphSchema(rows), nil
	}
}

func buildGraphSchema(rows []map[string]any) GraphSchemaOutput {
	out := GraphSchemaOutput{
		Vertices:  []GraphSchemaType{},
		Edges:     []GraphSchemaType{},
		Documents: []GraphSchemaType{},
	}
	for _, row := range rows {
		entry := GraphSchemaType{
			Name:       stringField(row["name"]),
			Kind:       stringField(row["type"]),
			Records:    intField(row["records"]),
			Properties: propertyNames(row["properties"]),
			Indexes:    stringList(row["indexes"]),
		}
		if entry.Name == "" {
			continue
		}
		switch entry.Kind {
		case "edge":
			out.Edges = append(out.Edges, entry)
		case "document":
			out.Documents = append(out.Documents, entry)
		default:
			out.Vertices = append(out.Vertices, entry)
		}
	}
	sortByName(out.Vertices)
	sortByName(out.Edges)
	sortByName(out.Documents)
	if len(out.Documents) == 0 {
		out.Documents = nil
	}
	return out
}

func sortByName(entries []GraphSchemaType) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
}

// propertyNames accepts both shapes ArcadeDB uses for `properties`: a list of
// descriptor objects, and a bare list of names.
func propertyNames(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			names = append(names, typed)
		case map[string]any:
			if name := stringField(typed["name"]); name != "" {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return names
}

func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			out = append(out, typed)
		case map[string]any:
			if name := stringField(typed["name"]); name != "" {
				out = append(out, name)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func stringField(value any) string {
	text, _ := value.(string)
	return text
}

// intField tolerates JSON's float64 as well as the integer types a future
// decoder might hand over.
func intField(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	}
	return 0
}
