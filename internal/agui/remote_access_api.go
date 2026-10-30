package agui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

// RemoteAccessStatus deliberately contains no secret-bearing settings or resource objects. Reason
// is a code, never upstream text; the wizard picks its step from it.
type RemoteAccessStatus struct {
	Enabled        bool     `json:"enabled"`
	Phase          string   `json:"phase"`
	PublicHostname string   `json:"public_hostname,omitempty"`
	APITokenSet    bool     `json:"api_token_set"`
	AccountID      string   `json:"account_id,omitempty"`
	AccountName    string   `json:"account_name,omitempty"`
	ZoneName       string   `json:"zone_name,omitempty"`
	PublicLabel    string   `json:"public_label,omitempty"`
	Nameservers    []string `json:"nameservers"`
	Reason         string   `json:"reason"`
	// Accepted is the phone test having passed for this address; Reachable is the tunnel serving now.
	Accepted  bool `json:"accepted"`
	Reachable bool `json:"reachable"`
	// HasResources is what makes a delete mean something; a row never accepted can hold a tunnel.
	HasResources  bool                             `json:"has_resources"`
	LastCheckedAt *time.Time                       `json:"last_checked_at,omitempty"`
	Abandoned     []remotetunnel.AbandonedResource `json:"abandoned"`
	Generation    int64                            `json:"generation"`
}

// RemoteAccessConfiguration is write-only; it must never enter logs or response bodies.
type RemoteAccessConfiguration struct {
	Enabled     bool   `json:"enabled"`
	Generation  int64  `json:"generation"`
	AccountID   string `json:"account_id"`
	ZoneName    string `json:"zone_name"`
	PublicLabel string `json:"public_label"`
	APIToken    string `json:"api_token,omitempty"`
}

// RemoteAccessEvent exposes bounded phase transitions without diagnostic payloads.
type RemoteAccessEvent struct {
	At         time.Time `json:"at"`
	Phase      string    `json:"phase"`
	Generation int64     `json:"generation"`
}

// RemoteAccessService keeps Cloudflare orchestration outside the HTTP boundary.
type RemoteAccessService interface {
	Status(context.Context) (RemoteAccessStatus, error)
	Verify(context.Context, string) ([]cloudflareapi.Account, error)
	Configure(context.Context, RemoteAccessConfiguration, string) error
	Action(context.Context, string, string, int64, string) error
	Events(context.Context) ([]RemoteAccessEvent, error)
}

// SetRemoteAccess binds the daemon-owned controller before serving requests.
func (s *Server) SetRemoteAccess(service RemoteAccessService) { s.remoteAccess = service }

func (s *Server) registerRemoteAccessRoutes(mux *http.ServeMux) {
	for _, route := range RemoteAccessRoutes() {
		mux.HandleFunc(route, s.handleRemoteAccess)
	}
}

// RemoteAccessRoutes keeps the gateway and authenticated parent mux on the same route set.
func RemoteAccessRoutes() []string {
	return []string{
		"GET /api/settings/remote-access", "PUT /api/settings/remote-access",
		"DELETE /api/settings/remote-access", "GET /api/settings/remote-access/events",
		"POST /api/settings/remote-access/token/verify", "POST /api/settings/remote-access/token/refresh",
		"POST /api/settings/remote-access/reconcile", "POST /api/settings/remote-access/abandoned/dismiss",
		"POST /api/settings/remote-access/disable", "POST /api/settings/remote-access/accept-external",
	}
}

func (s *Server) handleRemoteAccess(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if !s.authorizeSettingWrite(w, r, actor) {
		return
	}
	allowed, err := s.idAdmin.HasCapability(r.Context(), actor, identity.CapGovernanceWrite)
	if err != nil {
		writeRemoteAccessError(w, err)
		return
	}
	if !allowed {
		writeJSONStatus(w, 403, map[string]string{"error": "governance.write required"})
		return
	}
	if s.remoteAccess == nil {
		writeJSONStatus(w, 503, map[string]string{"error": "remote access unavailable"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/settings/remote-access")
	if r.Method == http.MethodGet {
		if path == "/events" {
			events, e := s.remoteAccess.Events(r.Context())
			if e != nil {
				writeRemoteAccessError(w, e)
				return
			}
			writeJSON(w, map[string]any{"events": events})
			return
		}
		state, e := s.remoteAccess.Status(r.Context())
		if e != nil {
			writeRemoteAccessError(w, e)
			return
		}
		// The cockpit iterates both lists; null would throw where [] renders nothing.
		if state.Nameservers == nil {
			state.Nameservers = []string{}
		}
		if state.Abandoned == nil {
			state.Abandoned = []remotetunnel.AbandonedResource{}
		}
		writeJSON(w, state)
		return
	}
	if r.Method == http.MethodPut {
		s.configureRemoteAccess(w, r, actor)
		return
	}
	if path == "/token/verify" {
		var body struct {
			APIToken string `json:"api_token"`
		}
		if strictDecodeJSON(w, r, &body, decodeOpts{}) != nil || strings.TrimSpace(body.APIToken) == "" {
			writeJSONStatus(w, 400, map[string]string{"error": "API token required"})
			return
		}
		accounts, e := s.remoteAccess.Verify(r.Context(), body.APIToken)
		if e != nil {
			writeRemoteAccessError(w, e)
			return
		}
		writeJSON(w, map[string]any{"accounts": accounts})
		return
	}
	var body struct {
		Hostname   string `json:"hostname"`
		Generation int64  `json:"generation"`
	}
	if strictDecodeJSON(w, r, &body, decodeOpts{allowEmpty: true}) != nil {
		writeJSONStatus(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	action := strings.TrimPrefix(path, "/")
	if r.Method == http.MethodDelete {
		action = "delete"
	}
	if action == "abandoned/dismiss" {
		action = "dismiss-abandoned"
	}
	if action == "accept-external" {
		if r.Header.Get("X-Aura-Remote-Ingress") != "tunnel" {
			writeJSONStatus(w, 403, map[string]string{"error": "open the public hostname through Cloudflare Access first"})
			return
		}
		state, e := s.remoteAccess.Status(r.Context())
		if e != nil {
			writeRemoteAccessError(w, e)
			return
		}
		if state.PublicHostname == "" || !strings.EqualFold(r.Host, state.PublicHostname) || state.Generation != body.Generation {
			writeJSONStatus(w, 409, map[string]string{"error": "public hostname or configuration generation changed"})
			return
		}
	}
	if e := s.remoteAccess.Action(r.Context(), action, body.Hostname, body.Generation, actor); e != nil {
		writeRemoteAccessError(w, e)
		return
	}
	slog.Info("aura admin: remote access change", "action", action, "actor", actor)
	writeJSONStatus(w, 202, map[string]string{"status": "accepted"})
}

func (s *Server) configureRemoteAccess(w http.ResponseWriter, r *http.Request, actor string) {
	var body RemoteAccessConfiguration
	if strictDecodeJSON(w, r, &body, decodeOpts{}) != nil {
		writeJSONStatus(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if err := s.remoteAccess.Configure(r.Context(), body, actor); err != nil {
		writeRemoteAccessError(w, err)
		return
	}
	slog.Info("aura admin: remote access change", "action", "configure", "actor", actor)
	writeJSONStatus(w, 202, map[string]string{"status": "accepted"})
}

// writeRemoteAccessError answers with a fixed sentence and the reason code; the error's own text,
// which may carry upstream detail, never reaches the body.
func writeRemoteAccessError(w http.ResponseWriter, err error) {
	reason := remotetunnel.ReasonOf(err)
	code, message := 502, "remote access operation failed; check configuration or retry"
	switch {
	case errors.Is(err, remotetunnel.ErrStaleGeneration):
		// Nothing failed: a newer save won. "" is the defined "no reason".
		code, message, reason = 409, "configuration changed; refresh and retry", ""
	case errors.Is(err, remotetunnel.ErrOwnershipConflict):
		code, message = 409, "remote resource ownership conflict"
	case errors.Is(err, remotetunnel.ErrConfiguration) || operatorInputReason(reason):
		code, message = 400, "invalid or incomplete remote access configuration"
	}
	// The wizard's unknown sentence sends the operator to these logs, and the body never carries the cause.
	if code != 409 && (reason == "" || reason == remotetunnel.ReasonUnknown) {
		slog.Warn("Remote access request failed", "code", code, "error", err)
	}
	writeJSONStatus(w, code, map[string]string{"error": message, "reason": string(reason)})
}

// operatorInputReason is a failure only the operator can fix by changing what they typed or chose.
func operatorInputReason(r remotetunnel.Reason) bool {
	switch r {
	case remotetunnel.ReasonTokenRejected, remotetunnel.ReasonNoAccounts, remotetunnel.ReasonAccountNotVisible, remotetunnel.ReasonDomainInvalid:
		return true
	}
	return false
}
