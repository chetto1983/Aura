package documents

import (
	"context"
	"testing"
)

func TestDeletingCatalogDelegatesDeleteThroughGraphCleanup(t *testing.T) {
	store := &fakeCatalogStore{
		detail: DocumentDetail{Document: Document{
			ID:         "10000000-0000-0000-0000-000000000001",
			IdentityID: "00000000-0000-0000-0000-000000000001",
			Metadata:   map[string]any{"search_document_id": "doc_search_1"},
			Status:     DocumentStatusReady,
		}},
	}
	graph := &recordingGraphDeactivator{}
	catalog := &DeletingCatalog{
		Catalog: &CatalogService{Store: store},
		Graph:   graph,
	}

	doc, err := catalog.DeleteDocument(context.Background(), "00000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != DocumentStatusDeleted {
		t.Fatalf("deleted doc = %#v", doc)
	}
	if graph.documentID != "doc_search_1" {
		t.Fatalf("graph deactivated %q", graph.documentID)
	}
}
