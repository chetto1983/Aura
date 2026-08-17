package agui

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

var (
	errSandboxOriginUnset      = errors.New("mcp view sandbox origin is not configured")
	errSandboxOriginSameAsHost = errors.New("mcp view sandbox origin must differ from the cockpit origin")
)

// mcp_views_api.go serves the MCP Apps view documents a mounted server declared
// (SEP-1865), so the cockpit can render one in a sandboxed frame.
//
// The document travels HERE and not on the SSE stream because it is static per
// server: re-sending ~20 KB of HTML on every call would spend the operator's
// downlink on bytes that never change. The stream carries the descriptor
// (aura.mcp_view), this route carries the document, and the two meet on
// (server, resource_uri).
//
// Nothing in this file knows which servers exist. The catalog is keyed by the
// pair, the trust gate ran at mount, and a URI that is not in the catalog is a
// 404 — there is no list of known servers to fall out of date.

const (
	mcpViewPath     = "GET /api/mcp/view"
	mcpViewCallPath = "POST /api/mcp/view/call"
	// MCPSandboxPath serves the sandbox proxy document. It is PUBLIC: it holds no
	// data of its own (everything it relays arrives by postMessage from the host)
	// and it is fetched from the second origin, which must not depend on a cookie
	// scoped to the cockpit's. Exported so the composition root's public-route
	// allowlist names the same constant the mux does.
	MCPSandboxPath = "/mcp-sandbox"
)

//go:embed mcp_sandbox.html
var mcpSandboxHTML []byte

// MCPViewToolCaller runs a tool a RENDERED VIEW asked for. The implementation
// (internal/agent/mcptools) refuses anything that is not read-only and anything
// on a server other than the one named — this package only carries the request.
type MCPViewToolCaller interface {
	CallForView(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, error)
}

// registerMCPViewRoutes mounts the view routes. The read + call routes inherit
// the whole-origin RequireAuth from the parent mux, like the composer routes: a
// view document is a mounted server's private HTML and a view's tool call runs
// against the operator's own data.
func (s *Server) registerMCPViewRoutes(mux *http.ServeMux) {
	mux.HandleFunc(mcpViewPath, s.handleMCPView)
	mux.HandleFunc(mcpViewCallPath, s.handleMCPViewCall)
	mux.HandleFunc("GET "+MCPSandboxPath, s.handleMCPSandbox)
}

// SetMCPViews wires the mount-time view catalog, the origin the cockpit must
// frame those views from, and the callback a rendered view's tools/call reaches.
// Set by the daemon composition root after NewServer; until set the routes answer
// 503, mirroring every other narrow seam here.
func (s *Server) SetMCPViews(catalog *mcp.ViewCatalog, sandboxOrigin string, caller MCPViewToolCaller) {
	s.mcpViews = catalog
	s.mcpSandboxOrigin = strings.TrimRight(strings.TrimSpace(sandboxOrigin), "/")
	s.mcpViewCaller = caller
}

// handleMCPSandbox serves the relay document (mcp_sandbox.html).
//
// Two headers carry the whole security posture of this route. The CSP allows the
// page's own inline script and NOTHING else to load — no network, no fonts, no
// images — because a relay needs none of it; and frame-ancestors narrows the
// embedders to the single origin the caller named. That origin is the CALLER's
// claim, not a configured one — the cockpit passes its own, and a page that
// passes a different one only ever gets a relay framed on an origin with no
// session, no data and nothing it may fetch. What it must never become is a way
// to write the header itself, which is why only a bare scheme://authority is
// accepted. The view document is not covered by this CSP at all: it lives in a
// nested frame with its own, which is the separation the proxy exists for.
func (s *Server) handleMCPSandbox(w http.ResponseWriter, r *http.Request) {
	ancestor := frameAncestor(r.URL.Query().Get("host"))
	if ancestor == "" {
		http.Error(w, "a scheme://host origin is required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; frame-src data: blob:; frame-ancestors "+ancestor)
	_, _ = w.Write(mcpSandboxHTML)
}

// mcpViewCallRequest is what a rendered view asks for through the host.
type mcpViewCallRequest struct {
	Server    string         `json:"server"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

// handleMCPViewCall serves POST /api/mcp/view/call: the view-initiated tools/call
// the MCP Apps protocol defines, which is what lets a view page and refresh.
//
// The gate is not here. It is in the caller, where the mounted server's own
// read-only classification lives — this handler must not grow a second, weaker
// copy of it. What it does own is refusing to run at all when the sandbox origin
// is missing or collapsed onto the cockpit's: in that state no view should be
// rendering, so none should be calling either.
func (s *Server) handleMCPViewCall(w http.ResponseWriter, r *http.Request) {
	if s.mcpViews == nil || s.mcpViewCaller == nil {
		http.Error(w, "mcp views unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := s.sandboxOriginFor(r); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req mcpViewCallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxViewCallBodyBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Server = strings.TrimSpace(req.Server)
	req.Tool = strings.TrimSpace(req.Tool)
	if req.Server == "" || req.Tool == "" {
		http.Error(w, "server and tool are required", http.StatusBadRequest)
		return
	}
	// A view may only call back into a server that actually served it one — the
	// catalog is the record of that, and without this a valid session could drive
	// any mounted server's read tools straight from the browser.
	if !s.mcpViews.HasServer(req.Server) {
		http.Error(w, "no such view server", http.StatusNotFound)
		return
	}
	structured, err := s.mcpViewCaller.CallForView(r.Context(), req.Server, req.Tool, req.Arguments)
	if err != nil {
		slog.Warn("mcp view tool call refused", "server", req.Server, "tool", req.Tool, "error", err)
		http.Error(w, "tool call refused", http.StatusForbidden)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, map[string]any{"structured_content": structured})
}

// maxViewCallBodyBytes bounds a view's tools/call body. A view sends a handful of
// arguments (a page number, an id); 64 KiB is headroom without letting a rendered
// document post a payload at the host.
const maxViewCallBodyBytes = 64 << 10

// mcpViewDTO is what the cockpit needs to frame one view: the document already
// armed with its policy, what the server declared, and where the sandbox lives.
//
// Declared is reported ALONGSIDE the policy that was actually applied, because
// the two can differ: a declared domain that is not unambiguously an https origin
// is dropped (mcp.ContentSecurityPolicy), and a human debugging a view that
// renders half its content needs to see which of the two lists it fell out of.
type mcpViewDTO struct {
	Server        string         `json:"server"`
	ResourceURI   string         `json:"resource_uri"`
	HTML          string         `json:"html"`
	SandboxOrigin string         `json:"sandbox_origin"`
	AppliedCSP    string         `json:"applied_csp"`
	Declared      mcpViewPolicy  `json:"declared"`
	Permissions   map[string]any `json:"permissions,omitempty"`
}

type mcpViewPolicy struct {
	Sealed          bool     `json:"sealed"`
	DeclaredCSP     bool     `json:"declared_csp"`
	ConnectDomains  []string `json:"connect_domains,omitempty"`
	ResourceDomains []string `json:"resource_domains,omitempty"`
	FrameDomains    []string `json:"frame_domains,omitempty"`
	BaseURIDomains  []string `json:"base_uri_domains,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	PrefersBorder   bool     `json:"prefers_border"`
}

// handleMCPView serves GET /api/mcp/view?server=&uri=.
//
// The same-origin refusal is the load-bearing check. MCP Apps requires a web host
// to frame a view through a sandbox proxy on a DIFFERENT origin; if Aura served
// the pair from one origin, `allow-same-origin` (which the view needs to run at
// all) would hand a mounted server's document the cockpit's own origin, and with
// it the operator's session. Refusing to answer is the only safe response to a
// misconfiguration here — a degraded render would be a silent hole.
func (s *Server) handleMCPView(w http.ResponseWriter, r *http.Request) {
	if s.mcpViews == nil {
		http.Error(w, "mcp views unavailable", http.StatusServiceUnavailable)
		return
	}
	origin, err := s.sandboxOriginFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	uri := strings.TrimSpace(r.URL.Query().Get("uri"))
	if server == "" || !mcp.IsAppURI(uri) {
		http.Error(w, "server and a ui:// uri are required", http.StatusBadRequest)
		return
	}
	doc, ok := s.mcpViews.Get(server, uri)
	if !ok {
		http.Error(w, "no such view", http.StatusNotFound)
		return
	}
	// A cookie ignores the port, so a declared domain pointing back at THIS host
	// would let a view call Aura's API as the operator from inside the sandbox.
	// The declaration is reported as the server wrote it; the policy that is
	// actually applied has that door taken out (mcp.WithoutHost).
	granted := doc.Policy.WithoutHost(hostnameOf(r))
	// The document leaves here already carrying its policy: the cockpit renders it
	// from a srcdoc frame, where no response header of ours would follow it, so the
	// policy has to travel with the bytes (mcp.ArmedHTML).
	csp := granted.ContentSecurityPolicy()
	// The body carries HTML inside a JSON field. nosniff is what keeps a browser
	// from deciding to render it as a document on THIS origin.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, mcpViewDTO{
		Server:        doc.Server,
		ResourceURI:   doc.ResourceURI,
		HTML:          mcp.ArmedHTML(doc.HTML, csp),
		SandboxOrigin: origin,
		AppliedCSP:    csp,
		Permissions:   doc.Policy.Permissions,
		Declared: mcpViewPolicy{
			Sealed:          doc.Policy.Sealed(),
			DeclaredCSP:     doc.Policy.DeclaredCSP,
			ConnectDomains:  doc.Policy.ConnectDomains,
			ResourceDomains: doc.Policy.ResourceDomains,
			FrameDomains:    doc.Policy.FrameDomains,
			BaseURIDomains:  doc.Policy.BaseURIDomains,
			Domain:          doc.Policy.Domain,
			PrefersBorder:   doc.Policy.PrefersBorder,
		},
	})
}

// sandboxOriginFor returns the configured sandbox origin, refusing when it is
// unset or when it is the origin this request arrived on.
func (s *Server) sandboxOriginFor(r *http.Request) (string, error) {
	if s.mcpSandboxOrigin == "" {
		return "", errSandboxOriginUnset
	}
	if sameOrigin(s.mcpSandboxOrigin, requestOrigin(r)) {
		return "", errSandboxOriginSameAsHost
	}
	return s.mcpSandboxOrigin, nil
}

// requestOrigin reconstructs the origin the browser used. Behind Caddy the scheme
// arrives as X-Forwarded-Proto and the authority as Host; both are client-
// influenced, which is fine here because the value is only ever COMPARED against
// the configured origin — a forged header can make this route refuse, never make
// it render a view somewhere it should not.
func requestOrigin(r *http.Request) string {
	scheme := "https"
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(forwarded)
	} else if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// sameOrigin compares two origins by scheme+host+port, resolving the default port
// so https://h and https://h:443 are recognised as the one origin a browser sees.
func sameOrigin(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) &&
		strings.EqualFold(ua.Hostname(), ub.Hostname()) &&
		originPort(ua) == originPort(ub)
}

// hostnameOf is the browser-visible host, port stripped — the granularity a
// cookie is scoped at, which is the granularity WithoutHost must compare at.
func hostnameOf(r *http.Request) string {
	host := r.Host
	if idx := strings.LastIndexByte(host, ':'); idx >= 0 && !strings.Contains(host[idx:], "]") {
		host = host[:idx]
	}
	return strings.Trim(host, "[]")
}

// frameAncestor normalises the origin a relay says it is being embedded from.
// The value lands verbatim in a CSP directive, so anything that is not a bare
// http(s) scheme://authority is refused: a path, credentials or trailing tokens
// would let a caller append directives of its own rather than name an ancestor.
func frameAncestor(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil {
		return ""
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return ""
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + u.Host
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}
