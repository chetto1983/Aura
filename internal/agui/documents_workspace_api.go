package agui

import (
	"context"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/docfields"
)

type documentsWorkspaceStore interface {
	Views(context.Context, string) ([]docfields.SavedView, error)
	SaveView(context.Context, string, docfields.SavedView) (docfields.SavedView, error)
	DeleteView(context.Context, string, string) error
	Content(context.Context, string, string, int, int) (docfields.ContentPage, error)
}

func (s *Server) registerDocumentWorkspaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+documentsBase+"/views", s.documentWorkspaceRoute(s.handleDocumentViews))
	mux.HandleFunc("POST "+documentsBase+"/views", s.documentWorkspaceRoute(s.handleDocumentViewSave))
	mux.HandleFunc("PUT "+documentsBase+"/views/{id}", s.documentWorkspaceRoute(s.handleDocumentViewSave))
	mux.HandleFunc("DELETE "+documentsBase+"/views/{id}", s.documentWorkspaceRoute(s.handleDocumentViewDelete))
	mux.HandleFunc("GET "+documentsBase+"/{id}/content", s.documentWorkspaceRoute(s.handleDocumentContent))
}

func (s *Server) documentWorkspaceRoute(h func(http.ResponseWriter, *http.Request, documentsWorkspaceStore, string)) http.HandlerFunc {
	return s.documentsRoute(func(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
		workspace, ok := store.(documentsWorkspaceStore)
		if !ok {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "documents_unavailable"})
			return
		}
		h(w, r, workspace, identityID)
	})
}

func (s *Server) handleDocumentViews(w http.ResponseWriter, r *http.Request, store documentsWorkspaceStore, identityID string) {
	views, err := store.Views(r.Context(), identityID)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"views": views})
}

func (s *Server) handleDocumentViewSave(w http.ResponseWriter, r *http.Request, store documentsWorkspaceStore, identityID string) {
	var view docfields.SavedView
	if !decodeDocumentsBody(w, r, &view) {
		return
	}
	view.ID = r.PathValue("id")
	saved, err := store.SaveView(r.Context(), identityID, view)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"view": saved})
}

func (s *Server) handleDocumentViewDelete(w http.ResponseWriter, r *http.Request, store documentsWorkspaceStore, identityID string) {
	if err := store.DeleteView(r.Context(), identityID, r.PathValue("id")); err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}

func (s *Server) handleDocumentContent(w http.ResponseWriter, r *http.Request, store documentsWorkspaceStore, identityID string) {
	offset, limit := 0, 25
	for name, dst := range map[string]*int{"offset": &offset, "limit": &limit} {
		if raw := r.URL.Query().Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 || (name == "limit" && value > 50) {
				writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_query"})
				return
			}
			*dst = value
		}
	}
	content, err := store.Content(r.Context(), identityID, r.PathValue("id"), offset, limit)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, content)
}
