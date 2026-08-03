package documents

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "trims lowercases dedupes and sorts", in: []string{" Servo ", "servo", "G220"}, want: []string{"g220", "servo"}},
		{name: "collapses internal whitespace", in: []string{"line   automation", "LINE AUTOMATION"}, want: []string{"line automation"}},
		{name: "drops empty tags", in: []string{"", "   "}, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeTags(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NormalizeTags(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeTagsRejectsLimits(t *testing.T) {
	if _, err := NormalizeTags([]string{strings.Repeat("a", 65)}); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("long tag error = %v, want max length error", err)
	}
	many := make([]string, 33)
	for i := range many {
		many[i] = "tag-" + string(rune('a'+i))
	}
	if _, err := NormalizeTags(many); err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("too many tags error = %v, want max count error", err)
	}
}

func TestCatalogServiceCreateDocumentNormalizesTags(t *testing.T) {
	store := &fakeCatalogStore{}
	svc := &CatalogService{Store: store}

	doc, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001",
		Title:      "Servo Manual",
		Tags:       []string{" Servo ", "G220", "servo"},
		Metadata:   map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTags := []string{"g220", "servo"}
	if !reflect.DeepEqual(store.createReq.Tags, wantTags) {
		t.Fatalf("stored create tags = %#v, want %#v", store.createReq.Tags, wantTags)
	}
	if store.createReq.Scope != DocumentScopeLibrary {
		t.Fatalf("create scope = %q, want %q", store.createReq.Scope, DocumentScopeLibrary)
	}
	if store.createReq.Status != DocumentStatusDraft {
		t.Fatalf("create status = %q, want %q", store.createReq.Status, DocumentStatusDraft)
	}
	if !reflect.DeepEqual(doc.Tags, wantTags) {
		t.Fatalf("returned tags = %#v, want %#v", doc.Tags, wantTags)
	}
}

func TestPostgresCatalogStoreUsesEmptyTagArrayForNoTags(t *testing.T) {
	if got := catalogTagsArray(nil); got == nil || len(got) != 0 {
		t.Fatalf("catalogTagsArray(nil) = %#v, want empty slice", got)
	}
}

func TestPostgresCatalogStoreSoftDeletesVersionAssets(t *testing.T) {
	db := &recordingCatalogDB{}
	// The worker now hangs off catalogTx — the pair of handles one identity-scoped
	// transaction hands out — because migration 0087 requires this statement to share a
	// transaction with the document soft-delete it accompanies. The assertions below are
	// unchanged; only the channel the fake DBTX arrives through is.
	sc := catalogTx{raw: db}
	identityID := mustCatalogTestUUID(t, "00000000-0000-0000-0000-000000000001")
	documentID := mustCatalogTestUUID(t, "10000000-0000-0000-0000-000000000001")

	if err := sc.softDeleteDocumentAssets(context.Background(), identityID, documentID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(db.execSQL, "UPDATE aura.assets") || !strings.Contains(db.execSQL, "aura.document_versions") {
		t.Fatalf("asset cleanup SQL = %q", db.execSQL)
	}
	if !reflect.DeepEqual(db.args, []any{identityID, documentID}) {
		t.Fatalf("asset cleanup args = %#v", db.args)
	}
}

func TestCatalogServiceUpdateDocumentNormalizesTags(t *testing.T) {
	store := &fakeCatalogStore{}
	svc := &CatalogService{Store: store}

	_, err := svc.UpdateDocument(context.Background(), UpdateDocumentRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001",
		DocumentID: "10000000-0000-0000-0000-000000000001",
		Scope:      DocumentScopeLibrary,
		Title:      "Updated Manual",
		Tags:       []string{"Line   Automation", "line automation"},
		Metadata:   map[string]any{},
		Status:     DocumentStatusReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTags := []string{"line automation"}
	if !reflect.DeepEqual(store.updateReq.Tags, wantTags) {
		t.Fatalf("stored update tags = %#v, want %#v", store.updateReq.Tags, wantTags)
	}
}

func TestCatalogServiceListDocumentsNormalizesTagFilter(t *testing.T) {
	store := &fakeCatalogStore{}
	svc := &CatalogService{Store: store}

	_, err := svc.ListDocuments(context.Background(), ListDocumentsRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001",
		Tag:        " Servo ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.listReq.Tag != "servo" {
		t.Fatalf("list tag filter = %q, want servo", store.listReq.Tag)
	}
	if store.listReq.Limit != defaultCatalogListLimit {
		t.Fatalf("list limit = %d, want %d", store.listReq.Limit, defaultCatalogListLimit)
	}
}

func TestCatalogServiceRejectsInvalidTagsBeforeStore(t *testing.T) {
	store := &fakeCatalogStore{}
	svc := &CatalogService{Store: store}

	_, err := svc.CreateDocument(context.Background(), CreateDocumentRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001",
		Title:      "Servo Manual",
		Tags:       []string{strings.Repeat("x", 65)},
	})
	if err == nil {
		t.Fatal("CreateDocument error = nil, want invalid tag error")
	}
	if store.createCalled {
		t.Fatal("store CreateDocument called for invalid tags")
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
	if store.recordReq.DocumentStatus != DocumentStatusReady || store.recordReq.VersionStatus != "ready" {
		t.Fatalf("record statuses = %q/%q, want ready/ready", store.recordReq.DocumentStatus, store.recordReq.VersionStatus)
	}
	if store.recordReq.Metadata["search_document_id"] != "doc_search_1" || store.recordReq.Metadata["document_job_id"] != "job-1" {
		t.Fatalf("record metadata = %#v", store.recordReq.Metadata)
	}
	if record.Version.SHA1 != "sha1" || record.Version.SHA256 != "sha256" {
		t.Fatalf("record version hashes = %#v", record.Version)
	}
}

func TestCatalogServiceDeleteDocumentRequiresIdentityAndDocument(t *testing.T) {
	svc := &CatalogService{Store: &fakeCatalogStore{}}

	if _, err := svc.DeleteDocument(context.Background(), "", "doc-1"); err == nil {
		t.Fatal("expected missing identity error")
	}
	if _, err := svc.DeleteDocument(context.Background(), "identity-1", ""); err == nil {
		t.Fatal("expected missing document error")
	}
}

func TestDeleteServiceSoftDeletesAndRemovesSourceAssets(t *testing.T) {
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
	svc := &DeleteService{
		Catalog: &CatalogService{Store: store},
		Assets:  assets,
	}

	doc, err := svc.SoftDeleteDocument(context.Background(), "00000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != DocumentStatusDeleted {
		t.Fatalf("deleted doc = %#v", doc)
	}
	if store.deleteIdentityID != "00000000-0000-0000-0000-000000000001" || store.deleteDocumentID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("delete call identity=%q document=%q", store.deleteIdentityID, store.deleteDocumentID)
	}
	if assets.identityID != "00000000-0000-0000-0000-000000000001" || assets.assetID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("asset delete identity=%q asset=%q", assets.identityID, assets.assetID)
	}
}

func TestDeleteServiceToleratesCleanupErrors(t *testing.T) {
	// A real document's assets are already soft-deleted by the catalog step, so the
	// asset cleanup here returns "already deleted". That must not fail the operator's
	// delete (regression: the docs library "delete does nothing" defect where a 404
	// stuck the confirm dialog).
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
	assets := &recordingAssetDeleter{err: errors.New("asset already deleted")}
	svc := &DeleteService{Catalog: &CatalogService{Store: store}, Assets: assets}

	doc, err := svc.SoftDeleteDocument(context.Background(),
		"00000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("delete must succeed despite cleanup errors: %v", err)
	}
	if doc.Status != DocumentStatusDeleted {
		t.Fatalf("deleted doc = %#v", doc)
	}
	if assets.assetID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("asset cleanup not attempted: %q", assets.assetID)
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

type fakeCatalogStore struct {
	createCalled     bool
	createReq        CreateDocumentRequest
	updateReq        UpdateDocumentRequest
	listReq          ListDocumentsRequest
	recordReq        RecordAssetVersionRequest
	detail           DocumentDetail
	deleteIdentityID string
	deleteDocumentID string
	// getIdentityID/getDocumentID/getErr let a test assert WHICH identity a lookup
	// was scoped to, and simulate the not-found a non-owner receives. Additive:
	// zero values keep the previous always-succeeds behaviour.
	getIdentityID string
	getDocumentID string
	getErr        error
}

func (f *fakeCatalogStore) CreateDocument(_ context.Context, req CreateDocumentRequest) (Document, error) {
	f.createCalled = true
	f.createReq = req
	return Document{
		ID:         "10000000-0000-0000-0000-000000000001",
		IdentityID: req.IdentityID,
		Scope:      req.Scope,
		Title:      req.Title,
		Tags:       append([]string(nil), req.Tags...),
		Metadata:   req.Metadata,
		Status:     req.Status,
	}, nil
}

func (f *fakeCatalogStore) UpdateDocument(_ context.Context, req UpdateDocumentRequest) (Document, error) {
	f.updateReq = req
	return Document{
		ID:              req.DocumentID,
		IdentityID:      req.IdentityID,
		Scope:           req.Scope,
		Title:           req.Title,
		Tags:            append([]string(nil), req.Tags...),
		Metadata:        req.Metadata,
		ActiveVersionID: req.ActiveVersionID,
		Status:          req.Status,
	}, nil
}

func (f *fakeCatalogStore) ListDocuments(_ context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	f.listReq = req
	return nil, nil
}

func (f *fakeCatalogStore) GetDocument(_ context.Context, identityID, documentID string) (DocumentDetail, error) {
	f.getIdentityID, f.getDocumentID = identityID, documentID
	if f.getErr != nil {
		return DocumentDetail{}, f.getErr
	}
	if f.detail.Document.ID != "" {
		return f.detail, nil
	}
	return DocumentDetail{Document: Document{ID: documentID, IdentityID: identityID}}, nil
}

func (f *fakeCatalogStore) SoftDeleteDocument(_ context.Context, identityID, documentID string) (Document, error) {
	f.deleteIdentityID = identityID
	f.deleteDocumentID = documentID
	return Document{ID: documentID, IdentityID: identityID, Status: DocumentStatusDeleted}, nil
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

type recordingAssetDeleter struct {
	identityID string
	assetID    string
	err        error
}

func (a *recordingAssetDeleter) DeleteDocumentAsset(_ context.Context, identityID, assetID string) error {
	a.identityID = identityID
	a.assetID = assetID
	return a.err
}

type recordingCatalogDB struct {
	execSQL string
	args    []any
}

func (db *recordingCatalogDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.execSQL = sql
	db.args = append([]any(nil), args...)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (db *recordingCatalogDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (db *recordingCatalogDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}
