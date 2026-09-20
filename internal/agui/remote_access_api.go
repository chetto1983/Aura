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

// RemoteAccessStatus deliberately contains no secret-bearing settings or resource objects.
type RemoteAccessStatus struct {
	Enabled            bool       `json:"enabled"`
	Phase              string     `json:"phase"`
	PublicHostname     string     `json:"public_hostname,omitempty"`
	WARPHostname       string     `json:"warp_hostname,omitempty"`
	APITokenSet        bool       `json:"api_token_set"`
	TunnelTokenSet     bool       `json:"tunnel_token_set"`
	Connector          string     `json:"connector"`
	LastError          string     `json:"last_error,omitempty"`
	Generation         int64      `json:"generation"`
	AccountID          string     `json:"account_id,omitempty"`
	ZoneName           string     `json:"zone_name,omitempty"`
	Nameservers        []string   `json:"nameservers,omitempty"`
	LastReconciledAt   *time.Time `json:"last_reconciled_at,omitempty"`
	AcceptanceRequired bool       `json:"acceptance_required"`
}

// RemoteAccessConfiguration is write-only; it must never enter logs or response bodies.
type RemoteAccessConfiguration struct {
	Enabled     bool   `json:"enabled"`
	Generation  int64  `json:"generation"`
	AccountID   string `json:"account_id"`
	ZoneName    string `json:"zone_name"`
	PublicLabel string `json:"public_label"`
	WARPLabel   string `json:"warp_label"`
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
		"POST /api/settings/remote-access/rotate-token", "POST /api/settings/remote-access/reconcile",
		"POST /api/settings/remote-access/disable", "POST /api/settings/remote-access/accept-external",
	}
}

func (s *Server) handleRemoteAccess(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if !s.authorizeSettingWrite(w, r, actor, true) {
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
		if state.LastError != "" {
			state.LastError = "Remote access needs attention; retry or check credentials."
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
	if path == "/rotate-token" {
		writeJSONStatus(w, 409, map[string]string{"error": "manual_rotation_required", "documentation_url": "https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/"})
		return
	}
	action := strings.TrimPrefix(path, "/")
	if r.Method == http.MethodDelete {
		action = "delete"
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
	if strings.TrimSpace(body.ZoneName) == "" {
		writeJSONStatus(w, 202, map[string]string{"status": "prerequisite_required", "prerequisite": "registered_domain", "message": "Register a domain before continuing. Quick Tunnels do not support Aura's production SSE traffic."})
		return
	}
	if err := s.remoteAccess.Configure(r.Context(), body, actor); err != nil {
		writeRemoteAccessError(w, err)
		return
	}
	slog.Info("aura admin: remote access change", "action", "configure", "actor", actor)
	writeJSONStatus(w, 202, map[string]string{"status": "accepted"})
}

func writeRemoteAccessError(w http.ResponseWriter, err error) {
	code, message := 502, "remote access operation failed; check configuration or retry"
	switch {
	case errors.Is(err, remotetunnel.ErrAddressLocked):
		code, message = 409, "delete integration before changing account, domain, or hostnames"
	case errors.Is(err, remotetunnel.ErrStaleGeneration):
		code, message = 409, "configuration changed; refresh and retry"
	case errors.Is(err, remotetunnel.ErrConfiguration):
		code, message = 400, "invalid or incomplete remote access configuration"
	case errors.Is(err, remotetunnel.ErrOwnershipConflict):
		code, message = 409, "remote resource ownership conflict"
	}
	writeJSONStatus(w, code, map[string]string{"error": message})
}
