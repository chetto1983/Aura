package main

// serve_webui_mcpviews.go carries the MCP Apps (SEP-1865) parent-mux mounts, kept
// OUT of serve_webui.go so that file stays under the 600-LOC ceiling (mirrors the
// serve_webui_composer.go / serve_webui_voice.go extractions). The handlers
// themselves live on the agui Server.Mux (internal/agui/mcp_views_api.go); this
// file only routes the parent mux to them.
//
// Three routes, two postures:
//
//   - GET /api/mcp/view and POST /api/mcp/view/call — bare aguiHandler, so they
//     inherit the whole-origin RequireAuth with no capability gate. A view
//     document is a mounted server's HTML and a view's tool call runs against the
//     operator's own data; any authenticated identity may see and drive its own.
//   - GET /mcp-sandbox — the relay document, admitted UNAUTHENTICATED by the
//     PublicRoute allowlist in serve_webui.go. It is fetched from the second
//     origin and holds no data of its own; requiring a cookie scoped to the
//     cockpit's origin would couple the two origins the extension exists to keep
//     apart.
//
// Without these mounts the routes are unreachable: the parent mux sends "/" to
// the embedded SPA, so an agui route nobody forwards answers with index.html.
// Measured 2026-08-17 — the sandbox document came back as a 1678-byte SPA shell
// with none of its security headers, because only the agui mux sets those.

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	mcpViewRoute     = "GET /api/mcp/view"
	mcpViewCallRoute = "POST /api/mcp/view/call"
)

// registerMCPViewRoutes mounts the MCP Apps routes on the parent mux. auth is
// accepted for signature parity with the sibling registrars.
func registerMCPViewRoutes(mux *http.ServeMux, aguiHandler http.Handler, _ agui.AuthDeps) {
	mux.Handle(mcpViewRoute, aguiHandler)
	mux.Handle(mcpViewCallRoute, aguiHandler)
	mux.Handle("GET "+agui.MCPSandboxPath, aguiHandler)
}
