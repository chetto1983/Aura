package agui

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
// The Google OAuth callback (GET /admin/auth/google/callback) is token-exempt and is hit DIRECTLY
// by the user's browser at the sidecar's external base URL — it is deliberately NOT proxied here.
// The Microsoft/Outlook device-code flow, by contrast, is fully cockpit-driven: start → poll status
// → the user enters the user code at the verification URL in a new tab.

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

// SetCalendarMCP wires the calendar resource server's /admin REST base URL and the same
// identity-scoped OAuth grant provider used by its MCP transport. Set by the daemon
// composition root after NewServer; until set (or when the URL is empty), the five
// /api/connect/pim/* routes answer 503 so a stack without the sidecar degrades gracefully. Kept off
// the constructor so existing NewServer callers/tests stay unchanged (the SetWhatsAppBridge
// precedent).
func (s *Server) SetCalendarMCP(baseURL string, auth MCPAccessTokenProvider) {
	s.calendarMCPURL = baseURL
	s.calendarMCPAuth = auth
}

// registerConnectPIMRoutes mounts the calendar/PIM connect routes on the supplied mux using Go 1.22
// method-pattern + {id} path-value routing — SPECIFIC method+path siblings under the /api/ carve-out,
// never a bare /api/. Called from Mux next to registerConnectRoutes. The parent-mux mount behind
// RequireCapability(governance.write) lives in cmd/aura/serve_webui.go (each route MUST be gated
// there — an unmounted route would be ungated). The auth/* trio drives the Microsoft/Outlook
// device-code flow; account/{id}/status drives the per-account linked badge across all providers.
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

// pimStartRetryAttempts / pimStartRetryDelay bound the google/start 404-retry. The sidecar
// reloads its account config a beat after a create (reloadOnChange fires ~300ms after the
// POST /admin/accounts write), so a /google/start fired by the wizard IMMEDIATELY after it
// creates the account 404s transiently. Retrying the idempotent GET a few times (~2s budget)
// absorbs that lag; a genuinely missing account still 404s after the budget is spent.
const (
	pimStartRetryAttempts = 8
	pimStartRetryDelay    = 250 * time.Millisecond
)

// handlePIMGoogleStart serves GET /api/connect/pim/accounts/{id}/google/start: forward the sidecar
// GET /admin/auth/{id}/google/start. The 200 body carries {authUrl,redirectUri} the wizard renders
// (the user opens authUrl in a new tab and registers redirectUri in their Google Cloud client). It
// retries a transient 404 (the post-create config-reload race) within a bounded budget.
func (s *Server) handlePIMGoogleStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMRetry404(w, r, http.MethodGet, "/admin/auth/"+id+"/google/start", nil, pimClient)
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
// and enters userCode). It uses the longer-timeout device client (the sidecar blocks on the IdP)
// and the SAME transient-404 retry as google/start (the wizard fires start right after create, and
// the .NET reloadOnChange briefly hides the account). Restarting is safe — the sidecar cancels any
// in-flight flow for the account before starting a new one, so a retried 404 never double-starts.
func (s *Server) handlePIMDeviceStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMRetry404(w, r, http.MethodPost, "/admin/auth/"+id+"/start", nil, pimDeviceClient)
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

// forwardPIMJSON forwards method+path (relative to the sidecar base) with the identity's OAuth access token
// injected server-side and passes the JSON response body + status code straight through. The body
// is size-capped and read into memory so a sanitized 502 can replace a partial/hostile body. The
// access token is NEVER written to the response.
func (s *Server) forwardPIMJSON(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	resp, ok := s.dialPIM(w, r, method, path, body, pimClient)
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

// forwardPIMRetry404 forwards a request like forwardPIMJSON using the supplied client, but retries a
// transient 404 up to pimStartRetryAttempts (pimStartRetryDelay apart) before passing the response
// through. Safe only for requests whose retry is harmless: the idempotent google/start fetch and the
// device-code start (the sidecar cancels any in-flight flow before starting a new one, and a 404 means
// nothing started). A non-404 (or the final 404) is passed through verbatim; a cancelled request
// short-circuits to 504.
func (s *Server) forwardPIMRetry404(w http.ResponseWriter, r *http.Request, method, path string, body []byte, client *http.Client) {
	for attempt := range pimStartRetryAttempts {
		resp, ok := s.dialPIM(w, r, method, path, body, client)
		if !ok {
			return // dialPIM already wrote 503/502
		}
		if resp.StatusCode == http.StatusNotFound && attempt < pimStartRetryAttempts-1 {
			_ = resp.Body.Close()
			select {
			case <-r.Context().Done():
				writeJSONStatus(w, http.StatusGatewayTimeout, map[string]string{"error": "calendar sidecar timeout"})
				return
			case <-time.After(pimStartRetryDelay):
			}
			continue
		}
		out, err := io.ReadAll(io.LimitReader(resp.Body, maxRunBodyBytes))
		_ = resp.Body.Close()
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "calendar sidecar read failed"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(out)
		return
	}
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
