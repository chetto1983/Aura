package agui

import (
	"encoding/json"
	"net/http"

	"github.com/chetto1983/aura/internal/board"
)

// board_views_api.go serves the operator's saved views of the board (consolidation item 3a,
// from PMSync's TaskSmartView): a name, the cockpit's filter object and a pin. The server
// stores filters without reading them, so a new filter is a cockpit change only.

func (s *Server) handleBoardViews(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	views, err := store.Views(r.Context(), identityID)
	if err != nil {
		writeBoardError(w, err)
		return
	}
	writeJSON(w, views)
}

// handleBoardSaveView creates the view or, under a name already used, replaces it.
func (s *Server) handleBoardSaveView(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	var body struct {
		Name    string          `json:"name"`
		Filters json.RawMessage `json:"filters"`
		Pinned  bool            `json:"pinned"`
	}
	if !decodeBoardBody(w, r, &body) {
		return
	}
	view, err := store.SaveView(r.Context(), identityID, body.Name, body.Filters, body.Pinned)
	if err != nil {
		writeBoardError(w, err)
		return
	}
	writeJSON(w, view)
}

func (s *Server) handleBoardDeleteView(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	removed, err := store.DeleteView(r.Context(), identityID, r.PathValue("id"))
	if err == nil && !removed {
		err = board.ErrNotFound
	}
	if err != nil {
		writeBoardError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}
