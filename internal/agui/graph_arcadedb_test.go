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
	graph       arcadedb.StudioGraph
	graphs      map[string]arcadedb.StudioGraph
	queryErr    error
	schemaCalls int
	queries     []stubGraphQuery
}

type stubGraphQuery struct {
	identityID string
	statement  string
	limit      int
}

func (s *stubSchemaReader) Schema(_ context.Context, identityID string) (arcadedb.Schema, error) {
	s.schemaCalls++
	s.gotID = identityID
	if s.err != nil {
		return arcadedb.Schema{}, s.err
	}
	if found, ok := s.perIdentity[identityID]; ok {
		return found, nil
	}
	return s.schema, nil
}

func (s *stubSchemaReader) QueryGraph(
	_ context.Context,
	identityID string,
	statement string,
	_ map[string]any,
	limit int,
) (arcadedb.StudioGraph, error) {
	s.queries = append(s.queries, stubGraphQuery{identityID: identityID, statement: statement, limit: limit})
	if s.queryErr != nil {
		return arcadedb.StudioGraph{}, s.queryErr
	}
	for marker, graph := range s.graphs {
		if strings.Contains(statement, marker) {
			return graph, nil
		}
	}
	return s.graph, nil
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

func TestArcadeGraphViewOverviewProjectsStudioGraph(t *testing.T) {
	reader := &stubSchemaReader{
		schema: liveCatalogue(),
		graphs: map[string]arcadedb.StudioGraph{
			"MENTIONS": {
				Vertices: []arcadedb.StudioVertex{
					{RID: "#1:0", Type: "Entity", Properties: map[string]any{"name": "Aura", "kind": "product"}, In: 1},
					{RID: "#1:1", Type: "Entity", Properties: map[string]any{"name": "ArcadeDB"}, Out: 1},
				},
				Edges: []arcadedb.StudioEdge{{RID: "#5:0", Type: "MENTIONS", Out: "#1:0", In: "#1:1", Properties: map[string]any{"predicate": "stores_in", "embedding": []any{1.0}}}},
			},
			"Chunk":  {Vertices: []arcadedb.StudioVertex{{RID: "#2:0", Type: "Chunk", Properties: map[string]any{"title": "Migration note", "embedding": []any{2.0}}}}},
			"Entity": {Vertices: []arcadedb.StudioVertex{{RID: "#1:0", Type: "Entity", Properties: map[string]any{"name": "Aura"}, In: 1}}},
		},
	}

	res, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", NodeCap: 10, EdgeCap: 10,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Nodes) != 3 || len(res.Edges) != 1 {
		t.Fatalf("overview = %d nodes / %d edges, want 3 / 1: %+v", len(res.Nodes), len(res.Edges), res)
	}
	if res.Edges[0].Source != "#1:0" || res.Edges[0].Target != "#1:1" || res.Edges[0].Caption != "stores_in" {
		t.Fatalf("edge projection = %+v", res.Edges[0])
	}
	if res.Nodes[0].Degree != 1 {
		t.Fatalf("node degree = %d, want ArcadeDB i+o", res.Nodes[0].Degree)
	}
	for _, node := range res.Nodes {
		if _, leaked := node.Props["embedding"]; leaked {
			t.Fatalf("embedding leaked into graph props: %+v", node.Props)
		}
	}
	if len(reader.queries) != 3 || reader.queries[0].identityID != "id-1" {
		t.Fatalf("tenant-scoped graph reads = %+v", reader.queries)
	}
	for _, query := range reader.queries[1:] {
		if query.limit != 10 || !strings.Contains(query.statement, "LIMIT 10") {
			t.Fatalf("vertex fill was narrowed by duplicate endpoints: %+v", reader.queries)
		}
	}
	if !strings.Contains(res.Query, "SELECT FROM") || strings.Contains(strings.ToLower(res.Query), "cypher") {
		t.Fatalf("display query is not ArcadeDB SQL: %q", res.Query)
	}
}

func TestArcadeGraphViewQueryScopesToTheIntentIdentity(t *testing.T) {
	reader := &stubSchemaReader{schema: liveCatalogue()}
	view := NewArcadeGraphView(reader)
	if _, err := view.Query(context.Background(), GraphIntent{Op: OpOverview, UserID: "id-7"}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if reader.gotID != "id-7" || len(reader.queries) == 0 || reader.queries[0].identityID != "id-7" {
		t.Fatalf("reader calls were not scoped to id-7: schema=%q queries=%+v", reader.gotID, reader.queries)
	}
}

func TestArcadeGraphViewExpandUsesValidatedRIDAndRelationshipFilter(t *testing.T) {
	reader := &stubSchemaReader{
		schema: liveCatalogue(),
		graph: arcadedb.StudioGraph{
			Vertices: []arcadedb.StudioVertex{
				{RID: "#1:0", Type: "Entity", Properties: map[string]any{"name": "Aura"}},
				{RID: "#1:1", Type: "Entity", Properties: map[string]any{"name": "ArcadeDB"}},
			},
			Edges: []arcadedb.StudioEdge{{RID: "#5:0", Type: "MENTIONS", Out: "#1:0", In: "#1:1"}},
		},
	}
	res, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpExpand, UserID: "id-1", NodeID: "#1:0", RelTypes: []string{"MENTIONS"}, NodeCap: 2, EdgeCap: 1,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Nodes) != 2 || len(res.Edges) != 1 {
		t.Fatalf("expanded graph = %+v", res)
	}
	if !res.Truncated {
		t.Fatal("an expansion that reaches its read cap must report possible truncation")
	}
	if len(reader.queries) != 1 || !strings.Contains(reader.queries[0].statement, "bothE('MENTIONS')") || !strings.Contains(reader.queries[0].statement, "FROM #1:0") {
		t.Fatalf("expand statement = %+v", reader.queries)
	}
}

func TestArcadeGraphViewClampsRequestedCapsToOperationMaxima(t *testing.T) {
	reader := &stubSchemaReader{schema: arcadedb.Schema{
		Edges:    []arcadedb.SchemaType{{Name: "MENTIONS", Kind: "edge"}},
		Vertices: []arcadedb.SchemaType{{Name: "Entity", Kind: "vertex"}},
	}}
	_, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", NodeCap: 999, EdgeCap: 999,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(reader.queries) != 2 {
		t.Fatalf("transport reads = %+v, want one edge and one vertex read", reader.queries)
	}
	if reader.queries[0].limit != maxGraphEdgeCap || reader.queries[1].limit != maxGraphNodeCap {
		t.Fatalf("transport reads = %+v, want edge/node maxima %d/%d", reader.queries, maxGraphEdgeCap, maxGraphNodeCap)
	}
}

func TestArcadeGraphViewRejectsInvalidExpandRIDBeforeAnyIO(t *testing.T) {
	reader := &stubSchemaReader{schema: liveCatalogue()}
	_, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpExpand, UserID: "id-1", NodeID: "#1:0; DELETE FROM Entity",
	})
	if err == nil {
		t.Fatal("unsafe RID was accepted")
	}
	if reader.schemaCalls != 0 || len(reader.queries) != 0 {
		t.Fatalf("unsafe RID caused I/O: schema=%d queries=%+v", reader.schemaCalls, reader.queries)
	}
}

func TestArcadeGraphViewRejectsNodeIDOnOverviewBeforeAnyIO(t *testing.T) {
	reader := &stubSchemaReader{schema: liveCatalogue()}
	_, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", NodeID: "#1:0",
	})
	if err == nil {
		t.Fatal("overview accepted an expansion-only node_id")
	}
	if reader.schemaCalls != 0 || len(reader.queries) != 0 {
		t.Fatalf("invalid overview caused I/O: schema=%d queries=%+v", reader.schemaCalls, reader.queries)
	}
}

func TestArcadeGraphViewCapsResultsAndDropsDanglingEdges(t *testing.T) {
	reader := &stubSchemaReader{
		schema: arcadedb.Schema{Edges: []arcadedb.SchemaType{{Name: "MENTIONS", Kind: "edge", Records: 3}}},
		graph: arcadedb.StudioGraph{
			Vertices: []arcadedb.StudioVertex{{RID: "#1:0", Type: "Entity"}, {RID: "#1:1", Type: "Entity"}},
			Edges:    []arcadedb.StudioEdge{{RID: "#5:0", Type: "MENTIONS", Out: "#1:0", In: "#1:1"}},
		},
	}
	res, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", NodeCap: 1, EdgeCap: 1,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Nodes) != 1 || len(res.Edges) != 0 {
		t.Fatalf("capped graph kept a dangling edge: %+v", res)
	}
	if !res.Truncated {
		t.Fatal("cap hit was not reported")
	}
}

func TestArcadeGraphViewPropagatesReadFailures(t *testing.T) {
	reader := &stubSchemaReader{err: errors.New("database mem_x is not available")}
	view := NewArcadeGraphView(reader)
	if _, err := view.Schema(context.Background(), "id-1"); err == nil {
		t.Fatal("expected the read failure to surface, not an empty schema")
	}
	if _, err := view.Query(context.Background(), GraphIntent{Op: OpOverview, UserID: "id-1"}); err == nil {
		t.Fatal("expected Query to refuse when the schema read fails")
	}
}

func TestArcadeGraphViewPropagatesGraphReadFailures(t *testing.T) {
	reader := &stubSchemaReader{schema: liveCatalogue(), queryErr: errors.New("query refused")}
	if _, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{Op: OpOverview, UserID: "id-1"}); err == nil {
		t.Fatal("expected the graph read failure to surface")
	}
}
