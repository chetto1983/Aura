package agui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func vertexType(name string, records int64) arcadedb.SchemaType {
	return arcadedb.SchemaType{Name: name, Kind: "vertex", Records: records}
}

func edgeType(name string, records int64) arcadedb.SchemaType {
	return arcadedb.SchemaType{Name: name, Kind: "edge", Records: records}
}

// typeRead is the stub key for the SELECT that reads one type.
func typeRead(name string) string {
	return "`" + name + "`"
}

func studioVertices(typeName string, rids ...string) []arcadedb.StudioVertex {
	out := make([]arcadedb.StudioVertex, 0, len(rids))
	for _, rid := range rids {
		out = append(out, arcadedb.StudioVertex{RID: rid, Type: typeName})
	}
	return out
}

func studioEdge(rid, out, in string) arcadedb.StudioEdge {
	return arcadedb.StudioEdge{RID: rid, Type: "KNOWS", Out: out, In: in}
}

func graphReads(queries []stubGraphQuery) string {
	reads := make([]string, 0, len(queries))
	for _, query := range queries {
		reads = append(reads, fmt.Sprintf("%s @%d", query.statement, query.limit))
	}
	return strings.Join(reads, "; ")
}

// The cockpit asks for 75 nodes and 200 edges (web/src/graph/graphIntent.ts). An intent with
// no caps must read the same window, and 75 must survive the clamp.
func TestArcadeGraphViewReadsTheCockpitCaps(t *testing.T) {
	cases := []struct {
		name               string
		nodeCap, edgeCap   int
		wantNode, wantEdge int
	}{
		{name: "no caps", wantNode: 75, wantEdge: 200},
		{name: "the cockpit's caps", nodeCap: 75, edgeCap: 200, wantNode: 75, wantEdge: 200},
		{name: "above the maxima", nodeCap: 999, edgeCap: 999, wantNode: 75, wantEdge: 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &stubSchemaReader{schema: arcadedb.Schema{
				Edges:    []arcadedb.SchemaType{edgeType("KNOWS", 0)},
				Vertices: []arcadedb.SchemaType{vertexType("Person", 0)},
			}}
			_, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
				Op: OpOverview, UserID: "id-1", NodeCap: tc.nodeCap, EdgeCap: tc.edgeCap,
			})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			want := fmt.Sprintf("SELECT FROM `KNOWS` LIMIT %d @%d; SELECT FROM `Person` LIMIT %d @%d",
				tc.wantEdge, tc.wantEdge, tc.wantNode, tc.wantNode)
			if got := graphReads(reader.queries); got != want {
				t.Fatalf("reads = %s, want %s", got, want)
			}
		})
	}
}

// Each edge type reads what is left of the edge budget, and no type is read once it is spent.
func TestArcadeGraphViewOverviewStopsReadingAtTheEdgeCap(t *testing.T) {
	reader := &stubSchemaReader{
		schema: arcadedb.Schema{Edges: []arcadedb.SchemaType{
			edgeType("KNOWS", 1), edgeType("LIKES", 2), edgeType("OWNS", 1),
		}},
		graphs: map[string]arcadedb.StudioGraph{
			typeRead("KNOWS"): {Edges: []arcadedb.StudioEdge{studioEdge("#5:0", "#1:0", "#1:1")}},
			typeRead("LIKES"): {Edges: []arcadedb.StudioEdge{
				studioEdge("#6:0", "#1:0", "#1:2"), studioEdge("#6:1", "#1:1", "#1:2"),
			}},
		},
	}
	got, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", EdgeCap: 3,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	want := "SELECT FROM `KNOWS` LIMIT 3 @3; SELECT FROM `LIKES` LIMIT 2 @2"
	if reads := graphReads(reader.queries); reads != want {
		t.Fatalf("reads = %s, want %s", reads, want)
	}
	if want := "SELECT FROM `KNOWS` LIMIT 3\nSELECT FROM `LIKES` LIMIT 2"; got.Query != want {
		t.Fatalf("query = %q, want the statements that ran, %q", got.Query, want)
	}
}

func TestArcadeGraphViewOverviewStopsReadingAtTheNodeCap(t *testing.T) {
	reader := &stubSchemaReader{
		schema: arcadedb.Schema{Vertices: []arcadedb.SchemaType{vertexType("Person", 2), vertexType("Place", 1)}},
		graphs: map[string]arcadedb.StudioGraph{
			typeRead("Person"): {Vertices: studioVertices("Person", "#1:0", "#1:1")},
		},
	}
	got, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", NodeCap: 2,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if reads, want := graphReads(reader.queries), "SELECT FROM `Person` LIMIT 2 @2"; reads != want {
		t.Fatalf("reads = %s, want %s", reads, want)
	}
	if want := "SELECT FROM `Person` LIMIT 2"; got.Query != want {
		t.Fatalf("query = %q, want the statement that ran, %q", got.Query, want)
	}
}

func TestArcadeGraphViewOverviewReportsWhatTheCapsLeftOut(t *testing.T) {
	cases := []struct {
		name   string
		schema arcadedb.Schema
		graphs map[string]arcadedb.StudioGraph
		want   bool
	}{
		{
			name:   "edge types that only together pass the edge cap",
			schema: arcadedb.Schema{Edges: []arcadedb.SchemaType{edgeType("KNOWS", 2), edgeType("LIKES", 2)}},
			want:   true,
		},
		{
			name:   "vertex types that only together pass the node cap",
			schema: arcadedb.Schema{Vertices: []arcadedb.SchemaType{vertexType("Person", 2), vertexType("Place", 2)}},
			want:   true,
		},
		{
			name: "a database that fills both caps exactly",
			schema: arcadedb.Schema{
				Edges:    []arcadedb.SchemaType{edgeType("KNOWS", 3)},
				Vertices: []arcadedb.SchemaType{vertexType("Person", 3)},
			},
			want: false,
		},
		{
			// The catalogue and the graph are separate reads: rows written between them are
			// read but not counted, and only the projection sees the cap pass.
			name:   "rows written after the catalogue was read",
			schema: arcadedb.Schema{Vertices: []arcadedb.SchemaType{vertexType("Person", 1), vertexType("Place", 1)}},
			graphs: map[string]arcadedb.StudioGraph{
				typeRead("Person"): {Vertices: studioVertices("Person", "#1:0", "#1:1")},
				typeRead("Place"):  {Vertices: studioVertices("Place", "#2:0", "#2:1")},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &stubSchemaReader{schema: tc.schema, graphs: tc.graphs}
			got, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
				Op: OpOverview, UserID: "id-1", NodeCap: 3, EdgeCap: 3,
			})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if got.Truncated != tc.want {
				t.Fatalf("truncated = %t, want %t (reads %s)", got.Truncated, tc.want, graphReads(reader.queries))
			}
		})
	}
}

func TestArcadeGraphViewExpandReportsAReadThatReachedACap(t *testing.T) {
	cases := []struct {
		name  string
		graph arcadedb.StudioGraph
		want  bool
	}{
		{
			name: "the edges reach the edge cap",
			graph: arcadedb.StudioGraph{
				Vertices: studioVertices("Person", "#1:0", "#1:1"),
				Edges:    []arcadedb.StudioEdge{studioEdge("#5:0", "#1:0", "#1:1"), studioEdge("#5:1", "#1:1", "#1:0")},
			},
			want: true,
		},
		{
			name: "the vertices reach the node cap",
			graph: arcadedb.StudioGraph{
				Vertices: studioVertices("Person", "#1:0", "#1:1", "#1:2"),
				Edges:    []arcadedb.StudioEdge{studioEdge("#5:0", "#1:0", "#1:1")},
			},
			want: true,
		},
		{
			name: "a read below both caps",
			graph: arcadedb.StudioGraph{
				Vertices: studioVertices("Person", "#1:0", "#1:1"),
				Edges:    []arcadedb.StudioEdge{studioEdge("#5:0", "#1:0", "#1:1")},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &stubSchemaReader{
				schema: arcadedb.Schema{Edges: []arcadedb.SchemaType{edgeType("KNOWS", 0)}},
				graph:  tc.graph,
			}
			got, err := NewArcadeGraphView(reader).Query(context.Background(), GraphIntent{
				Op: OpExpand, UserID: "id-1", NodeID: "#1:0", NodeCap: 3, EdgeCap: 2,
			})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if got.Truncated != tc.want {
				t.Fatalf("truncated = %t, want %t", got.Truncated, tc.want)
			}
			if want := "SELECT expand(bothE('KNOWS')) FROM #1:0 LIMIT 2"; got.Query != want {
				t.Fatalf("query = %q, want the statement that ran, %q", got.Query, want)
			}
		})
	}
}

func TestArcadeGraphViewRefusesBeforeReading(t *testing.T) {
	cases := []struct {
		name   string
		schema arcadedb.Schema
		intent GraphIntent
	}{
		{
			name:   "a label the database does not have",
			schema: liveCatalogue(),
			intent: GraphIntent{Op: OpOverview, Labels: []string{"Ghost"}},
		},
		{
			name:   "a relationship type the database does not have",
			schema: liveCatalogue(),
			intent: GraphIntent{Op: OpOverview, RelTypes: []string{"GHOST"}},
		},
		{
			name:   "a vertex type name that is not an identifier",
			schema: arcadedb.Schema{Vertices: []arcadedb.SchemaType{vertexType("Person` WHERE 1=1 --", 1)}},
			intent: GraphIntent{Op: OpOverview},
		},
		{
			name:   "an edge type name that is not an identifier",
			schema: arcadedb.Schema{Edges: []arcadedb.SchemaType{edgeType("KNOWS') FROM #1:0 --", 1)}},
			intent: GraphIntent{Op: OpExpand, NodeID: "#1:0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &stubSchemaReader{schema: tc.schema}
			tc.intent.UserID = "id-1"
			if _, err := NewArcadeGraphView(reader).Query(context.Background(), tc.intent); err == nil {
				t.Fatal("Query accepted it")
			}
			if len(reader.queries) != 0 {
				t.Fatalf("Query read the graph first: %s", graphReads(reader.queries))
			}
		})
	}
}

// Query checks the RID before any read. The builder checks it again because it splices the
// RID into SQL, and must not rely on its caller for that.
func TestExpandStatementRefusesARIDThatIsNotOne(t *testing.T) {
	if _, err := expandStatement("#1:0 OR 1=1", nil, 10); !errors.Is(err, errInvalidArcadeRID) {
		t.Fatalf("err = %v, want errInvalidArcadeRID", err)
	}
}
