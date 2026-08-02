package agui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// stubSchemaReader is a scripted ArcadeSchemaReader: it hands back a canned catalogue (or
// an error) and records the identity it was asked about.
type stubSchemaReader struct {
	schema      arcadedb.Schema
	err         error
	gotID       string
	perIdentity map[string]arcadedb.Schema
}

func (s *stubSchemaReader) Schema(_ context.Context, identityID string) (arcadedb.Schema, error) {
	s.gotID = identityID
	if s.err != nil {
		return arcadedb.Schema{}, s.err
	}
	if found, ok := s.perIdentity[identityID]; ok {
		return found, nil
	}
	return s.schema, nil
}

// liveCatalogue is the shape internal/arcadedb.buildSchema produces for a memory
// database: vertex types, one edge type, one document type that is NOT a graph node.
func liveCatalogue() arcadedb.Schema {
	return arcadedb.Schema{
		Vertices: []arcadedb.SchemaType{
			{Name: "Chunk", Kind: "vertex", Records: 3221, Properties: []string{"embedding", "id", "text"}},
			{Name: "Entity", Kind: "vertex", Records: 48, Properties: []string{"name", "text"}},
		},
		Edges: []arcadedb.SchemaType{
			{Name: "MENTIONS", Kind: "edge", Records: 12, Properties: []string{"score"}},
		},
		Documents: []arcadedb.SchemaType{
			{Name: "AuditEntry", Kind: "document", Records: 7, Properties: []string{"at"}},
		},
	}
}

func TestArcadeGraphViewProjectsVerticesAndEdges(t *testing.T) {
	view := NewArcadeGraphView(&stubSchemaReader{schema: liveCatalogue()})
	got, err := view.Schema(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if strings.Join(got.Labels, ",") != "Chunk,Entity" {
		t.Fatalf("labels = %v, want the vertex types", got.Labels)
	}
	if strings.Join(got.RelTypes, ",") != "MENTIONS" {
		t.Fatalf("rel_types = %v, want the edge types", got.RelTypes)
	}
}

// A document type is a record with no place on either side of an arrow. Offering it as a
// canvas filter would advertise a node type that can never be drawn.
func TestArcadeGraphViewKeepsDocumentTypesOutOfLabels(t *testing.T) {
	view := NewArcadeGraphView(&stubSchemaReader{schema: liveCatalogue()})
	got, err := view.Schema(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	for _, label := range got.Labels {
		if label == "AuditEntry" {
			t.Fatalf("document type leaked into labels: %v", got.Labels)
		}
	}
	if got.Counts["AuditEntry"] != 7 {
		t.Fatalf("counts = %v, want the document type counted", got.Counts)
	}
}

func TestArcadeGraphViewUnionsPropertyKeysAndCounts(t *testing.T) {
	view := NewArcadeGraphView(&stubSchemaReader{schema: liveCatalogue()})
	got, err := view.Schema(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if strings.Join(got.PropertyKeys, ",") != "at,embedding,id,name,score,text" {
		t.Fatalf("property_keys = %v, want the sorted de-duplicated union", got.PropertyKeys)
	}
	if got.Counts["Chunk"] != 3221 || got.Counts["MENTIONS"] != 12 {
		t.Fatalf("counts = %v, want the live record counts", got.Counts)
	}
}

// EntityTypes has no equivalent in `schema:types` — it was a DISTINCT over Entity.type
// values, a data scan this catalogue read is not. It stays empty rather than being faked.
func TestArcadeGraphViewLeavesEntityTypesEmpty(t *testing.T) {
	view := NewArcadeGraphView(&stubSchemaReader{schema: liveCatalogue()})
	got, err := view.Schema(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(got.EntityTypes) != 0 {
		t.Fatalf("entity_types = %v, want empty", got.EntityTypes)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "entity_types") {
		t.Fatalf("entity_types should be omitted, got %s", encoded)
	}
}

// An empty database is a legitimate answer. labels/rel_types must still encode as arrays
// so the cockpit can iterate without a null check.
func TestArcadeGraphViewOnEmptyDatabaseEncodesEmptyArrays(t *testing.T) {
	view := NewArcadeGraphView(&stubSchemaReader{schema: arcadedb.Schema{}})
	got, err := view.Schema(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"labels":[]`) || !strings.Contains(string(encoded), `"rel_types":[]`) {
		t.Fatalf("empty schema did not encode as arrays: %s", encoded)
	}
	if strings.Contains(string(encoded), "counts") {
		t.Fatalf("an empty catalogue should carry no counts: %s", encoded)
	}
}

func TestArcadeGraphViewSchemaScopesToTheIdentity(t *testing.T) {
	reader := &stubSchemaReader{perIdentity: map[string]arcadedb.Schema{
		"id-a": {Vertices: []arcadedb.SchemaType{{Name: "OnlyA", Kind: "vertex"}}},
		"id-b": {Vertices: []arcadedb.SchemaType{{Name: "OnlyB", Kind: "vertex"}}},
	}}
	view := NewArcadeGraphView(reader)
	got, err := view.Schema(context.Background(), "id-b")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if reader.gotID != "id-b" {
		t.Fatalf("reader asked about %q, want id-b", reader.gotID)
	}
	if strings.Join(got.Labels, ",") != "OnlyB" {
		t.Fatalf("labels = %v, want only id-b's", got.Labels)
	}
}

// Query answers every op the same way. The empty node list is the CONSEQUENCE of a
// removed capability, so the response must carry the reason: a caller reading
// {nodes:[]} alone would record "this graph is empty", which is a different fact.
func TestArcadeGraphViewQueryIsSchemaOnlyAndSaysSo(t *testing.T) {
	reader := &stubSchemaReader{schema: liveCatalogue()}
	view := NewArcadeGraphView(reader)
	for _, op := range []string{OpSeed, OpExpand, OpSchemaOverview} {
		t.Run(op, func(t *testing.T) {
			res, err := view.Query(context.Background(), GraphIntent{Op: op, UserID: "id-1"})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(res.Nodes) != 0 || len(res.Edges) != 0 {
				t.Fatalf("expected an empty canvas, got %d nodes / %d edges", len(res.Nodes), len(res.Edges))
			}
			if len(res.Schema.Labels) == 0 {
				t.Fatalf("the live schema must still ride along: %+v", res.Schema)
			}
			if !strings.Contains(res.Query, "schema-only") || !strings.Contains(res.Query, "not implemented") {
				t.Fatalf("query field does not explain the empty canvas: %q", res.Query)
			}
		})
	}
}

func TestArcadeGraphViewQueryScopesToTheIntentIdentity(t *testing.T) {
	reader := &stubSchemaReader{schema: liveCatalogue()}
	view := NewArcadeGraphView(reader)
	if _, err := view.Query(context.Background(), GraphIntent{Op: OpSeed, UserID: "id-7"}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if reader.gotID != "id-7" {
		t.Fatalf("reader asked about %q, want the intent's identity id-7", reader.gotID)
	}
}

func TestArcadeGraphViewPropagatesReadFailures(t *testing.T) {
	reader := &stubSchemaReader{err: errors.New("database mem_x is not available")}
	view := NewArcadeGraphView(reader)
	if _, err := view.Schema(context.Background(), "id-1"); err == nil {
		t.Fatal("expected the read failure to surface, not an empty schema")
	}
	if _, err := view.Query(context.Background(), GraphIntent{Op: OpSeed, UserID: "id-1"}); err == nil {
		t.Fatal("expected Query to refuse when the schema read fails")
	}
}
