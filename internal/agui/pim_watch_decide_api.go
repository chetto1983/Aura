package agui

import "net/http"

// handleMailWatchCloudDecide records the person's consent for the hosted decision model to read an
// account's mail. An account that is not watched answers 404.
func (s *Server) handleMailWatchCloudDecide(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	var body struct {
		On *bool `json:"on"`
	}
	if !decodeMailWatchBody(w, r, &body) {
		return
	}
	if body.On == nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := service.SetCloudDecide(r.Context(), identityID, r.PathValue("id"), *body.On); err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"cloud_decide": *body.On})
}
