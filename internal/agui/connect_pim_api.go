package agui

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// connect_pim_api.go is the cockpit "Connect calendar / PIM account" admin-proxy (operator directive
// 2026-06-21: "can't setup calendar"; 2026-06-27: "configure all variable on frontend not just
// google"). It is the thin REST adapter the Governance MCP detail's calendar connect section drives,
// forwarding to the aura-pim-mcp sidecar's token-gated /admin REST API. It mirrors connect_api.go's
// outbound-HTTP shape (bounded http.Client, JSON passthrough, SanitizeString on every wire error,
// nil-check-503) — split into a sibling file so connect_api.go stays under the 600-LOC cap and the
// Bearer-injecting dial helper is colocated with the routes that need it.
//
// The routes are operator WRITE-class actions (creating an account stores the operator's own OAuth
// client or IMAP credentials; a delete drops the linked account; starting a device-code flow links
// a Microsoft/Outlook account), so the parent-mux mount (serve_webui.go) is behind
// RequireCapability(governance.write). They cover every provider the sidecar exposes — google
// (web-redirect), microsoft365/outlook.com (device-code), imap, ics, json — not just Google. The
// sidecar base URL + identity-scoped OAuth grant are wired by the daemon composition root via
// SetCalendarMCP; an unset dependency answers 503 so a stack with the sidecar off degrades
// gracefully. The access token is injected server-side as Authorization: Bearer and
// NEVER returned to the client; a transport error or a non-2xx status is passed through with a
// sanitized JSON body (the sidecar host never leaks). The standard OAuth subject is the
// sole tenant selector; the browser cannot choose or override it.
//
// The Google OAuth callback (GET /admin/auth/google/callback) is the one public route here: the
// aura-connect relay sends the browser to it on the cockpit's own origin, and it is forwarded to
// the sidecar without a token (see handlePIMGoogleCallback). The Microsoft/Outlook device-code
// flow is fully cockpit-driven: start → poll status → the user enters the code in a new tab.

// pimClientTimeout bounds one sidecar round-trip. The /admin API is a sibling container answering
// account CRUD + auth-start in well under a second; 8s is generous headroom while bounding a hung
// sidecar before it stalls the cockpit.
const pimClientTimeout = 8 * time.Second

// pimClient is the bounded outbound client for every PIM-admin forward. A single shared client
// reuses connections to the sibling sidecar across the connect wizard's calls.
var pimClient = &http.Client{Timeout: pimClientTimeout}

// pimDeviceStartTimeout bounds the Microsoft/Outlook device-code start round-trip. Unlike the
// sub-second account-CRUD calls, POST /admin/auth/{id}/start blocks server-side until the identity
// provider (MSAL) returns the device code, which the sidecar caps at ~30s; 35s gives that headroom
// without hanging the cockpit indefinitely.
const pimDeviceStartTimeout = 35 * time.Second

// pimDeviceClient is the longer-timeout outbound client used ONLY by the device-code start forward.
var pimDeviceClient = &http.Client{Timeout: pimDeviceStartTimeout}

// MCPAccessTokenProvider restores the current identity's OAuth access token for
// a named MCP resource server without exposing that token to the browser.
type MCPAccessTokenProvider interface {
	AccessToken(ctx context.Context, server string) (string, error)
}

// SetCalendarMCP wires the calendar resource server's /admin REST base URL, the optional pinned
// cockpit origin (AURA_WEB_PUBLIC_URL) and the same identity-scoped OAuth grant provider used by
// its MCP transport. Set by the daemon
// composition root after NewServer; until set (or when the URL is empty), the five
// /api/connect/pim/* routes answer 503 so a stack without the sidecar degrades gracefully. Kept off
// the constructor so existing NewServer callers/tests stay unchanged (the SetWhatsAppBridge
// precedent).
func (s *Server) SetCalendarMCP(baseURL, publicURL string, auth MCPAccessTokenProvider) {
	s.calendarMCPURL = baseURL
	s.calendarPublicURL = publicURL
	s.calendarMCPAuth = auth
}

// registerConnectPIMRoutes mounts the calendar/PIM connect routes on the supplied mux using Go 1.22
// method-pattern + {id} path-value routing — SPECIFIC method+path siblings under the /api/ carve-out,
// never a bare /api/. Called from Mux next to registerConnectRoutes. The parent-mux mount behind
// RequireCapability(governance.write) lives in cmd/aura/serve_webui.go (each route MUST be gated
// there — an unmounted route would be ungated), except the Google callback, which is public by
// design, and the provider-app PUT, which only an admin (identity.create) may reach. The auth/*
// trio drives the Microsoft/Outlook device-code flow; account/{id}/status carries `linked`, which
// the wizard polls to close the Google panel once consent lands.
func (s *Server) registerConnectPIMRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connect/pim/accounts", s.handlePIMListAccounts)
	mux.HandleFunc("POST /api/connect/pim/accounts", s.handlePIMCreateAccount)
	mux.HandleFunc("DELETE /api/connect/pim/accounts/{id}", s.handlePIMDeleteAccount)
	mux.HandleFunc("GET /api/connect/pim/accounts/{id}/status", s.handlePIMAccountStatus)
	mux.HandleFunc("GET /api/connect/pim/accounts/{id}/google/start", s.handlePIMGoogleStart)
	mux.HandleFunc("POST /api/connect/pim/accounts/{id}/logout", s.handlePIMLogout)
	mux.HandleFunc("POST /api/connect/pim/accounts/{id}/auth/start", s.handlePIMDeviceStart)
	mux.HandleFunc("GET /api/connect/pim/accounts/{id}/auth/status", s.handlePIMAuthStatus)
	mux.HandleFunc("POST /api/connect/pim/accounts/{id}/auth/cancel", s.handlePIMAuthCancel)
	mux.HandleFunc("GET /api/connect/pim/providers", s.handlePIMProvidersList)
	mux.HandleFunc("PUT /api/connect/pim/providers/{provider}", s.handlePIMProviderPut)
	mux.HandleFunc("GET "+PIMGoogleCallbackPath, s.handlePIMGoogleCallback)
}

// handlePIMListAccounts serves GET /api/connect/pim/accounts: forward the sidecar
// GET /admin/accounts (no secrets are ever echoed by the sidecar) and pass through the JSON body +
// status code.
func (s *Server) handlePIMListAccounts(w http.ResponseWriter, r *http.Request) {
	s.forwardPIMJSON(w, r, http.MethodGet, "/admin/accounts", nil)
}

// handlePIMCreateAccount serves POST /api/connect/pim/accounts: forward the operator-supplied body
// (account slug + display name + provider:google + providerConfig{clientId,clientSecret}) to the
// sidecar POST /admin/accounts. The body is size-capped; the sidecar response carries NO secrets.
func (s *Server) handlePIMCreateAccount(w http.ResponseWriter, r *http.Request) {
	body, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	s.forwardPIMJSON(w, r, http.MethodPost, "/admin/accounts", body)
}

// handlePIMDeleteAccount serves DELETE /api/connect/pim/accounts/{id}: forward the sidecar
// DELETE /admin/accounts/{id}?logout=true (drops the linked Google session as well).
func (s *Server) handlePIMDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMJSON(w, r, http.MethodDelete, "/admin/accounts/"+id+"?logout=true", nil)
}

// handlePIMGoogleStart serves GET /api/connect/pim/accounts/{id}/google/start: forward the sidecar
// GET /admin/auth/{id}/google/start. The 200 body carries {authUrl,redirectUri}: the wizard opens
// authUrl, and redirectUri is the shared relay URI registered once in the Google client. returnBase
// tells the sidecar which origin the relay must send the browser back to, so the callback lands on
// the address the operator is actually using — a LAN IP, a tunnel or loopback alike.
func (s *Server) handlePIMGoogleStart(w http.ResponseWriter, r *http.Request) {
	path := "/admin/auth/" + r.PathValue("id") + "/google/start"
	if origin := s.pimReturnOrigin(r); origin != "" {
		path += "?" + url.Values{"returnBase": {origin}}.Encode()
	}
	s.forwardPIMJSON(w, r, http.MethodGet, path, nil)
}

// pimReturnOrigin applies the MCP OAuth callback's rule (mcpAuthService.callbackFor): a pinned
// AURA_WEB_PUBLIC_URL wins, otherwise the origin this cockpit request arrived on. Only the origin
// is kept, because the sidecar refuses a returnBase that carries a path.
func (s *Server) pimReturnOrigin(r *http.Request) string {
	if s.calendarPublicURL == "" {
		return cockpitOrigin(r)
	}
	pinned, err := url.Parse(strings.TrimSpace(s.calendarPublicURL))
	if err != nil || pinned.Scheme == "" || pinned.Host == "" {
		return cockpitOrigin(r)
	}
	return pinned.Scheme + "://" + pinned.Host
}

// PIMGoogleCallbackPath is where the aura-connect relay sends the browser after Google's consent.
// The daemon serves it on every origin the cockpit is reachable on and forwards it to the sidecar,
// so the callback works through Caddy, a tunnel or a direct :9080 alike.
const PIMGoogleCallbackPath = "/admin/auth/google/callback"

// handlePIMGoogleCallback forwards Google's answer to the sidecar, which exchanges the code and
// renders its own result page. It is a public route for the reason the MCP OAuth callback is
// (serve_webui.go): the browser arrives by a cross-site top-level navigation that withholds the
// SameSite session cookie. No token is attached — the sidecar exempts this one path and
// authenticates it by `state`, which must be a value it issued and has not yet consumed.
func (s *Server) handlePIMGoogleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.calendarMCPURL == "" {
		http.Error(w, "calendar connect not configured", http.StatusServiceUnavailable)
		return
	}
	target := s.calendarMCPURL + PIMGoogleCallbackPath + "?" + r.URL.RawQuery
	// The host is the configured sidecar URL; the browser contributes only the query string.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil) //nolint:gosec // G704: fixed sidecar host, see above.
	if err != nil {
		http.Error(w, "calendar sidecar request failed", http.StatusBadGateway)
		return
	}
	resp, err := pimClient.Do(req) //nolint:gosec // G704: fixed sidecar host, see above.
	if err != nil {
		http.Error(w, "calendar sidecar unreachable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	page, err := io.ReadAll(io.LimitReader(resp.Body, maxRunBodyBytes))
	if err != nil {
		http.Error(w, "calendar sidecar read failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(page)
}

// handlePIMLogout serves POST /api/connect/pim/accounts/{id}/logout: forward the sidecar
// POST /admin/accounts/{id}/logout (drops the linked session without deleting the account).
func (s *Server) handlePIMLogout(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMJSON(w, r, http.MethodPost, "/admin/accounts/"+id+"/logout", nil)
}

// handlePIMAccountStatus serves GET /api/connect/pim/accounts/{id}/status: forward the sidecar
// GET /admin/accounts/{id}/status. The body carries {accountId,displayName,provider,enabled,authFlow};
// authFlow==null means no pending flow. Drives the wizard's per-account pending/linked badge across
// every provider (no secrets are echoed).
func (s *Server) handlePIMAccountStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMJSON(w, r, http.MethodGet, "/admin/accounts/"+id+"/status", nil)
}

// handlePIMDeviceStart serves POST /api/connect/pim/accounts/{id}/auth/start: forward the sidecar
// POST /admin/auth/{id}/start — the Microsoft/Outlook device-code flow. The 200 body carries
// {userCode,verificationUrl,message,expiresIn} the wizard renders (the user opens verificationUrl
// and enters userCode). It uses the longer-timeout device client: the sidecar blocks on the IdP.
func (s *Server) handlePIMDeviceStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIM(w, r, http.MethodPost, "/admin/auth/"+id+"/start", nil, pimDeviceClient)
}

// handlePIMAuthStatus serves GET /api/connect/pim/accounts/{id}/auth/status: forward the sidecar
// GET /admin/auth/{id}/status so the wizard can poll a pending device-code flow
// (pending|awaiting_user|completed|failed|cancelled|not_found).
func (s *Server) handlePIMAuthStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMJSON(w, r, http.MethodGet, "/admin/auth/"+id+"/status", nil)
}

// handlePIMAuthCancel serves POST /api/connect/pim/accounts/{id}/auth/cancel: forward the sidecar
// POST /admin/auth/{id}/cancel to abort a pending device-code flow.
func (s *Server) handlePIMAuthCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMJSON(w, r, http.MethodPost, "/admin/auth/"+id+"/cancel", nil)
}

// readCappedBody reads the request body bounded by maxRunBodyBytes (T-12-12 DoS guard) so a hostile
// oversized body can't exhaust memory before it is forwarded. A read failure is a sanitized 400.
func readCappedBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "calendar request body too large"})
		return nil, false
	}
	return body, true
}

// forwardPIMJSON forwards method+path (relative to the sidecar base) through the short-timeout client.
func (s *Server) forwardPIMJSON(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	s.forwardPIM(w, r, method, path, body, pimClient)
}

// forwardPIM forwards method+path with the identity's OAuth access token injected server-side and
// passes the JSON response body + status code straight through. The body is size-capped and read
// into memory so a sanitized 502 can replace a partial/hostile body. The access token is NEVER
// written to the response. No retry is needed after a write: the sidecar reloads its account
// registry before answering one (measured 2026-09-23, fork commit 529a101).
func (s *Server) forwardPIM(w http.ResponseWriter, r *http.Request, method, path string, body []byte, client *http.Client) {
	resp, ok := s.dialPIM(w, r, method, path, body, client)
	if !ok {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxRunBodyBytes))
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "calendar sidecar read failed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)
}

// dialPIM nil-checks the configured sidecar URL (503 when unwired), builds + sends the bounded
// outbound request with the identity-scoped Bearer token injected, and maps a transport failure onto a
// sanitized 502 (the sidecar host/path/token never leaks). It returns the live response (the caller
// closes the body) and whether the handler may proceed — mirroring dialBridge's (value, ok) shape.
func (s *Server) dialPIM(w http.ResponseWriter, r *http.Request, method, path string, body []byte, client *http.Client) (*http.Response, bool) {
	if s.calendarMCPURL == "" {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "calendar connect not configured"})
		return nil, false
	}
	if _, ok := principalIdentityID(r); !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return nil, false
	}
	if s.calendarMCPAuth == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "calendar authorization not configured"})
		return nil, false
	}
	accessToken, err := s.calendarMCPAuth.AccessToken(r.Context(), "calendar")
	if err != nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "calendar authorization required"})
		return nil, false
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, s.calendarMCPURL+path, reader)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "calendar sidecar request failed"})
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		// The error embeds the sidecar host/URL — SanitizeString collapses any DSN/userinfo/token;
		// the generic message keeps it diagnosable without leaking the sidecar host.
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": SanitizeString("calendar sidecar unreachable")})
		return nil, false
	}
	return resp, true
}
