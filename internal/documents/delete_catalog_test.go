package documents

import (
	"context"
	"testing"
)

func TestDeletingCatalogDelegatesDurableDelete(t *testing.T) {
	store := &fakeCatalogStore{
		detail: DocumentDetail{Document: Document{
			ID:         "10000000-0000-0000-0000-000000000001",
			IdentityID: "00000000-0000-0000-0000-000000000001",
			Metadata:   map[string]any{"search_document_id": "doc_search_1"},
			Status:     DocumentStatusReady,
		}, Versions: []DocumentVersion{{
			ID:      "20000000-0000-0000-0000-000000000001",
			AssetID: "30000000-0000-0000-0000-000000000001",
		}}},
	}
	catalog := &DeletingCatalog{
		Catalog: &CatalogService{Store: store},
	}

	doc, err := catalog.DeleteDocument(context.Background(), "00000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != DocumentStatusDeleting {
		t.Fatalf("deleted doc = %#v", doc)
	}
	if store.deleteDocumentID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("durable delete not delegated: %q", store.deleteDocumentID)
	}
}
