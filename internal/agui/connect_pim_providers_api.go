package agui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/pimprovider"
)

// PIMGoogleRelayRedirectURI is the one redirect URI every install registers in its Google Web
// client. It must equal the sidecar's GoogleOAuthRelayUrl default (chetto1983/aura-pim-mcp,
// src/CalendarMcp.Core/Configuration/CalendarMcpConfiguration.cs); the relay's address is fixed
// by design, which is why a constant is enough for the admin panel.
const PIMGoogleRelayRedirectURI = "https://chetto1983.github.io/aura-connect/google/callback/"

type pimProviderApps interface {
	List(ctx context.Context) ([]pimprovider.App, error)
	Get(ctx context.Context, provider string) (pimprovider.App, error)
	Upsert(ctx context.Context, app pimprovider.App, updatedBy string) error
}

// SetPIMProviderApps wires the admin-set OAuth clients. Until it is called the provider routes
// and every managed-provider account create answer 503.
func (s *Server) SetPIMProviderApps(apps pimProviderApps) { s.pimApps = apps }

type pimProviderStatus struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
}

type pimProviderAdminStatus struct {
	pimProviderStatus
	ClientID    string `json:"clientId"`
	TenantID    string `json:"tenantId"`
	SecretSet   bool   `json:"secretSet"`
	RedirectURI string `json:"redirectUri,omitempty"`
}

var errPIMAppsUnavailable = map[string]string{"error": "provider apps unavailable"}

// handlePIMProvidersList serves GET /api/connect/pim/providers. A member learns only whether each
// managed provider is ready to connect; the client ID and tenant are for the admin who edits them.
func (s *Server) handlePIMProvidersList(w http.ResponseWriter, r *http.Request) {
	if s.pimApps == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	apps, err := s.pimApps.List(r.Context())
	if err != nil {
		slog.Error("pim providers: list failed", "err", err)
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	byProvider := make(map[string]pimprovider.App, len(apps))
	for _, app := range apps {
		byProvider[app.Provider] = app
	}
	admin := s.callerIsAdmin(r)
	views := make([]any, 0, len(pimprovider.ManagedProviders()))
	for _, provider := range pimprovider.ManagedProviders() {
		app, configured := byProvider[provider]
		views = append(views, pimProviderView(provider, app, configured, admin))
	}
	writeJSON(w, map[string]any{"providers": views})
}

// handlePIMProviderPut serves PUT /api/connect/pim/providers/{provider}. The identity.create gate
// is on the mount in cmd/aura/serve_webui.go.
func (s *Server) handlePIMProviderPut(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !pimprovider.Managed(provider) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
		return
	}
	if s.pimApps == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	body, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var req struct {
		ClientID     string `json:"clientId"`
		TenantID     string `json:"tenantId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	next := pimprovider.App{
		Provider:     provider,
		ClientID:     strings.TrimSpace(req.ClientID),
		TenantID:     strings.TrimSpace(req.TenantID),
		ClientSecret: strings.TrimSpace(req.ClientSecret),
	}
	var stored *pimprovider.App
	current, err := s.pimApps.Get(r.Context(), provider)
	switch {
	case err == nil:
		stored = &current
	case !errors.Is(err, pimprovider.ErrNotConfigured):
		slog.Error("pim providers: read before save failed", "provider", provider, "err", err)
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	if err := pimprovider.Validate(next, stored); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updatedBy, _ := principalIdentityID(r)
	if err := s.pimApps.Upsert(r.Context(), next, updatedBy); err != nil {
		slog.Error("pim providers: save failed", "provider", provider, "err", err)
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	saved := next
	saved.SecretSet = provider == pimprovider.Google
	writeJSON(w, pimProviderView(provider, saved, true, true))
}

func pimProviderView(provider string, app pimprovider.App, configured, admin bool) any {
	status := pimProviderStatus{Provider: provider, Configured: configured}
	if !admin {
		return status
	}
	view := pimProviderAdminStatus{
		pimProviderStatus: status,
		ClientID:          app.ClientID,
		TenantID:          app.TenantID,
		SecretSet:         app.SecretSet,
	}
	if provider == pimprovider.Google {
		view.RedirectURI = PIMGoogleRelayRedirectURI
	}
	return view
}

// callerIsAdmin fails closed: no identity seam, no principal or a failed read all mean "member".
func (s *Server) callerIsAdmin(r *http.Request) bool {
	id, ok := principalIdentityID(r)
	if !ok || s.idAdmin == nil {
		return false
	}
	admin, err := s.idAdmin.HasCapability(r.Context(), id, identity.CapIdentityCreate)
	if err != nil {
		slog.Warn("pim providers: admin check failed", "err", err)
		return false
	}
	return admin
}
