package agui

// restart_api.go is the in-app daemon restart for an appliance with no monitor and no
// SSH: POST /api/admin/restart answers 202, then the daemon drains exactly as on
// SIGTERM and exits cleanly, and the container's restart policy brings it back.

import (
	"log/slog"
	"net/http"
)

// SetRestartTrigger wires the daemon's graceful-shutdown trigger. The composition root
// wires it only where a supervisor restarts a cleanly exited process; until then the
// route answers 409 and the settings list reports restart_supported=false.
func (s *Server) SetRestartTrigger(trigger func()) { s.restartTrigger = trigger }

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if s.restartTrigger == nil {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "restart_unsupported"})
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]bool{"restarting": true})
	// Under the idempotency adapter this flush is a no-op: the body is held until its
	// replay envelope is durable. The 202 still arrives, because the drain ends in
	// http.Server.Shutdown, which waits for this request to finish.
	_ = http.NewResponseController(w).Flush()
	slog.Info("agui: daemon restart requested", "identity_id", actor)
	s.restartTrigger()
}
