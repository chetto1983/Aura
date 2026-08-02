package documents

import (
	"context"
	"testing"
)

func TestDeletingCatalogDelegatesDeleteThroughAssetCleanup(t *testing.T) {
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
	assets := &recordingAssetDeleter{}
	catalog := &DeletingCatalog{
		Catalog: &CatalogService{Store: store},
		Assets:  assets,
	}

	doc, err := catalog.DeleteDocument(context.Background(), "00000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != DocumentStatusDeleted {
		t.Fatalf("deleted doc = %#v", doc)
	}
	if assets.assetID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("asset cleanup not delegated: %q", assets.assetID)
	}
}
