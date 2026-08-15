package documents

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeCatalogStore is the one-method CatalogStore a CatalogService now needs.
type fakeCatalogStore struct {
	recordReq RecordAssetVersionRequest
}

func TestPostgresCatalogStoreUsesEmptyTagArrayForNoTags(t *testing.T) {
	if got := catalogTagsArray(nil); got == nil || len(got) != 0 {
		t.Fatalf("catalogTagsArray(nil) = %#v, want empty slice", got)
	}
}

func TestCatalogServiceRecordAssetVersionDefaultsReadyDocument(t *testing.T) {
	store := &fakeCatalogStore{}
	svc := &CatalogService{Store: store}

	record, err := svc.RecordAssetVersion(context.Background(), RecordAssetVersionRequest{
		IdentityID:       "00000000-0000-0000-0000-000000000001",
		AssetID:          "30000000-0000-0000-0000-000000000001",
		Scope:            DocumentScopeThread,
		FileName:         " Servo Manual.pdf ",
		MIMEType:         "application/pdf",
		SizeBytes:        99,
		ObjectBucket:     "aura-assets",
		ObjectKey:        "identity/local/asset/asset-1/original",
		ObjectETag:       "etag-1",
		SearchDocumentID: "doc_search_1",
		JobID:            "job-1",
		SparseChunks:     7,
		SHA1:             "sha1",
		SHA256:           "sha256",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.recordReq.Title != "Servo Manual.pdf" {
		t.Fatalf("record title = %q, want trimmed filename", store.recordReq.Title)
	}
	if store.recordReq.DocumentStatus != DocumentStatusStored ||
		store.recordReq.VersionStatus != DocumentVersionStatusStored {
		t.Fatalf("record statuses = %q/%q, want stored/stored",
			store.recordReq.DocumentStatus, store.recordReq.VersionStatus)
	}
	if store.recordReq.Metadata["search_document_id"] != "doc_search_1" || store.recordReq.Metadata["document_job_id"] != "job-1" {
		t.Fatalf("record metadata = %#v", store.recordReq.Metadata)
	}
	if record.Version.SHA1 != "sha1" || record.Version.SHA256 != "sha256" {
		t.Fatalf("record version hashes = %#v", record.Version)
	}
}

func TestCatalogDocumentFromSQLDecodesMetadataAndUUIDs(t *testing.T) {
	activeVersionID := mustCatalogTestUUID(t, "20000000-0000-0000-0000-000000000001")
	row := sqlc.AuraDocuments{
		ID:              mustCatalogTestUUID(t, "10000000-0000-0000-0000-000000000001"),
		IdentityID:      mustCatalogTestUUID(t, "00000000-0000-0000-0000-000000000001"),
		Scope:           "library",
		Title:           "Servo Manual",
		Tags:            []string{"servo", "g220"},
		Metadata:        []byte(`{"line":"automation"}`),
		ActiveVersionID: activeVersionID,
		Status:          "ready",
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("document ID = %q", doc.ID)
	}
	if doc.ActiveVersionID != "20000000-0000-0000-0000-000000000001" {
		t.Fatalf("active version ID = %q", doc.ActiveVersionID)
	}
	if doc.Metadata["line"] != "automation" {
		t.Fatalf("metadata = %#v", doc.Metadata)
	}
}

func TestCatalogDocumentFromSQLReturnsEmptyTags(t *testing.T) {
	row := sqlc.AuraDocuments{
		ID:         mustCatalogTestUUID(t, "10000000-0000-0000-0000-000000000001"),
		IdentityID: mustCatalogTestUUID(t, "00000000-0000-0000-0000-000000000001"),
		Scope:      "library",
		Title:      "Servo Manual",
		Metadata:   []byte(`{}`),
		Status:     "ready",
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Tags == nil || len(doc.Tags) != 0 {
		t.Fatalf("document tags = %#v, want empty slice", doc.Tags)
	}
}

func (f *fakeCatalogStore) RecordAssetVersion(_ context.Context, req RecordAssetVersionRequest) (DocumentVersionRecord, error) {
	f.recordReq = req
	return DocumentVersionRecord{
		Document: Document{
			ID:         "10000000-0000-0000-0000-000000000001",
			IdentityID: req.IdentityID,
			Scope:      req.Scope,
			Title:      req.Title,
			Metadata:   req.Metadata,
			Status:     req.DocumentStatus,
		},
		Version: DocumentVersion{
			ID:            "20000000-0000-0000-0000-000000000001",
			DocumentID:    "10000000-0000-0000-0000-000000000001",
			VersionNumber: 1,
			Status:        req.VersionStatus,
			SHA1:          req.SHA1,
			SHA256:        req.SHA256,
		},
	}, nil
}

func mustCatalogTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	id, err := pgUUID("test uuid", value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
