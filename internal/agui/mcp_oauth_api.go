package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// mcp_oauth_api.go is the cockpit's half of the MCP authorization flow — the answer to
// "there is no way to authenticate a connector from the cockpit".
//
// It is start-then-poll rather than one blocking call, because the SDK's fetcher blocks
// and an HTTP handler cannot. Hermes's dashboard does exactly this
// (apps/desktop/src/lib/mcp-dashboard-oauth.ts: start, then poll status until approved or
// error, tolerating a few consecutive poll failures), and the status vocabulary here is
// its own so the two can be read side by side.
//
// The redirect lands on a route of ours rather than on the browser: the authorization
// server posts nothing, it redirects the human back, and the code arrives as a query
// parameter on a plain GET. That GET is matched to its flow by the OAuth `state`, which
// is the only value a redirect is guaranteed to carry — LibreChat keys its callback the
// same way for the same reason.

// MCPAuthorization is the stored, per-identity authorization state a row renders.
type MCPAuthorization struct {
	// Supported reports whether this server takes an authorization flow at all. False
	// for a stdio server, for one carrying a static bearer token, and for one the
	// operator opted out of — three different reasons the cockpit must not offer a
	// button.
	Supported  bool      `json:"supported"`
	Authorized bool      `json:"authorized"`
	ExpiresAt  time.Time `json:"expiresAt,omitzero"`
	// Reason explains an unsupported server in the operator's terms, so the cockpit can
	// say why instead of rendering a disabled control with no explanation.
	Reason string `json:"reason,omitempty"`
}

// MCPAuthorizationProvider is the narrow seam the handlers depend on. The concrete
// implementation — grant store, flow registry, session opener, configured callback URL —
// is assembled at the composition root, where the pool and the runtime egress policy
// already live.
type MCPAuthorizationProvider interface {
	AuthorizationState(ctx context.Context, server string) (MCPAuthorization, error)
	// StartAuthorization takes the origin the browser reaches this cockpit on, because
	// that is where the authorization server has to send the human back. It is NOT a
	// public address: the redirect is followed by the browser, never fetched by the
	// provider, so http://localhost:8080 is a perfectly valid destination.
	StartAuthorization(ctx context.Context, owner, server, origin string) (mcp.Flow, error)
	AuthorizationFlow(owner, id string) (mcp.Flow, error)
	CompleteAuthorization(state, code, iss string) (mcp.Flow, error)
	FailAuthorization(state string, reason error) (mcp.Flow, error)
	RevokeAuthorization(ctx context.Context, server string) (bool, error)
}

// SetMCPAuthorizations wires the provider. A Server without one answers every route here
// 503 rather than pretending no server needs authorizing.
func (s *Server) SetMCPAuthorizations(p MCPAuthorizationProvider) { s.mcpAuth = p }

// registerMCPAuthorizationRoutes mounts the authorization routes beside the governance
// board's. The parent-mux mounts (governance.read for the GETs, governance.write for the
// POST and DELETE) live in cmd/aura/serve_webui.go, like every other route here.
func (s *Server) registerMCPAuthorizationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/governance/mcp/{name}/authorization", s.handleMCPAuthorizationState)
	mux.HandleFunc("POST /api/governance/mcp/{name}/authorization", s.handleMCPAuthorizationStart)
	mux.HandleFunc("DELETE /api/governance/mcp/{name}/authorization", s.handleMCPAuthorizationRevoke)
	mux.HandleFunc("GET /api/governance/mcp/authorization/flow/{id}", s.handleMCPAuthorizationFlow)
	mux.HandleFunc("GET /api/governance/mcp/authorization/callback", s.handleMCPAuthorizationCallback)
}

func (s *Server) mcpAuthProvider(w http.ResponseWriter) (MCPAuthorizationProvider, bool) {
	if s.mcpAuth == nil {
		http.Error(w, "mcp authorization not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	return s.mcpAuth, true
}

// owner is the identity every flow and every grant is scoped to. An unauthenticated
// request cannot reach these routes, but reading it defensively costs nothing and makes
// the scoping explicit at the point it matters.
func owner(r *http.Request) (string, bool) {
	id := identityctx.IdentityID(r.Context())
	return id, id != ""
}

// cockpitOrigin reports the address the browser reaches this cockpit on, which is where
// the authorization server must send the human back.
//
// The Origin header first, because it is the browser's own statement of where it is —
// correct through a TLS-terminating proxy, where r.TLS is nil and a scheme guessed from
// the connection would produce an http:// redirect that a provider rejects. It is present
// on the fetch POST that starts a flow.
//
// This is not a trust decision the security of the flow rests on: a redirect URI only
// matters once a human completes a consent screen in that same browser, the route is
// behind session auth, and an operator who needs the address pinned regardless sets
// AURA_WEB_PUBLIC_URL, which takes precedence over anything read here.
func cockpitOrigin(r *http.Request) string {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && origin != "null" {
		return origin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	if r.Host == "" {
		return ""
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleMCPAuthorizationState(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mcpAuthProvider(w)
	if !ok {
		return
	}
	state, err := provider.AuthorizationState(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, mcp.RedactSecrets(err.Error()), http.StatusNotFound)
		return
	}
	writeJSON(w, state)
}

func (s *Server) handleMCPAuthorizationStart(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mcpAuthProvider(w)
	if !ok {
		return
	}
	id, ok := owner(r)
	if !ok {
		http.Error(w, "no identity on this request", http.StatusUnauthorized)
		return
	}
	flow, err := provider.StartAuthorization(r.Context(), id, r.PathValue("name"), cockpitOrigin(r))
	if err != nil {
		http.Error(w, mcp.RedactSecrets(err.Error()), http.StatusBadRequest)
		return
	}
	writeJSON(w, flowResponse(flow))
}

func (s *Server) handleMCPAuthorizationFlow(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mcpAuthProvider(w)
	if !ok {
		return
	}
	id, ok := owner(r)
	if !ok {
		http.Error(w, "no identity on this request", http.StatusUnauthorized)
		return
	}
	flow, err := provider.AuthorizationFlow(id, r.PathValue("id"))
	if err != nil {
		http.Error(w, "no such authorization flow", http.StatusNotFound)
		return
	}
	writeJSON(w, flowResponse(flow))
}

func (s *Server) handleMCPAuthorizationRevoke(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mcpAuthProvider(w)
	if !ok {
		return
	}
	removed, err := provider.RevokeAuthorization(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, mcp.RedactSecrets(err.Error()), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"removed": removed})
}

// handleMCPAuthorizationCallback receives the human coming back from the provider.
//
// It answers text/plain deliberately. The only values available to render are ones the
// authorization server chose, and the cheapest way to be certain none of them can become
// markup in the operator's own origin is to serve no markup at all.
func (s *Server) handleMCPAuthorizationCallback(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mcpAuthProvider(w)
	if !ok {
		return
	}
	query := r.URL.Query()
	state := query.Get("state")
	if state == "" {
		plain(w, http.StatusBadRequest, "This redirect carries no state, so it cannot be matched to an authorization.")
		return
	}
	if refused := query.Get("error"); refused != "" {
		// Reported through the flow so the cockpit's poll surfaces it, instead of
		// leaving a spinner running until the flow expires.
		_, _ = provider.FailAuthorization(state, errors.New(refused+": "+query.Get("error_description")))
		plain(w, http.StatusOK, "Authorization was refused. You can close this tab; the cockpit has the details.")
		return
	}
	code := query.Get("code")
	if code == "" {
		plain(w, http.StatusBadRequest, "This redirect carries no authorization code.")
		return
	}
	if _, err := provider.CompleteAuthorization(state, code, query.Get("iss")); err != nil {
		plain(w, http.StatusConflict, "This authorization was already completed or has expired. You can close this tab.")
		return
	}
	plain(w, http.StatusOK, "Aura is authorized. You can close this tab.")
}

func plain(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message+"\n")
}

// flowResponse projects a flow onto the wire. The state never leaves the process: it is
// the value that completes the authorization, so a status endpoint that returned it would
// let anyone who could read one finish somebody else's flow.
func flowResponse(flow mcp.Flow) map[string]any {
	out := map[string]any{
		"flowId":      flow.ID,
		"server":      flow.ServerName,
		"status":      flow.Status,
		"expiresAt":   flow.ExpiresAt,
		"redirectUri": flow.RedirectURI,
	}
	if flow.AuthorizationURL != "" {
		out["authorizationUrl"] = flow.AuthorizationURL
	}
	if flow.Error != "" {
		out["error"] = flow.Error
	}
	return out
}
