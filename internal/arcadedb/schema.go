package arcadedb

import (
	"context"
	"sort"
)

// schemaStatement reads ArcadeDB's schema pseudo-type. `records` is the live row
// count per type, which is what makes this answer "is anything actually in
// there" as well as "what shape is it".
const schemaStatement = "SELECT name, type, records, properties, indexes FROM schema:types"

// SchemaType is one vertex, edge or document type.
//
// The `jsonschema` tags are not decoration: cmd/arcadedb-mcp aliases this type as
// its graph_schema tool output, and the MCP SDK reflects those tags into the
// tool's advertised output schema. Editing a description here changes what the
// model is told the field means.
type SchemaType struct {
	Name       string   `json:"name" jsonschema:"the type name, e.g. Chunk"`
	Kind       string   `json:"kind" jsonschema:"vertex, edge or document"`
	Records    int64    `json:"records" jsonschema:"live row count for this type"`
	Properties []string `json:"properties,omitempty" jsonschema:"property names, sorted"`
	Indexes    []string `json:"indexes,omitempty" jsonschema:"index names covering this type"`
}

// Schema is split by kind because a caller writing a traversal needs to know
// which names are legal on either side of an arrow.
type Schema struct {
	Vertices  []SchemaType `json:"vertices"`
	Edges     []SchemaType `json:"edges"`
	Documents []SchemaType `json:"documents,omitempty"`
}

// Schema reads the connected database's type catalogue. It is per database, and
// therefore per identity: a tenant credential reaches its own database and no
// other, so this answers "what is in MY memory" without a scoping clause anyone
// could forget to write.
func (c *Client) Schema(ctx context.Context) (Schema, error) {
	rows, err := c.Query(ctx, schemaStatement, nil)
	if err != nil {
		return Schema{}, err
	}
	return buildSchema(rows), nil
}

// buildSchema projects `schema:types` rows onto Schema. It tolerates the shapes
// ArcadeDB has actually been observed emitting rather than the one shape the
// manual shows — see propertyNames below.
func buildSchema(rows []map[string]any) Schema {
	out := Schema{
		Vertices:  []SchemaType{},
		Edges:     []SchemaType{},
		Documents: []SchemaType{},
	}
	for _, row := range rows {
		entry := SchemaType{
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

func sortByName(entries []SchemaType) {
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
