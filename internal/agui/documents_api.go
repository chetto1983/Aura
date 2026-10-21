package agui

import (
	"context"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
)

// documents_api.go serves the Documenti view (docs/superpowers/specs/2026-10-18-document-fields-
// design.md, decision 4): the library with its fields, one document's detail and history, and the
// operator's edits. The library is the authenticated principal's own, so no route takes an
// identity, and a document is named by the id document_search returns too.

const documentsBase = "/api/documents"

// documentsBodyMaxBytes bounds a request body: a bulk selection by ids carries at most a library's
// worth of them.
const documentsBodyMaxBytes = 2 << 20

// documentsStore is the seam over *docfields.Store, declared at the consumer.
type documentsStore interface {
	List(ctx context.Context, identityID string, q docfields.ListQuery) (docfields.ListResult, error)
	Select(ctx context.Context, identityID string, q docfields.ListQuery, exclude []string) ([]string, error)
	Document(ctx context.Context, identityID, documentID string) (docfields.DocumentDetail, error)
	ApplyToDocuments(ctx context.Context, identityID string, documentIDs []string, d docfields.Diff) error
	ReviewDocuments(ctx context.Context, identityID string, documentIDs []string) error
	Labels(ctx context.Context, identityID string, kind docfields.Kind) ([]docfields.Label, error)
	SaveLabel(ctx context.Context, identityID string, in docfields.Label) (docfields.Label, error)
	DeleteLabel(ctx context.Context, identityID, id string) error
	RunRule(ctx context.Context, identityID, labelID string) (docfields.PassResult, error)
	CustomFields(ctx context.Context, identityID string) ([]docfields.CustomField, error)
	SaveCustomField(ctx context.Context, identityID string, in docfields.CustomField) (docfields.CustomField, error)
	DeleteCustomField(ctx context.Context, identityID, id string) error
}

// SetDocumentFieldsStore wires the Documenti view. Until set, its routes answer 503.
func (s *Server) SetDocumentFieldsStore(store documentsStore) { s.documents = store }

func (s *Server) registerDocumentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+documentsBase, s.documentsRoute(s.handleDocumentsList))
	mux.HandleFunc("POST "+documentsBase+"/bulk", s.documentsRoute(s.handleDocumentsBulk))
	mux.HandleFunc("GET "+documentsBase+"/{id}", s.documentsRoute(s.handleDocument))
	mux.HandleFunc("PATCH "+documentsBase+"/{id}", s.documentsRoute(s.handleDocumentPatch))
	mux.HandleFunc("POST "+documentsBase+"/{id}/review", s.documentsRoute(s.handleDocumentReview))
	s.registerDocumentListRoutes(mux)
	s.registerDocumentWorkspaceRoutes(mux)
	s.registerDocumentPreviewRoutes(mux)
}

// documentsHandler is a Documenti route with the store checked and the caller's identity resolved.
type documentsHandler func(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string)

func (s *Server) documentsRoute(h documentsHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.documents == nil {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "documents_unavailable"})
			return
		}
		h(w, r, s.documents, scopedIdentityID(r.Context()))
	}
}

// documentsFilter is the library filter, as the list's query parameters carry it and as a bulk
// selection's body does. Values are label ids: the view picks them from the operator's lists. The
// _not lists exclude, and unassigned names the fields that must hold nothing.
type documentsFilter struct {
	Type              []string                 `json:"type"`
	Counterparty      []string                 `json:"counterparty"`
	Tags              []string                 `json:"tags"`
	TypeNot           []string                 `json:"type_not"`
	CounterpartyNot   []string                 `json:"counterparty_not"`
	TagsNot           []string                 `json:"tags_not"`
	Unassigned        []string                 `json:"unassigned"`
	TagsMode          string                   `json:"tags_mode"`
	DateFrom          string                   `json:"date_from"`
	DateTo            string                   `json:"date_to"`
	Search            string                   `json:"q"`
	IncludeUnreviewed *bool                    `json:"include_unreviewed"`
	NeedsReview       bool                     `json:"needs_review"`
	SearchMode        string                   `json:"search_mode"`
	FileType          string                   `json:"file_type"`
	Inbox             bool                     `json:"inbox"`
	SourceKeys        []string                 `json:"source_key"`
	Custom            []docfields.CustomFilter `json:"custom"`
}

func (f documentsFilter) query() docfields.ListQuery {
	return docfields.ListQuery{
		Fields: arcadedb.FieldFilter{
			Types: f.Type, Counterparties: f.Counterparty, Tags: f.Tags, TagsAll: f.TagsMode == "all",
			DateFrom: f.DateFrom, DateTo: f.DateTo, IncludeUnreviewed: f.IncludeUnreviewed == nil || *f.IncludeUnreviewed,
		},
		Exclude:     arcadedb.FieldFilter{Types: f.TypeNot, Counterparties: f.CounterpartyNot, Tags: f.TagsNot},
		Unassigned:  f.Unassigned,
		Search:      f.Search,
		NeedsReview: f.NeedsReview,
		SearchMode:  f.SearchMode,
		FileType:    docfields.NormalizeFileType(f.FileType),
		Inbox:       f.Inbox,
		SourceKeys:  f.SourceKeys,
		Custom:      f.Custom,
	}
}

func (s *Server) handleDocumentsList(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	q, err := docfields.ParseListQuery(r.URL.Query())
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_query"})
		return
	}
	result, err := store.List(r.Context(), identityID, q)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDocument(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	detail, err := store.Document(r.Context(), identityID, r.PathValue("id"))
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, detail)
}

func (s *Server) handleDocumentPatch(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	var diff docfields.Diff
	if !decodeDocumentsBody(w, r, &diff) {
		return
	}
	writeDocumentsDone(w, store.ApplyToDocuments(r.Context(), identityID, []string{r.PathValue("id")}, diff))
}

func (s *Server) handleDocumentReview(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	writeDocumentsDone(w, store.ReviewDocuments(r.Context(), identityID, []string{r.PathValue("id")}))
}

// decodeDocumentsBody is the write boundary every Documenti route crosses: JSON only, and no
// field the route does not declare.
func decodeDocumentsBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := strictDecodeJSON(w, r, v, decodeOpts{maxBytes: documentsBodyMaxBytes}); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	return true
}

func writeDocumentsDone(w http.ResponseWriter, err error) {
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// writeDocumentsError answers a store error. Another identity's document is ErrNotFound, never a
// 403: it lives in another database, and the answer must not tell the two apart.
func writeDocumentsError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal"
	switch {
	case errors.Is(err, docfields.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, docfields.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	}
	writeJSONStatus(w, status, map[string]string{"error": code, "message": sanitizeErr(err)})
}
