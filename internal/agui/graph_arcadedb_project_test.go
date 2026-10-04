package agui

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func studioEdgeIDs(edges []arcadedb.StudioEdge) string {
	ids := make([]string, 0, len(edges))
	for _, edge := range edges {
		ids = append(ids, edge.RID)
	}
	return strings.Join(ids, ",")
}

func studioVertexIDs(vertices []arcadedb.StudioVertex) string {
	ids := make([]string, 0, len(vertices))
	for _, vertex := range vertices {
		ids = append(ids, vertex.RID)
	}
	return strings.Join(ids, ",")
}

func edgeIDs(edges []GraphEdge) string {
	ids := make([]string, 0, len(edges))
	for _, edge := range edges {
		ids = append(ids, edge.ID)
	}
	return strings.Join(ids, ",")
}

// Each record the merge drops is followed by one it keeps, so a skip that ended the loop
// would show.
func TestMergeStudioGraphKeepsEachRecordOnce(t *testing.T) {
	graph := arcadedb.StudioGraph{
		Vertices: studioVertices("Person", "#1:0", "#1:1"),
		Edges:    []arcadedb.StudioEdge{studioEdge("#5:0", "#1:0", "#1:1"), studioEdge("#5:1", "#1:1", "#1:0")},
	}
	mergeStudioGraph(&graph, arcadedb.StudioGraph{
		Vertices: studioVertices("Person", "", "#1:1", "#1:2", "#1:2"),
		Edges: []arcadedb.StudioEdge{
			studioEdge("", "#1:0", "#1:2"), studioEdge("#5:1", "#1:1", "#1:0"),
			studioEdge("#5:2", "#1:1", "#1:2"), studioEdge("#5:2", "#1:1", "#1:2"),
		},
	})
	if got := studioVertexIDs(graph.Vertices); got != "#1:0,#1:1,#1:2" {
		t.Fatalf("vertices = %s, want each RID once and none without one", got)
	}
	if got := studioEdgeIDs(graph.Edges); got != "#5:0,#5:1,#5:2" {
		t.Fatalf("edges = %s, want each RID once and none without one", got)
	}
}

// An expansion projects what the store returned, unmerged.
func TestProjectStudioGraphDrawsOnlyWhatItCanPlace(t *testing.T) {
	raw := arcadedb.StudioGraph{
		Vertices: []arcadedb.StudioVertex{
			{Type: "Person"},
			{RID: "#1:9"},
			{RID: "#1:0", Type: "Person"},
			{RID: "#1:0", Type: "Person"},
			{RID: "#1:1", Type: "Person"},
		},
		Edges: []arcadedb.StudioEdge{
			studioEdge("", "#1:0", "#1:1"),
			studioEdge("#5:8", "#1:7", "#1:0"),
			studioEdge("#5:9", "#1:0", "#1:7"),
			studioEdge("#5:0", "#1:0", "#1:1"),
			studioEdge("#5:0", "#1:0", "#1:1"),
			studioEdge("#5:1", "#1:1", "#1:0"),
		},
	}
	got := projectStudioGraph(raw, GraphIntent{}, GraphSchema{}, 75, 200)
	if ids := nodeIDs(got.Nodes); ids != "#1:0,#1:1" {
		t.Fatalf("nodes = %s, want the typed vertices with a RID, once", ids)
	}
	if ids := edgeIDs(got.Edges); ids != "#5:0,#5:1" {
		t.Fatalf("edges = %s, want the edges with a RID and both ends drawn, once", ids)
	}
	if got.Truncated {
		t.Fatal("a projection that dropped only what it cannot draw reported a cap")
	}
}

func TestProjectStudioGraphReportsAnEdgePastTheCap(t *testing.T) {
	raw := arcadedb.StudioGraph{
		Vertices: studioVertices("Person", "#1:0", "#1:1"),
		Edges:    []arcadedb.StudioEdge{studioEdge("#5:0", "#1:0", "#1:1"), studioEdge("#5:1", "#1:1", "#1:0")},
	}
	got := projectStudioGraph(raw, GraphIntent{}, GraphSchema{}, 75, 1)
	if ids := edgeIDs(got.Edges); ids != "#5:0" || !got.Truncated {
		t.Fatalf("edges = %s, truncated = %t; want #5:0 and the cap reported", ids, got.Truncated)
	}
}
