package agui

import (
	"net/http"

	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/docfields/rules"
)

// The operator's lists (types, counterparties, tags, each label with its matching rule) and the
// custom fields, under /api/documents.

func (s *Server) registerDocumentListRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+documentsBase+"/labels", s.documentsRoute(s.handleLabels))
	mux.HandleFunc("POST "+documentsBase+"/labels", s.documentsRoute(s.handleLabelSave))
	mux.HandleFunc("PUT "+documentsBase+"/labels/{id}", s.documentsRoute(s.handleLabelSave))
	mux.HandleFunc("DELETE "+documentsBase+"/labels/{id}", s.documentsRoute(s.handleLabelDelete))
	mux.HandleFunc("GET "+documentsBase+"/custom-fields", s.documentsRoute(s.handleCustomFields))
	mux.HandleFunc("POST "+documentsBase+"/custom-fields", s.documentsRoute(s.handleCustomFieldSave))
	mux.HandleFunc("PUT "+documentsBase+"/custom-fields/{id}", s.documentsRoute(s.handleCustomFieldSave))
	mux.HandleFunc("DELETE "+documentsBase+"/custom-fields/{id}", s.documentsRoute(s.handleCustomFieldDelete))
}

func (s *Server) handleLabels(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	labels, err := store.Labels(r.Context(), identityID, docfields.Kind(r.URL.Query().Get("kind")))
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"labels": labels})
}

// handleLabelSave creates a label (POST) or updates the one the path names (PUT): the path's id
// wins over any in the body. A label that carries a rule runs it over the whole library before
// answering, as saving a rule does (spec decision 3), and the answer says what it suggested.
func (s *Server) handleLabelSave(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	var in docfields.Label
	if !decodeDocumentsBody(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	saved, err := store.SaveLabel(r.Context(), identityID, in)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	out := map[string]any{"label": saved, "suggested": 0}
	if saved.Algorithm != "" && saved.Algorithm != rules.None {
		result, err := store.RunRule(r.Context(), identityID, saved.ID)
		if err != nil {
			writeDocumentsError(w, err)
			return
		}
		out["suggested"] = result.Suggested
	}
	writeJSON(w, out)
}

func (s *Server) handleLabelDelete(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	writeDocumentsDone(w, store.DeleteLabel(r.Context(), identityID, r.PathValue("id")))
}

func (s *Server) handleCustomFields(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	fields, err := store.CustomFields(r.Context(), identityID)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"fields": fields})
}

func (s *Server) handleCustomFieldSave(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	var in docfields.CustomField
	if !decodeDocumentsBody(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	saved, err := store.SaveCustomField(r.Context(), identityID, in)
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleCustomFieldDelete(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	writeDocumentsDone(w, store.DeleteCustomField(r.Context(), identityID, r.PathValue("id")))
}
