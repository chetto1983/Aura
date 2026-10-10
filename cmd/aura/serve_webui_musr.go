package main

// serve_webui_musr.go carries the Phase-36 (MUSR-01) admin/user-distinction parent-mux
// mounts, kept OUT of serve_webui.go so that file stays under the 600-LOC ceiling. Each
// delegates to the AG-UI handler, where the routes live on Server.Mux (audit_api.go,
// credit_api.go, deprovision_route.go, spend_overview_api.go, restart_api.go,
// openrouter_reconcile.go, system_update_api.go).
//
// GET /api/me and GET /api/system/update are every identity's: the first so the SPA can
// hide what the caller may not use, the second because a member must be warned before the
// host updater restarts Aura under them.
//
// Everything else here administers the deployment or another identity — the roster and its
// grants, the activity feed, credit caps, spend, restart, key minting, updates — so it takes
// an administrative capability. It used to take governance.write, which D-01 grants to every
// identity, so any member could revoke the admin's capabilities or lift their own credit cap
// (prd.md §3, 2026-10-10). The SPA hide is cosmetic; THIS gate is the trust boundary. Removal takes
// identity.delete, every other route identity.create.

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

const (
	meRoute                       = "GET /api/me"
	adminIdentitiesRoute          = "GET /api/admin/identities"
	adminGrantRoute               = "POST /api/admin/identities/{id}/capabilities"
	adminRevokeRoute              = "DELETE /api/admin/identities/{id}/capabilities/{capability}"
	adminAuditRoute               = "GET /api/admin/audit"
	adminCreditGetRoute           = "GET /api/admin/identities/{id}/credit"  // #nosec G101 -- a route pattern, not a credential.
	adminCreditSetRoute           = "POST /api/admin/identities/{id}/credit" // #nosec G101 -- a route pattern, not a credential.
	adminRemoveRoute              = "DELETE /api/admin/identities/{id}"
	adminSpendOverviewRoute       = "GET /api/admin/spend/overview"
	adminRestartRoute             = "POST /api/admin/restart"
	adminOpenRouterReconcileRoute = "POST /api/admin/openrouter/reconcile"
	systemUpdateRoute             = "GET /api/system/update"
	systemUpdateApplyRoute        = "POST /api/system/update/apply"
	systemUpdateDeferRoute        = "POST /api/system/update/defer"
)

// registerMUSRRoutes mounts the admin/user-distinction routes on the parent mux. Each
// delegates to the AG-UI handler (routes live on Server.Mux). Method+path-specific so each
// wins Go 1.22 longest-pattern precedence over the bare "/api/" carve-out and the "/" embed
// catch-all; the "/api/" fallback exclusion already returns them as backend routes.
func registerMUSRRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(meRoute, aguiHandler)
	for _, route := range []string{
		adminIdentitiesRoute, adminGrantRoute, adminRevokeRoute, adminAuditRoute,
		adminCreditGetRoute, adminCreditSetRoute, adminOpenRouterReconcileRoute,
		adminSpendOverviewRoute, adminRestartRoute, systemUpdateApplyRoute, systemUpdateDeferRoute,
	} {
		mux.Handle(route, agui.RequireCapability(aguiHandler, auth, identity.CapIdentityCreate))
	}
	mux.Handle(adminRemoveRoute, agui.RequireCapability(aguiHandler, auth, identity.CapIdentityDelete))
	mux.Handle(systemUpdateRoute, aguiHandler)
}
