package agui

import (
	"context"
	"errors"

	"net/http"

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
// The callback listener lives beside the private desktop. Its matched state completes
// the same owner-scoped flow that the cockpit polls; the cockpit has no public callback.

// MCPAuthorization is the stored, per-identity authorization state a row renders.
type MCPAuthorization struct {
	// Supported reports whether this server takes an authorization flow at all. False
	// for a stdio server, for one carrying a static bearer token, and for one the
	// operator opted out of — three different reasons the cockpit must not offer a
	// button.
	Supported  bool      `json:"supported"`
	Authorized bool      `json:"authorized"`
	Expired    bool      `json:"expired,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt,omitzero"`
	// Reason explains an unsupported server in the operator's terms, so the cockpit can
	// say why instead of rendering a disabled control with no explanation.
	Reason string `json:"reason,omitempty"`
}

// ErrNoAuthorizationFlow marks a server that takes no authorization flow. The cockpit offers
// it no button, so a start or revoke that reaches it anyway is the caller's mistake.
var ErrNoAuthorizationFlow = errors.New("this MCP server takes no authorization flow")

// MCPAuthorizationProvider is the narrow seam the handlers depend on. The concrete
// implementation — grant store, flow registry, session opener and private desktop —
// is assembled at the composition root, where the pool and the runtime egress policy
// already live.
type MCPAuthorizationProvider interface {
	AuthorizationState(ctx context.Context, server string) (MCPAuthorization, error)
	StartSignIn(ctx context.Context, owner, server string, request SignInRequest) (MCPSignInResult, error)
	StartLoopbackAuthorization(ctx context.Context, owner, server string) (mcp.Flow, error)
	PasteAuthorization(owner, id, address string) (mcp.Flow, error)
	AuthorizationFlow(owner, id string) (mcp.Flow, error)
	RevokeAuthorization(ctx context.Context, server string) (bool, error)
}

// SignInRequest separates the browser's address from the OAuth loopback redirect.
type SignInRequest struct {
	Origin string
	Remote bool
}

// MCPSignInResult adds cockpit preparation and navigation to the OAuth flow snapshot.
type MCPSignInResult struct {
	mcp.Flow
	SignInURL   string
	Preparing   bool
	Via, Reason string
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
	mux.HandleFunc("POST /api/governance/mcp/authorization/flow/{id}/paste", s.handleMCPAuthorizationPaste)
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
	var body struct {
		Via string `json:"via"`
	}
	if !decodeMCPBody(w, r, &body) {
		return
	}
	var started MCPSignInResult
	var err error
	switch body.Via {
	case "paste":
		started.Flow, err = provider.StartLoopbackAuthorization(r.Context(), id, r.PathValue("name"))
		started.Via = "paste"
	case "":
		started, err = provider.StartSignIn(r.Context(), id, r.PathValue("name"), SignInRequest{Origin: requestOrigin(r), Remote: r.Header.Get("X-Aura-Remote-Ingress") == "tunnel"})
	default:
		http.Error(w, "unknown authorization method", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, mcp.RedactSecrets(err.Error()), http.StatusBadRequest)
		return
	}
	if started.Preparing {
		writeJSON(w, map[string]any{"preparing": true, "reason": started.Reason})
		return
	}
	out := flowResponse(started.Flow)
	if started.SignInURL != "" {
		out["signInUrl"] = started.SignInURL
	}
	if started.Via != "" {
		out["via"] = started.Via
	}
	if started.Reason != "" {
		out["reason"] = started.Reason
	}
	writeJSON(w, out)
}

func (s *Server) handleMCPAuthorizationPaste(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.mcpAuthProvider(w)
	if !ok {
		return
	}
	id, ok := owner(r)
	if !ok {
		http.Error(w, "no identity on this request", http.StatusUnauthorized)
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if !decodeMCPBody(w, r, &body) {
		return
	}
	flow, err := provider.PasteAuthorization(id, r.PathValue("id"), body.Address)
	if err != nil {
		switch {
		case errors.Is(err, mcp.ErrNoSuchFlow):
			http.Error(w, "no such authorization flow", http.StatusNotFound)
		case errors.Is(err, mcp.ErrPasteMismatch):
			http.Error(w, mcp.ErrPasteMismatch.Error(), http.StatusConflict)
		case errors.Is(err, mcp.ErrFlowNotWaiting):
			http.Error(w, mcp.ErrFlowNotWaiting.Error(), http.StatusConflict)
		default:
			http.Error(w, "could not complete the sign-in", http.StatusInternalServerError)
		}
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
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNoAuthorizationFlow) {
			status = http.StatusBadRequest
		}
		http.Error(w, mcp.RedactSecrets(err.Error()), status)
		return
	}
	writeJSON(w, map[string]any{"removed": removed})
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
