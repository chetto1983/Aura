package agui

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
)

func (s *Server) registerDocumentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/documents", s.handleDocumentCreate)
	mux.HandleFunc("GET /api/documents", s.handleDocumentList)
	mux.HandleFunc("GET /api/documents/{id}", s.handleDocumentGet)
	mux.HandleFunc("PATCH /api/documents/{id}", s.handleDocumentPatch)
	mux.HandleFunc("GET /api/documents/{id}/versions", s.handleDocumentVersions)
}

type documentWriteBody struct {
	Scope           documents.DocumentScope  `json:"scope"`
	Title           string                   `json:"title"`
	Tags            []string                 `json:"tags"`
	Metadata        map[string]any           `json:"metadata"`
	ActiveVersionID string                   `json:"active_version_id"`
	Status          documents.DocumentStatus `json:"status"`
}

func (s *Server) handleDocumentCreate(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.documentAPIReady(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body documentWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	doc, err := s.documentCatalog.CreateDocument(r.Context(), documents.CreateDocumentRequest{
		IdentityID: identityID,
		Scope:      body.Scope,
		Title:      body.Title,
		Tags:       body.Tags,
		Metadata:   body.Metadata,
		Status:     body.Status,
	})
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSONStatus(w, http.StatusCreated, doc)
}

func (s *Server) handleDocumentList(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.documentAPIReady(w, r)
	if !ok {
		return
	}
	limit, ok := parseDocumentQueryInt(w, r, "limit")
	if !ok {
		return
	}
	offset, ok := parseDocumentQueryInt(w, r, "offset")
	if !ok {
		return
	}
	items, err := s.documentCatalog.ListDocuments(r.Context(), documents.ListDocumentsRequest{
		IdentityID: identityID,
		Scope:      documents.DocumentScope(r.URL.Query().Get("scope")),
		Query:      r.URL.Query().Get("q"),
		Tag:        r.URL.Query().Get("tag"),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *Server) handleDocumentGet(w http.ResponseWriter, r *http.Request) {
	detail, ok := s.getDocumentDetailForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, detail)
}

func (s *Server) handleDocumentVersions(w http.ResponseWriter, r *http.Request) {
	detail, ok := s.getDocumentDetailForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, detail.Versions)
}

func (s *Server) handleDocumentPatch(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.documentAPIReady(w, r)
	if !ok {
		return
	}
	documentID, ok := parseDocumentID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body documentWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	doc, err := s.documentCatalog.UpdateDocument(r.Context(), documents.UpdateDocumentRequest{
		IdentityID:      identityID,
		DocumentID:      documentID,
		Scope:           body.Scope,
		Title:           body.Title,
		Tags:            body.Tags,
		Metadata:        body.Metadata,
		ActiveVersionID: body.ActiveVersionID,
		Status:          body.Status,
	})
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, doc)
}

func (s *Server) getDocumentDetailForRequest(w http.ResponseWriter, r *http.Request) (documents.DocumentDetail, bool) {
	identityID, ok := s.documentAPIReady(w, r)
	if !ok {
		return documents.DocumentDetail{}, false
	}
	documentID, ok := parseDocumentID(w, r)
	if !ok {
		return documents.DocumentDetail{}, false
	}
	detail, err := s.documentCatalog.GetDocument(r.Context(), identityID, documentID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return documents.DocumentDetail{}, false
	}
	return detail, true
}

func (s *Server) documentAPIReady(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.documentCatalog == nil {
		http.Error(w, "document catalog unavailable", http.StatusServiceUnavailable)
		return "", false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return identityID, true
}

func parseDocumentID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "document not found", http.StatusNotFound)
		return "", false
	}
	return id, true
}

func parseDocumentQueryInt(w http.ResponseWriter, r *http.Request, key string) (int, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		http.Error(w, "invalid "+key, http.StatusBadRequest)
		return 0, false
	}
	if n < 0 {
		http.Error(w, key+" must be non-negative", http.StatusBadRequest)
		return 0, false
	}
	return n, true
}
