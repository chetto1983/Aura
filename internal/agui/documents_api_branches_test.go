package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

// documents_api_branches_test.go covers the document handler branches the happy-path tests
// leave uncovered: the pure query/id parsers, the documentJobID projection edge cases, and
// the store-error legs (create/patch/delete/get/list/events) that need a catalog that
// returns errors. The always-succeeding fakeDocumentCatalog cannot reach these, so a small
// error-returning catalog + event reader are injected here.

// errDocumentCatalog fails every catalog call, exercising the 400/404/500 error legs.
type errDocumentCatalog struct{ err error }

func (e errDocumentCatalog) CreateDocument(context.Context, documents.CreateDocumentRequest) (documents.Document, error) {
	return documents.Document{}, e.err
}
func (e errDocumentCatalog) UpdateDocument(context.Context, documents.UpdateDocumentRequest) (documents.Document, error) {
	return documents.Document{}, e.err
}
func (e errDocumentCatalog) ListDocuments(context.Context, documents.ListDocumentsRequest) ([]documents.DocumentSummary, error) {
	return nil, e.err
}
func (e errDocumentCatalog) GetDocument(context.Context, string, string) (documents.DocumentDetail, error) {
	return documents.DocumentDetail{}, e.err
}
func (e errDocumentCatalog) DeleteDocument(context.Context, string, string) (documents.Document, error) {
	return documents.Document{}, e.err
}

type errDocumentEvents struct{ err error }

func (e errDocumentEvents) ListByJob(context.Context, string) ([]documents.IngestionEvent, error) {
	return nil, e.err
}

func TestDocumentJobID(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want string
	}{
		{"nil metadata", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"non-string value", map[string]any{"document_job_id": 42}, ""},
		{"string value", map[string]any{"document_job_id": "job-7"}, "job-7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := documentJobID(tc.meta); got != tc.want {
				t.Fatalf("documentJobID(%v) = %q, want %q", tc.meta, got, tc.want)
			}
		})
	}
}

func TestParseDocumentQueryInt(t *testing.T) {
	newReq := func(q string) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/api/documents?"+q, nil)
	}
	t.Run("absent yields zero ok", func(t *testing.T) {
		n, ok := parseDocumentQueryInt(httptest.NewRecorder(), newReq(""), "limit")
		if !ok || n != 0 {
			t.Fatalf("absent limit = (%d,%v), want (0,true)", n, ok)
		}
	})
	t.Run("non-integer 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		_, ok := parseDocumentQueryInt(rec, newReq("limit=abc"), "limit")
		if ok || rec.Code != http.StatusBadRequest {
			t.Fatalf("non-int limit ok=%v code=%d, want (false,400)", ok, rec.Code)
		}
	})
	t.Run("negative 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		_, ok := parseDocumentQueryInt(rec, newReq("offset=-3"), "offset")
		if ok || rec.Code != http.StatusBadRequest {
			t.Fatalf("negative offset ok=%v code=%d, want (false,400)", ok, rec.Code)
		}
	})
	t.Run("valid", func(t *testing.T) {
		n, ok := parseDocumentQueryInt(httptest.NewRecorder(), newReq("limit=25"), "limit")
		if !ok || n != 25 {
			t.Fatalf("limit = (%d,%v), want (25,true)", n, ok)
		}
	})
}

func TestParseDocumentIDInvalid(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/documents/not-a-uuid", nil)
	r.SetPathValue("id", "not-a-uuid")
	if _, ok := parseDocumentID(rec, r); ok || rec.Code != http.StatusNotFound {
		t.Fatalf("invalid id ok=%v code=%d, want (false,404)", ok, rec.Code)
	}
}

func newDocServer(t *testing.T, catalog DocumentCatalogService) *Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetDocumentCatalog(catalog)
	return s
}

const docID = "10000000-0000-0000-0000-000000000001"

func TestDocumentCreateStoreErrorIs400(t *testing.T) {
	s := newDocServer(t, errDocumentCatalog{err: errAGUITest})
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(`{"title":"x"}`)), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on a create store error", rec.Code)
	}
}

func TestDocumentCreateInvalidBodyIs400(t *testing.T) {
	s := newDocServer(t, &fakeDocumentCatalog{})
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(`{bad`)), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on a malformed create body", rec.Code)
	}
}

func TestDocumentListStoreErrorIs500(t *testing.T) {
	s := newDocServer(t, errDocumentCatalog{err: errAGUITest})
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/documents", nil), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on a list store error", rec.Code)
	}
}

func TestDocumentGetStoreErrorIs404(t *testing.T) {
	s := newDocServer(t, errDocumentCatalog{err: errAGUITest})
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/documents/"+docID, nil), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 on a get store error", rec.Code)
	}
}

func TestDocumentDeleteStoreErrorIs404(t *testing.T) {
	s := newDocServer(t, errDocumentCatalog{err: errAGUITest})
	req := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/documents/"+docID, nil), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 on a delete store error", rec.Code)
	}
}

func TestDocumentPatchInvalidBodyIs400(t *testing.T) {
	s := newDocServer(t, &fakeDocumentCatalog{})
	req := withPrincipal(httptest.NewRequest(http.MethodPatch, "/api/documents/"+docID, strings.NewReader(`{bad`)), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on a malformed patch body", rec.Code)
	}
}

func TestDocumentPatchStoreErrorIs400(t *testing.T) {
	s := newDocServer(t, errDocumentCatalog{err: errAGUITest})
	req := withPrincipal(httptest.NewRequest(http.MethodPatch, "/api/documents/"+docID, strings.NewReader(`{"title":"x"}`)), documentAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on a patch store error", rec.Code)
	}
}

// TestDocumentEventsBranches covers the events handler legs: unwired event store → 503, a
// document with no job id → empty list, a non-uuid job id → empty list, and an event-store
// error → 500.
func TestDocumentEventsBranches(t *testing.T) {
	t.Run("503 when event store unwired", func(t *testing.T) {
		detail := documents.DocumentDetail{Document: documents.Document{ID: docID, IdentityID: documentAPIIdentityID, Metadata: map[string]any{"document_job_id": "30000000-0000-0000-0000-000000000001"}}}
		s := newDocServer(t, &fakeDocumentCatalog{detailResp: detail})
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/documents/"+docID+"/events", nil), documentAPIIdentityID)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 when the event store is unwired", rec.Code)
		}
	})

	t.Run("no job id yields empty list", func(t *testing.T) {
		detail := documents.DocumentDetail{Document: documents.Document{ID: docID, IdentityID: documentAPIIdentityID}}
		s := newDocServer(t, &fakeDocumentCatalog{detailResp: detail})
		s.SetDocumentEvents(&fakeDocumentEvents{})
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/documents/"+docID+"/events", nil), documentAPIIdentityID)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("no-job events = (%d,%q), want (200,[])", rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("non-uuid job id yields empty list", func(t *testing.T) {
		detail := documents.DocumentDetail{Document: documents.Document{ID: docID, IdentityID: documentAPIIdentityID, Metadata: map[string]any{"document_job_id": "not-a-uuid"}}}
		s := newDocServer(t, &fakeDocumentCatalog{detailResp: detail})
		s.SetDocumentEvents(&fakeDocumentEvents{})
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/documents/"+docID+"/events", nil), documentAPIIdentityID)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("bad-job events = (%d,%q), want (200,[])", rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("event store error is 500", func(t *testing.T) {
		detail := documents.DocumentDetail{Document: documents.Document{ID: docID, IdentityID: documentAPIIdentityID, Metadata: map[string]any{"document_job_id": "30000000-0000-0000-0000-000000000001"}}}
		s := newDocServer(t, &fakeDocumentCatalog{detailResp: detail})
		s.SetDocumentEvents(errDocumentEvents{err: errAGUITest})
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/documents/"+docID+"/events", nil), documentAPIIdentityID)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 on an event store error", rec.Code)
		}
	})
}
