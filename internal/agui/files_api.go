package agui

import (
	"context"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/assets"
)

// FileBrowser lists one identity's stored objects as folders and files.
type FileBrowser interface {
	List(ctx context.Context, identityID, prefix string, limit int) (assets.BrowseResult, error)
}

// SetFileBrowser wires the object browser the file manager reads.
func (s *Server) SetFileBrowser(browser FileBrowser) { s.files = browser }

func (s *Server) registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/files", s.handleFileList)
}

// handleFileList returns one folder of the caller's own bucket.
//
// The identity comes from the authenticated principal and is never a parameter: a browser
// that took an identity from the query string would be one typo away from listing someone
// else's corpus, and the resolver below scopes the read by the owner's own credential
// rather than by a prefix this handler remembered to apply.
//
// It replaces GET /api/documents for the file manager. That route listed the catalog, which
// only ever had rows for uploaded documents -- so a document reconciled from the bucket was
// invisible to the UI no matter that it was fully indexed and answerable.
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "file browser unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			http.Error(w, "limit must be a non-negative integer", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	result, err := s.files.List(r.Context(), identityID, r.URL.Query().Get("prefix"), limit)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, result)
}
