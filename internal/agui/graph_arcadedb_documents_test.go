package agui

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// documentCatalogue is a memory database that also holds indexed documents: the ingest
// sidecar writes Passage and IndexedDocument beside the memory, and no edge reaches them.
func documentCatalogue() arcadedb.Schema {
	return arcadedb.Schema{
		Vertices: []arcadedb.SchemaType{
			{Name: "IndexedDocument", Kind: "vertex"},
			{Name: "Passage", Kind: "vertex"},
			{Name: "Person", Kind: "vertex"},
		},
	}
}

func documentGraphs() map[string]arcadedb.StudioGraph {
	return map[string]arcadedb.StudioGraph{
		"`IndexedDocument`": {Vertices: []arcadedb.StudioVertex{
			{RID: "#95:0", Type: "IndexedDocument", Properties: map[string]any{"file_name": "Manuale-casa.md"}},
		}},
		"`Passage`": {Vertices: []arcadedb.StudioVertex{
			{RID: "#96:0", Type: "Passage", Properties: map[string]any{"text": "La caldaia va revisionata ogni anno."}},
		}},
		"`Person`": {Vertices: []arcadedb.StudioVertex{
			{RID: "#7:0", Type: "Person", Properties: map[string]any{"name": "Giulia"}},
		}},
	}
}

func nodeIDs(nodes []GraphNode) string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return strings.Join(ids, ",")
}

// Read in type order, the passages took the free node slots ahead of Person (prd.md §9).
func TestArcadeGraphViewOverviewLeavesDocumentsOut(t *testing.T) {
	store := &stubSchemaReader{schema: documentCatalogue(), graphs: documentGraphs()}
	view := NewArcadeGraphView(store)

	got, err := view.Query(context.Background(), GraphIntent{Op: OpOverview, UserID: "id-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if ids := nodeIDs(got.Nodes); ids != "#7:0" {
		t.Fatalf("nodes = %s, want only the Person", ids)
	}
	for _, query := range store.queries {
		if strings.Contains(query.statement, "Passage") || strings.Contains(query.statement, "IndexedDocument") {
			t.Fatalf("an unfiltered overview read a document type: %q", query.statement)
		}
	}
	schema, err := view.Schema(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if labels := strings.Join(schema.Labels, ","); labels != "IndexedDocument,Passage,Person" {
		t.Fatalf("labels = %s, want the document types still offered as filters", labels)
	}
}

func TestArcadeGraphViewOverviewShowsDocumentsWhenAsked(t *testing.T) {
	view := NewArcadeGraphView(&stubSchemaReader{schema: documentCatalogue(), graphs: documentGraphs()})

	got, err := view.Query(context.Background(), GraphIntent{
		Op: OpOverview, UserID: "id-1", Labels: []string{"Passage", "IndexedDocument"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if ids := nodeIDs(got.Nodes); ids != "#95:0,#96:0" {
		t.Fatalf("nodes = %s, want the document and its passage", ids)
	}
	if got.Nodes[0].Caption != "Manuale-casa.md" {
		t.Fatalf("document caption = %q, want its file name", got.Nodes[0].Caption)
	}
	if got.Nodes[1].Caption != "La caldaia va revisionata ogni anno." {
		t.Fatalf("passage caption = %q, want its text", got.Nodes[1].Caption)
	}
}

func TestGraphCaptionPrefersANameAndBoundsAText(t *testing.T) {
	if got := graphCaption(map[string]any{"name": "Giulia", "text": "long"}, "Entity"); got != "Giulia" {
		t.Fatalf("caption = %q, want the name before the text", got)
	}
	if got := graphCaption(map[string]any{"content": "a turn"}, "ConversationTurn"); got != "ConversationTurn" {
		t.Fatalf("caption = %q, want the type for a vertex with no naming property", got)
	}
	text := strings.Repeat("parola ", 40)
	got := graphCaption(map[string]any{"text": text}, "Passage")
	if utf8.RuneCountInString(got) != graphCaptionRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("caption = %q (%d runes), want %d runes and an ellipsis", got, utf8.RuneCountInString(got), graphCaptionRunes+1)
	}
}
