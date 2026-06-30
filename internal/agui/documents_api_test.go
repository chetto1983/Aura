package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

const documentAPIIdentityID = "00000000-0000-0000-0000-000000000001"

func TestDocumentAPICreateUsesPrincipalAndRoundTripsTags(t *testing.T) {
	catalog := &fakeDocumentCatalog{
		createResp: documents.Document{
			ID:         "10000000-0000-0000-0000-000000000001",
			IdentityID: documentAPIIdentityID,
			Scope:      documents.DocumentScopeLibrary,
			Title:      "Servo Manual",
			Tags:       []string{"g220", "servo"},
			Metadata:   map[string]any{"source": "test"},
			Status:     documents.DocumentStatusDraft,
		},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetDocumentCatalog(catalog)

	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(`{"title":"Servo Manual","tags":[" Servo ","G220"],"metadata":{"source":"test"}}`))
	req = withPrincipal(req, documentAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if catalog.createReq.IdentityID != documentAPIIdentityID {
		t.Fatalf("create identity = %q, want bound principal", catalog.createReq.IdentityID)
	}
	if catalog.createReq.Title != "Servo Manual" || len(catalog.createReq.Tags) != 2 {
		t.Fatalf("create request = %#v", catalog.createReq)
	}
	var got documents.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != catalog.createResp.ID || strings.Join(got.Tags, ",") != "g220,servo" {
		t.Fatalf("response = %#v", got)
	}
}

func TestDocumentAPIListUsesFiltersAndPrincipal(t *testing.T) {
	catalog := &fakeDocumentCatalog{
		listResp: []documents.DocumentSummary{{
			ID:         "10000000-0000-0000-0000-000000000001",
			IdentityID: documentAPIIdentityID,
			Scope:      documents.DocumentScopeLibrary,
			Title:      "Servo Manual",
			Tags:       []string{"servo"},
			Status:     documents.DocumentStatusReady,
		}},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetDocumentCatalog(catalog)

	req := httptest.NewRequest(http.MethodGet, "/api/documents?scope=library&q=servo&tag=Servo&limit=25&offset=5", nil)
	req = withPrincipal(req, documentAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if catalog.listReq.IdentityID != documentAPIIdentityID ||
		catalog.listReq.Scope != documents.DocumentScopeLibrary ||
		catalog.listReq.Query != "servo" ||
		catalog.listReq.Tag != "Servo" ||
		catalog.listReq.Limit != 25 ||
		catalog.listReq.Offset != 5 {
		t.Fatalf("list request = %#v", catalog.listReq)
	}
	var got []documents.DocumentSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].ID != catalog.listResp[0].ID {
		t.Fatalf("response = %#v", got)
	}
}

func TestDocumentAPIDetailAndVersionsRoutes(t *testing.T) {
	detail := documents.DocumentDetail{
		Document: documents.Document{
			ID:         "10000000-0000-0000-0000-000000000001",
			IdentityID: documentAPIIdentityID,
			Title:      "Servo Manual",
			Tags:       []string{"servo"},
			Status:     documents.DocumentStatusReady,
		},
		Versions: []documents.DocumentVersion{{
			ID:            "20000000-0000-0000-0000-000000000001",
			DocumentID:    "10000000-0000-0000-0000-000000000001",
			VersionNumber: 1,
			Status:        "ready",
			SHA256:        "abc",
		}},
	}
	catalog := &fakeDocumentCatalog{detailResp: detail}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetDocumentCatalog(catalog)

	req := httptest.NewRequest(http.MethodGet, "/api/documents/10000000-0000-0000-0000-000000000001", nil)
	req = withPrincipal(req, documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var gotDetail documents.DocumentDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &gotDetail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if gotDetail.Document.ID != detail.Document.ID || strings.Join(gotDetail.Document.Tags, ",") != "servo" {
		t.Fatalf("detail response = %#v", gotDetail)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/documents/10000000-0000-0000-0000-000000000001/versions", nil)
	req = withPrincipal(req, documentAPIIdentityID)
	rec = httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("versions status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var gotVersions []documents.DocumentVersion
	if err := json.Unmarshal(rec.Body.Bytes(), &gotVersions); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	if len(gotVersions) != 1 || gotVersions[0].ID != detail.Versions[0].ID {
		t.Fatalf("versions response = %#v", gotVersions)
	}
	if catalog.detailIdentityID != documentAPIIdentityID || catalog.detailDocumentID != detail.Document.ID {
		t.Fatalf("detail called identity=%q document=%q", catalog.detailIdentityID, catalog.detailDocumentID)
	}
}

func TestDocumentAPIPatchUsesPrincipalAndDocumentID(t *testing.T) {
	catalog := &fakeDocumentCatalog{
		updateResp: documents.Document{
			ID:              "10000000-0000-0000-0000-000000000001",
			IdentityID:      documentAPIIdentityID,
			Scope:           documents.DocumentScopeLibrary,
			Title:           "Updated Manual",
			Tags:            []string{"updated"},
			Metadata:        map[string]any{},
			ActiveVersionID: "20000000-0000-0000-0000-000000000001",
			Status:          documents.DocumentStatusReady,
		},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetDocumentCatalog(catalog)

	req := httptest.NewRequest(http.MethodPatch, "/api/documents/10000000-0000-0000-0000-000000000001", strings.NewReader(`{"title":"Updated Manual","scope":"library","tags":["updated"],"metadata":{},"active_version_id":"20000000-0000-0000-0000-000000000001","status":"ready"}`))
	req = withPrincipal(req, documentAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if catalog.updateReq.IdentityID != documentAPIIdentityID || catalog.updateReq.DocumentID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("update request = %#v", catalog.updateReq)
	}
	var got documents.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ActiveVersionID != "20000000-0000-0000-0000-000000000001" {
		t.Fatalf("response active version = %q", got.ActiveVersionID)
	}
}

func TestDocumentAPIRequiresServiceAndPrincipal(t *testing.T) {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired status = %d, want 503", rec.Code)
	}

	s.SetDocumentCatalog(&fakeDocumentCatalog{})
	rec = httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", rec.Code)
	}
}

type fakeDocumentCatalog struct {
	createReq CreateDocumentRequestAlias
	updateReq UpdateDocumentRequestAlias
	listReq   ListDocumentsRequestAlias

	createResp documents.Document
	updateResp documents.Document
	listResp   []documents.DocumentSummary
	detailResp documents.DocumentDetail

	detailIdentityID string
	detailDocumentID string
}

type CreateDocumentRequestAlias = documents.CreateDocumentRequest
type UpdateDocumentRequestAlias = documents.UpdateDocumentRequest
type ListDocumentsRequestAlias = documents.ListDocumentsRequest

func (f *fakeDocumentCatalog) CreateDocument(_ context.Context, req documents.CreateDocumentRequest) (documents.Document, error) {
	f.createReq = req
	return f.createResp, nil
}

func (f *fakeDocumentCatalog) UpdateDocument(_ context.Context, req documents.UpdateDocumentRequest) (documents.Document, error) {
	f.updateReq = req
	return f.updateResp, nil
}

func (f *fakeDocumentCatalog) ListDocuments(_ context.Context, req documents.ListDocumentsRequest) ([]documents.DocumentSummary, error) {
	f.listReq = req
	return f.listResp, nil
}

func (f *fakeDocumentCatalog) GetDocument(_ context.Context, identityID, documentID string) (documents.DocumentDetail, error) {
	f.detailIdentityID = identityID
	f.detailDocumentID = documentID
	return f.detailResp, nil
}
