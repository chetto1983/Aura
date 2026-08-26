package agui

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"
)

// connect_api.go is the cockpit "Connect" device-linking proxy (operator directive
// 2026-06-21: "can't setup whatsapp"). It is the thin REST adapter the Governance MCP
// detail's WhatsApp "Link device" section drives, forwarding to the aura-whatsapp bridge
// management REST (chetto1983/whatsapp-mcp, host 8094 → container :8081). It mirrors
// image_proxy.go's outbound-HTTP shape (bounded http.Client, strict response headers,
// SanitizeString on every wire error) and governance_write_api.go's nil-check-503 +
// JSON-passthrough posture.
//
// The three routes are operator WRITE-class actions (a logout drops the paired session, a
// QR scan links a device), so the parent-mux mount (serve_webui.go) is behind
// RequireCapability(governance.write) — never a bare unauthenticated relay. The bridge base
// URL + private bridge bearer are wired by the daemon composition root via
// SetWhatsAppBridge. Every request also carries the authenticated tenant id, so the
// gateway selects that tenant's isolated WhatsMeow runtime and store. Missing
// configuration answers 503; bridge host/credentials never leak in an error response.

// connectClientTimeout bounds one bridge round-trip. The bridge management REST is local
// (a sibling container) and answers status/qr/logout in well under a second; 8s is generous
// headroom while bounding a hung sidecar before it stalls the cockpit poll.
const connectClientTimeout = 8 * time.Second

// connectClient is the bounded outbound client for every bridge forward. A single shared
// client reuses connections to the sibling sidecar across the 4s status poll.
var connectClient = &http.Client{Timeout: connectClientTimeout}

// SetWhatsAppBridge wires the WhatsApp management endpoint and its private bridge
// bearer. This credential never authenticates the MCP resource server.
func (s *Server) SetWhatsAppBridge(url, token string) {
	s.whatsappBridgeURL = strings.TrimRight(strings.TrimSpace(url), "/")
	s.whatsappBridgeToken = strings.TrimSpace(token)
}

// registerConnectRoutes mounts the three WhatsApp connect routes on the supplied mux using
// Go 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/ carve-out,
// never a bare /api/ (Pitfall 5). Called from BuildHandler (Mux) next to
// registerGovernanceWriteRoutes. The parent-mux mount behind
// RequireCapability(governance.write) lives in cmd/aura/serve_webui.go.
func (s *Server) registerConnectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connect/whatsapp/status", s.handleWhatsAppStatus)
	mux.HandleFunc("GET /api/connect/whatsapp/qr.png", s.handleWhatsAppQR)
	mux.HandleFunc("POST /api/connect/whatsapp/logout", s.handleWhatsAppLogout)
}

// handleWhatsAppStatus serves GET /api/connect/whatsapp/status: forward the bridge
// GET /api/status and pass through its JSON body + status code so the UI can branch on
// state/paired/connected. An unset bridge → 503; a transport/decode failure → sanitized 502.
func (s *Server) handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	s.forwardBridgeJSON(w, r, http.MethodGet, "/api/status")
}

// handleWhatsAppLogout serves POST /api/connect/whatsapp/logout: forward the bridge
// POST /api/logout (drops the paired session) and pass through its JSON body + status code.
func (s *Server) handleWhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	s.forwardBridgeJSON(w, r, http.MethodPost, "/api/logout")
}

// handleWhatsAppQR serves GET /api/connect/whatsapp/qr.png: fetch the bridge GET /api/qr,
// decode {"code":string}, and render the raw linked-devices payload server-side as a QR PNG
// (no frontend QR dependency). A 409 (already paired) or 503 (qr not ready) from the bridge
// is propagated with a small JSON body so the UI knows paired/not-ready without parsing an
// image. The 200 PNG carries Cache-Control: no-store (the QR rotates ~20-60s) + nosniff.
func (s *Server) handleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	resp, ok := s.dialBridge(w, r, http.MethodGet, "/api/qr")
	if !ok {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// The bridge tells the UI paired (409) / not-ready (503) out of band — propagate the
	// status with a small JSON body so the cockpit can branch without decoding an image.
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusServiceUnavailable {
		writeJSONStatus(w, resp.StatusCode, map[string]string{"state": bridgeQRState(resp.StatusCode)})
		return
	}
	if resp.StatusCode != http.StatusOK {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "whatsapp bridge error"})
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRunBodyBytes)).Decode(&body); err != nil || body.Code == "" {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "whatsapp qr payload invalid"})
		return
	}
	code, err := qr.Encode(body.Code, qr.M)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "whatsapp qr render failed"})
		return
	}
	png := code.PNG()
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Content-Length", strconv.Itoa(len(png)))
	// The linked-devices QR rotates server-side every ~20-60s; never cache a stale frame.
	h.Set("Cache-Control", "no-store")
	// Defense-in-depth: the proxied bytes are an image, never a document.
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// bridgeQRState maps the bridge's out-of-band QR status codes onto the UI-facing state
// string the cockpit branches on (paired = stop polling QR; qr_not_ready = keep polling).
func bridgeQRState(status int) string {
	if status == http.StatusConflict {
		return "paired"
	}
	return "qr_not_ready"
}

// forwardBridgeJSON forwards method+path to the bridge and passes the JSON response body +
// status code straight through (status/logout). The Content-Type is forced to JSON so a
// bridge that omits it still reads as JSON; the bytes are size-capped and read into memory so
// a sanitized 502 can replace a partial/hostile body.
func (s *Server) forwardBridgeJSON(w http.ResponseWriter, r *http.Request, method, path string) {
	resp, ok := s.dialBridge(w, r, method, path)
	if !ok {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRunBodyBytes))
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "whatsapp bridge read failed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// dialBridge requires the endpoint + bridge bearer, resolves the authenticated
// principal, and forwards both the bearer and X-Tenant-ID. The gateway never
// accepts a caller-supplied tenant selector.
func (s *Server) dialBridge(w http.ResponseWriter, r *http.Request, method, path string) (*http.Response, bool) {
	if s.whatsappBridgeURL == "" || s.whatsappBridgeToken == "" {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "whatsapp connect not configured"})
		return nil, false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return nil, false
	}
	req, err := http.NewRequestWithContext(r.Context(), method, s.whatsappBridgeURL+path, nil)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "whatsapp bridge request failed"})
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.whatsappBridgeToken)
	req.Header.Set("X-Tenant-ID", identityID)
	resp, err := connectClient.Do(req)
	if err != nil {
		// The error embeds the bridge host/URL — SanitizeString collapses any DSN/userinfo/
		// token; the generic message keeps it diagnosable without leaking the sidecar host.
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": SanitizeString("whatsapp bridge unreachable")})
		return nil, false
	}
	return resp, true
}
