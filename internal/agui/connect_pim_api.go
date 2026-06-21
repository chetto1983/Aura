package agui

import (
	"bytes"
	"io"
	"net/http"
	"time"
)

// connect_pim_api.go is the cockpit "Connect Google Calendar" admin-proxy (operator directive
// 2026-06-21: "can't setup calendar"). It is the thin REST adapter the Governance MCP detail's
// calendar connect section drives, forwarding to the aura-pim-mcp sidecar's token-gated /admin
// REST API. It mirrors connect_api.go's outbound-HTTP shape (bounded http.Client, JSON
// passthrough, SanitizeString on every wire error, nil-check-503) — split into a sibling file so
// connect_api.go stays under the 600-LOC cap and the Bearer-injecting dial helper is colocated
// with the routes that need it.
//
// The five routes are operator WRITE-class actions (creating an account stores the operator's own
// Google OAuth client; a delete drops the linked account), so the parent-mux mount (serve_webui.go)
// is behind RequireCapability(governance.write). The sidecar base URL + admin token are wired by
// the daemon composition root via SetCalendarMCP (from AURA_PIM_MCP_URL + AURA_PIM_MCP_ADMIN_TOKEN);
// an unset URL answers 503 so a stack with the sidecar off degrades gracefully. The admin token is
// injected server-side as Authorization: Bearer and NEVER returned to the client; a transport error
// or a non-2xx status is passed through with a sanitized JSON body (the sidecar host never leaks).
//
// The Google OAuth callback (GET /admin/auth/google/callback) is token-exempt and is hit DIRECTLY
// by the user's browser at the sidecar's external base URL — it is deliberately NOT proxied here.

// pimClientTimeout bounds one sidecar round-trip. The /admin API is a sibling container answering
// account CRUD + auth-start in well under a second; 8s is generous headroom while bounding a hung
// sidecar before it stalls the cockpit.
const pimClientTimeout = 8 * time.Second

// pimClient is the bounded outbound client for every PIM-admin forward. A single shared client
// reuses connections to the sibling sidecar across the connect wizard's calls.
var pimClient = &http.Client{Timeout: pimClientTimeout}

// SetCalendarMCP wires the aura-pim-mcp sidecar's /admin REST base URL + admin token the connect
// calendar routes forward to (AURA_PIM_MCP_URL + AURA_PIM_MCP_ADMIN_TOKEN). Set by the daemon
// composition root after NewServer; until set (or when the URL is empty), the five
// /api/connect/pim/* routes answer 503 so a stack without the sidecar degrades gracefully. Kept off
// the constructor so existing NewServer callers/tests stay unchanged (the SetWhatsAppBridge
// precedent).
func (s *Server) SetCalendarMCP(baseURL, adminToken string) {
	s.calendarMCPURL = baseURL
	s.calendarMCPToken = adminToken
}

// registerConnectPIMRoutes mounts the five calendar connect routes on the supplied mux using Go
// 1.22 method-pattern + {id} path-value routing — SPECIFIC method+path siblings under the /api/
// carve-out, never a bare /api/. Called from Mux next to registerConnectRoutes. The parent-mux
// mount behind RequireCapability(governance.write) lives in cmd/aura/serve_webui.go.
func (s *Server) registerConnectPIMRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connect/pim/accounts", s.handlePIMListAccounts)
	mux.HandleFunc("POST /api/connect/pim/accounts", s.handlePIMCreateAccount)
	mux.HandleFunc("DELETE /api/connect/pim/accounts/{id}", s.handlePIMDeleteAccount)
	mux.HandleFunc("GET /api/connect/pim/accounts/{id}/google/start", s.handlePIMGoogleStart)
	mux.HandleFunc("POST /api/connect/pim/accounts/{id}/logout", s.handlePIMLogout)
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
	s.forwardPIMRetry404(w, r, http.MethodGet, "/admin/auth/"+id+"/google/start", nil)
}

// handlePIMLogout serves POST /api/connect/pim/accounts/{id}/logout: forward the sidecar
// POST /admin/accounts/{id}/logout (drops the linked Google session without deleting the account).
func (s *Server) handlePIMLogout(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.forwardPIMJSON(w, r, http.MethodPost, "/admin/accounts/"+id+"/logout", nil)
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

// forwardPIMJSON forwards method+path (relative to the sidecar base) with the Bearer admin token
// injected server-side and passes the JSON response body + status code straight through. The body
// is size-capped and read into memory so a sanitized 502 can replace a partial/hostile body. The
// admin token is NEVER written to the response.
func (s *Server) forwardPIMJSON(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	resp, ok := s.dialPIM(w, r, method, path, body)
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

// forwardPIMRetry404 forwards an IDEMPOTENT GET like forwardPIMJSON, but retries a transient 404
// up to pimStartRetryAttempts (pimStartRetryDelay apart) before passing the response through. Only
// safe for idempotent reads (the google/start fetch). A non-404 (or the final 404) is passed
// through verbatim; a cancelled request short-circuits to 504.
func (s *Server) forwardPIMRetry404(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	for attempt := 0; attempt < pimStartRetryAttempts; attempt++ {
		resp, ok := s.dialPIM(w, r, method, path, body)
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
// outbound request with the admin Bearer token injected, and maps a transport failure onto a
// sanitized 502 (the sidecar host/path/token never leaks). It returns the live response (the caller
// closes the body) and whether the handler may proceed — mirroring dialBridge's (value, ok) shape.
func (s *Server) dialPIM(w http.ResponseWriter, r *http.Request, method, path string, body []byte) (*http.Response, bool) {
	if s.calendarMCPURL == "" {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "calendar connect not configured"})
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
	if s.calendarMCPToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.calendarMCPToken)
	}
	resp, err := pimClient.Do(req)
	if err != nil {
		// The error embeds the sidecar host/URL — SanitizeString collapses any DSN/userinfo/token;
		// the generic message keeps it diagnosable without leaking the sidecar host.
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": SanitizeString("calendar sidecar unreachable")})
		return nil, false
	}
	return resp, true
}
