package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGraphNodesEdgesAndBrokenRefs(t *testing.T) {
	pages := map[string]*Page{
		"alpha": {
			Title:    "Alpha",
			Category: "test",
			Related:  RelatedFromSlugs([]string{"beta", "missing-related"}),
			Body:     "See [[beta]], [[missing]], and [[alpha]].",
		},
		"beta": {
			Title:    "Beta",
			Category: "test",
			Body:     "Back to [[alpha]] and [[alpha]].",
		},
		"orphan": {
			Title:    "Orphan",
			Category: "notes",
			Body:     "No links here.",
		},
	}

	graph := BuildGraph(pages)

	if len(graph.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3: %+v", len(graph.Nodes), graph.Nodes)
	}
	wantEdges := map[string]string{
		"alpha->beta": "mentions",
		"beta->alpha": "mentions",
	}
	if len(graph.Edges) != len(wantEdges) {
		t.Fatalf("edges = %d, want %d: %+v", len(graph.Edges), len(wantEdges), graph.Edges)
	}
	for _, edge := range graph.Edges {
		key := edge.Source + "->" + edge.Target
		if want, ok := wantEdges[key]; !ok {
			t.Fatalf("unexpected edge %s: %+v", key, graph.Edges)
		} else if edge.Type != want {
			t.Fatalf("edge %s type = %q, want %q", key, edge.Type, want)
		}
	}
	wantBroken := map[string]bool{
		"alpha->missing":         true,
		"alpha->missing-related": true,
	}
	if len(graph.BrokenRefs) != len(wantBroken) {
		t.Fatalf("broken refs = %d, want %d: %+v", len(graph.BrokenRefs), len(wantBroken), graph.BrokenRefs)
	}
	for _, ref := range graph.BrokenRefs {
		key := ref.Source + "->" + ref.Target
		if !wantBroken[key] {
			t.Fatalf("unexpected broken ref %s: %+v", key, graph.BrokenRefs)
		}
	}
	if got := strings.Join(graph.Orphans, ","); got != "orphan" {
		t.Fatalf("orphans = %q, want orphan", got)
	}
}

func TestBuildGraphTypedEdges(t *testing.T) {
	pages := map[string]*Page{
		"ruling": {Title: "Ruling", Body: "[[art-1218|cites]] supplies [[acme]]."},
		"art-1218": {Title: "Art 1218", Body: ""},
		"acme": {Title: "Acme", Body: ""},
	}
	graph := BuildGraph(pages)

	edgeMap := make(map[string]string)
	for _, e := range graph.Edges {
		edgeMap[e.Source+"->"+e.Target] = e.Type
	}
	if got := edgeMap["ruling->art-1218"]; got != "cites" {
		t.Fatalf("ruling->art-1218 type = %q, want cites", got)
	}
	if got := edgeMap["ruling->acme"]; got != "mentions" {
		t.Fatalf("ruling->acme type = %q, want mentions (plain [[slug]])", got)
	}
}

func TestBuildGraphEmptySlicesMarshalAsArrays(t *testing.T) {
	graph := BuildGraph(nil)
	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"nodes":[]`, `"edges":[]`, `"broken_refs":[]`, `"orphans":[]`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("graph JSON = %s, missing %s", data, want)
		}
	}
}

func TestWriteGraphFiles(t *testing.T) {
	dir := t.TempDir()
	graph := BuildGraph(map[string]*Page{
		"alpha": {Title: "Alpha", Body: "See [[beta]]."},
		"beta":  {Title: "Beta", Body: ""},
	})

	if err := WriteGraphFiles(dir, graph); err != nil {
		t.Fatalf("WriteGraphFiles: %v", err)
	}

	graphPath := filepath.Join(dir, "graph", "graph.json")
	contextPath := filepath.Join(dir, "graph", "context.md")
	if _, err := os.Stat(graphPath); err != nil {
		t.Fatalf("graph.json missing: %v", err)
	}
	context, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read context.md: %v", err)
	}
	for _, want := range []string{"# Wiki Graph Context", "- Pages: 2", "- Edges: 1", "[[alpha]] degree=1"} {
		if !strings.Contains(string(context), want) {
			t.Fatalf("context.md missing %q:\n%s", want, context)
		}
	}

	got, err := ReadGraphFile(dir)
	if err != nil {
		t.Fatalf("ReadGraphFile: %v", err)
	}
	if len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("graph file = %+v", got)
	}
}
