package agui

// system_update_api.go is the cockpit's side of an appliance update. The host updater
// (deploy/aura-image-update.sh) downloads every build but restarts Aura only when an admin
// asks, when Aura has been idle, or when the deadline it sets has passed; these routes show
// its status and hand it an admin's decision through the directory both share
// (internal/hostupdate). GET is for everyone, because a member must be warned before the
// restart; the two POSTs are mounted behind identity.create in cmd/aura.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/hostupdate"
)

const stateRequested = "requested"

// SetHostUpdate wires the directory the host updater shares with this daemon. On a stack
// without an updater the directory holds no status and every route reports unmanaged.
func (s *Server) SetHostUpdate(dir string) {
	s.hostUpdateDir = dir
	s.hostUpdateNow = time.Now
}

func (s *Server) registerSystemUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system/update", s.handleSystemUpdateGet)
	mux.HandleFunc("POST /api/system/update/apply", s.handleSystemUpdateApply)
	mux.HandleFunc("POST /api/system/update/defer", s.handleSystemUpdateDefer)
}

type systemUpdateView struct {
	Managed          bool       `json:"managed"`
	State            string     `json:"state,omitempty"`
	RunningRev       string     `json:"running_rev,omitempty"`
	AvailableRev     string     `json:"available_rev,omitempty"`
	AvailableBuiltAt *time.Time `json:"available_built_at,omitempty"`
	PendingSince     *time.Time `json:"pending_since,omitempty"`
	Deadline         *time.Time `json:"deadline,omitempty"`
	DeferredUntil    *time.Time `json:"deferred_until"`
	CheckedAt        *time.Time `json:"checked_at,omitempty"`
	CanDecide        bool       `json:"can_decide"`
	Error            *string    `json:"error,omitempty"`
	LastActivityAt   *time.Time `json:"last_activity_at,omitempty"`
	LiveRuns         *int       `json:"live_runs,omitempty"`
}

func (s *Server) handleSystemUpdateGet(w http.ResponseWriter, r *http.Request) {
	status, managed, err := s.readHostUpdateStatus()
	if !managed {
		writeJSONStatus(w, http.StatusOK, systemUpdateView{})
		return
	}
	view := systemUpdateView{Managed: true, State: "unknown", CanDecide: s.callerIsAdmin(r)}
	if err != nil {
		slog.Error("system update: status unreadable", "dir", s.hostUpdateDir, "err", err)
		writeJSONStatus(w, http.StatusOK, view)
		return
	}
	view.State = status.State
	view.RunningRev = status.RunningRev
	view.AvailableRev = status.AvailableRev
	view.AvailableBuiltAt = timeOrNil(status.AvailableBuilt)
	view.PendingSince = timeOrNil(status.PendingSince)
	view.Deadline = timeOrNil(status.Deadline)
	view.DeferredUntil = timeOrNil(status.DeferredUntil)
	view.CheckedAt = timeOrNil(status.CheckedAt)
	// Between a POST and the updater's next pass the status is one decision behind: show the
	// decision already, or the modal would reopen on the admin who just made it.
	if req, ok, _ := hostupdate.ReadRequest(s.hostUpdateDir); ok && req.ID != status.HandledRequest {
		switch req.Action {
		case hostupdate.ActionApply:
			view.State = stateRequested
		case hostupdate.ActionDefer:
			view.DeferredUntil = timeOrNil(req.Until)
		}
	}
	if view.CanDecide {
		view.Error = &status.Error
		if activity, ok, err := hostupdate.ReadActivity(s.hostUpdateDir); ok && err == nil {
			view.LastActivityAt = timeOrNil(activity.LastActivityAt)
			view.LiveRuns = &activity.LiveRuns
		}
	}
	writeJSONStatus(w, http.StatusOK, view)
}

func (s *Server) handleSystemUpdateApply(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	status, managed, err := s.readHostUpdateStatus()
	if !managed || err != nil || (status.State != hostupdate.StatePending && status.State != hostupdate.StateFailed) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no update waiting to be applied"})
		return
	}
	if !s.writeHostUpdateRequest(w, hostupdate.Request{Action: hostupdate.ActionApply, By: actor}) {
		return
	}
	slog.Info("system update: apply requested", "identity_id", actor, "available_rev", status.AvailableRev)
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"state": stateRequested})
}

func (s *Server) handleSystemUpdateDefer(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var in struct {
		Until time.Time `json:"until"`
	}
	now := s.hostUpdateNow()
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !in.Until.After(now) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "until must be a future RFC 3339 time"})
		return
	}
	status, managed, err := s.readHostUpdateStatus()
	if !managed || err != nil || (status.State != hostupdate.StatePending && status.State != hostupdate.StateFailed) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no update waiting to be deferred"})
		return
	}
	until := in.Until.UTC().Truncate(time.Second)
	if !status.Deadline.IsZero() {
		if !status.Deadline.After(now) {
			writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "the deadline has passed; the update applies at the next idle moment"})
			return
		}
		if until.After(status.Deadline) {
			until = status.Deadline
		}
	}
	if !s.writeHostUpdateRequest(w, hostupdate.Request{Action: hostupdate.ActionDefer, Until: until, By: actor}) {
		return
	}
	slog.Info("system update: deferred", "identity_id", actor, "until", until)
	writeJSONStatus(w, http.StatusAccepted, map[string]time.Time{"deferred_until": until})
}

func (s *Server) readHostUpdateStatus() (hostupdate.Status, bool, error) {
	if s.hostUpdateDir == "" {
		return hostupdate.Status{}, false, nil
	}
	return hostupdate.ReadStatus(s.hostUpdateDir)
}

func (s *Server) writeHostUpdateRequest(w http.ResponseWriter, req hostupdate.Request) bool {
	id, err := hostupdate.NewRequestID()
	if err == nil {
		req.ID = id
		err = hostupdate.WriteRequest(s.hostUpdateDir, req)
	}
	if err != nil {
		slog.Error("system update: request not written", "dir", s.hostUpdateDir, "err", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "the request could not reach the host updater"})
		return false
	}
	return true
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}
