// integrations_proxy.go is the Aura-backend reverse proxy the operator cockpit calls
// to drive a managed sidecar's admin/management REST API WITHOUT ever holding the
// sidecar's admin token: the cockpit makes a same-origin call to
// /api/integrations/<name>/<subpath>, and Aura forwards it to the sidecar on
// loopback, injecting the token server-side. This is the connect/account-management
// data plane the v1.0.0 cockpit "Integrations" surface (Phase 28/29) wires onto.
//
// Posture: the route mounts on the SAME loopback, auth-deferred daemon mux as the
// AG-UI gateway (amendment #35) — it inherits that posture and is gated together with
// the rest of the mux when serve auth lands (Phase 24).
//
// Registry-driven (builtinIntegrations): two legs are wired.
//   - calendar: the PIM sidecar's token-gated /admin REST API (accounts CRUD +
//     device-code auth), token injected server-side.
//   - whatsapp: the forked whatsmeow bridge's management REST (/api/{status,qr,logout})
//     the cockpit drives to link a device — loopback + unauthenticated, no token.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// integrationsRoutePrefix is the parent-mux subtree the cockpit calls. Go 1.22
// ServeMux longest-pattern precedence keeps it ahead of the "/" embed catch-all.
const integrationsRoutePrefix = "/api/integrations/"

// pimAdminTokenDefault matches compose.yaml's CALENDAR_MCP_ADMIN_TOKEN default, so a
// stock loopback deployment works without the operator setting anything.
const pimAdminTokenDefault = "changeme-aura-pim-local"

// integrationTarget describes one sidecar's management surface behind the proxy.
type integrationTarget struct {
	baseURL    func() string       // scheme://host:port (no trailing slash)
	apiPrefix  string              // path the sidecar serves its admin API under, e.g. "/admin"
	injectAuth func(h http.Header) // server-side auth injection; nil = none
}

// pimAdminToken is the admin token Aura injects on calendar-integration forwards. It
// mirrors the value compose passes the sidecar as CALENDAR_MCP_ADMIN_TOKEN.
func pimAdminToken() string {
	if v := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_ADMIN_TOKEN")); v != "" {
		return v
	}
	return pimAdminTokenDefault
}

// builtinIntegrations is the proxy's dispatch registry.
func builtinIntegrations() map[string]integrationTarget {
	return map[string]integrationTarget{
		"calendar": {
			baseURL:   mcpmanager.PIMSidecarBaseURL,
			apiPrefix: "/admin",
			injectAuth: func(h http.Header) {
				h.Set("X-Admin-Token", pimAdminToken())
			},
		},
		"whatsapp": {
			// The whatsmeow bridge management REST is loopback + unauthenticated, so
			// no token is injected. /api/integrations/whatsapp/status -> bridge /api/status.
			baseURL:    mcpmanager.WhatsAppBridgeBaseURL,
			apiPrefix:  "/api",
			injectAuth: nil,
		},
	}
}

// newIntegrationsProxy builds the http.Handler mounted at integrationsRoutePrefix. It
// dispatches on the <name> segment to a per-target reverse proxy; an unknown name is
// a 404 (never a silent fall-through to a wrong sidecar).
func newIntegrationsProxy() http.Handler {
	proxies := make(map[string]*httputil.ReverseProxy)
	for name, tgt := range builtinIntegrations() {
		proxies[name] = newTargetProxy(name, tgt)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, integrationsRoutePrefix)
		name, _, _ := strings.Cut(rest, "/")
		proxy, ok := proxies[name]
		if name == "" || !ok {
			http.Error(w, fmt.Sprintf("unknown integration %q", name), http.StatusNotFound)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// newTargetProxy returns a reverse proxy that rewrites
// /api/integrations/<name>/<sub> → <baseURL><apiPrefix>/<sub>, injects the target's
// auth server-side, and answers 502 when the sidecar is unreachable (it never
// crashes the daemon).
func newTargetProxy(name string, tgt integrationTarget) *httputil.ReverseProxy {
	prefix := integrationsRoutePrefix + name // e.g. "/api/integrations/calendar"
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			base, err := url.Parse(tgt.baseURL())
			if err != nil {
				return // baseURL is constructed loopback/DNS, not user input
			}
			sub := strings.TrimPrefix(pr.In.URL.Path, prefix) // "/accounts", "/auth/{id}/start", ""
			pr.Out.URL.Scheme = base.Scheme
			pr.Out.URL.Host = base.Host
			pr.Out.URL.Path = tgt.apiPrefix + sub
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Host = base.Host
			pr.SetXForwarded() // the sidecar's redirect-URI builder reads X-Forwarded-*
			if tgt.injectAuth != nil {
				tgt.injectAuth(pr.Out.Header)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			slog.Warn("integrations proxy: sidecar unreachable", "integration", name, "err", err)
			http.Error(w, fmt.Sprintf("integration %q sidecar unreachable", name), http.StatusBadGateway)
		},
	}
}
