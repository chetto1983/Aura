package main

// serve_webui_musr.go carries the Phase-36 (MUSR-01) admin/user-distinction parent-mux
// mounts, kept OUT of serve_webui.go so that file stays under the 600-LOC ceiling. It
// mounts the D-03/D-26/D-28 surface registered on the agui Server.Mux (audit_api.go):
//
//   - GET /api/me — SELF-scoped (a user reads their OWN capabilities so the SPA can hide
//     admin surfaces). It inherits the whole-origin RequireAuth from the parent-mux wrap;
//     NO RequireCapability (self-read is not privileged).
//   - GET /api/admin/identities, POST/DELETE .../{id}/capabilities[/{cap}],
//     GET /api/admin/audit — the admin surface, each interposed with
//     RequireCapability(governance.write). The SPA hide is cosmetic; THIS server-side gate
//     is the trust boundary (T-36-10-E). governance.write is the EXISTING capability
//     (RESEARCH OQ3 — no net-new settings.model.write).
//
// GET /api/settings/telegram/link is deliberately NOT touched here — it stays a self-scoped
// USER action (D-02), gated only by the governance.write it already carries in
// serve_webui.go for the write-class Telegram recovery, never re-gated as an admin route.

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	meRoute              = "GET /api/me"
	adminIdentitiesRoute = "GET /api/admin/identities"
	adminGrantRoute      = "POST /api/admin/identities/{id}/capabilities"
	adminRevokeRoute     = "DELETE /api/admin/identities/{id}/capabilities/{capability}"
	adminAuditRoute      = "GET /api/admin/audit"
)

// registerMUSRRoutes mounts the admin/user-distinction routes on the parent mux. Each
// delegates to the AG-UI handler (routes live on Server.Mux). Method+path-specific so each
// wins Go 1.22 longest-pattern precedence over the bare "/api/" carve-out and the "/" embed
// catch-all; the "/api/" fallback exclusion already returns them as backend routes.
func registerMUSRRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(meRoute, aguiHandler)
	mux.Handle(adminIdentitiesRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(adminGrantRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(adminRevokeRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(adminAuditRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
}
