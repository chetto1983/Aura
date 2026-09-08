package agui

import (
	"net/http"
	"strings"
)

// A title is a separately available resource: generation can outlive the reply.
func (s *Server) handleConversationTitle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	conv, err := s.conv.GetForIdentity(r.Context(), id, scopedIdentityID(r.Context()))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !conv.TitleSet || strings.TrimSpace(conv.Title) == "" {
		http.Error(w, "title not ready", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"title": conv.Title})
}
