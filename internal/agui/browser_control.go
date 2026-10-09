package agui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/browsercontrol"
)

// browser_control.go records which viewer drives each box browser session (prd.md §12, "The
// operator takes the browser over", 2026-10-09). A viewer's input takes the hold, so the
// sign-in flows that use this view need no extra click; the cockpit's "let Aura drive"
// button releases it through the control route; and the end of the viewer's stream releases
// it too. While a session is held the MCP bridge refuses the agent's writes on it, and after
// release the agent's first element action by reference waits for a fresh snapshot.

// SetBrowserControl wires the registry the MCP bridge reads. Nil leaves the live view as it
// was before control existed.
func (s *Server) SetBrowserControl(r *browsercontrol.Registry) { s.browserControl = r }

func (s *Server) registerBrowserControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/browser/sessions/{session}/control", s.handleBrowserControl)
}

func (s *Server) holdBrowserControl(ctx context.Context, session string, viewer *browserViewer) {
	if s.browserControl == nil {
		return
	}
	identityID := scopedIdentityID(ctx)
	if !s.browserControl.HeldBy(identityID, session, viewer) {
		s.browserControl.Hold(identityID, session, viewer)
		slog.Info("browser session taken over by the operator", "session", session)
	}
}

func (s *Server) releaseBrowserControl(ctx context.Context, session string, viewer *browserViewer) {
	if s.browserControl == nil {
		return
	}
	if s.browserControl.Release(scopedIdentityID(ctx), session, viewer) {
		slog.Info("browser session released to the agent", "session", session)
	}
}

// handleBrowserControl takes or releases the caller's own viewer's hold: {"held": false}
// hands the session back to the agent, {"held": true} takes it without an input event.
func (s *Server) handleBrowserControl(w http.ResponseWriter, r *http.Request) {
	session := r.PathValue("session")
	if !s.browserSessionAllowed(r.Context(), session) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "invalid_session"})
		return
	}
	if s.browserControl == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "browser_control_unavailable"})
		return
	}
	var body struct {
		Held *bool `json:"held"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, browserInputMaxBytes)).Decode(&body); err != nil || body.Held == nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "held_required"})
		return
	}
	viewer := s.browserViewers.get(scopedIdentityID(r.Context()) + "\x00" + session)
	if viewer == nil {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no_live_view"})
		return
	}
	if *body.Held {
		s.holdBrowserControl(r.Context(), session, viewer)
	} else {
		s.releaseBrowserControl(r.Context(), session, viewer)
	}
	w.WriteHeader(http.StatusNoContent)
}
