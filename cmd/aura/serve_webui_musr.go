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
//
// Phase 2 plan 07 (RBAC-05/CRED-03/CRED-06) adds two more admin routes here:
//
//   - GET/POST /api/admin/identities/{id}/credit — the credit-cap read/write
//     (credit_api.go). Gated on governance.write, the SAME gate the four routes above
//     already use "for consistency" per audit_api.go's own header comment — under D-01
//     that gate no longer distinguishes an admin from a member, but this route was
//     never meant to be admin-exclusive in the D-01 sense; it just needs a caller who
//     is authenticated and passes the existing admin-surface gate, exactly like the
//     capability grant/revoke routes it sits beside.
//   - DELETE /api/admin/identities/{id} — identity removal (deprovision_route.go).
//     Gated on identity.delete, NOT governance.write — this is the ONE route on this
//     surface that IS still admin-exclusive under D-01 (identity.delete is one of
//     exactly two administrative capabilities), and copying the neighbouring
//     governance.write mount would make removal available to every user in the
//     deployment. Referenced directly as identity.CapIdentityDelete (not through a
//     same-shaped local alias like identityCreateCapability) so the mount and the
//     capability name it depends on are one grep away from each other.
//
// Phase 2 plan 09 (RBAC-11/CRED-06) adds a third:
//
//   - GET /api/admin/spend/overview — the account-wide reconciliation surface
//     (spend_overview_api.go): five KPI tiles, Top-Identities-by-spend, the
//     over-allocation advisory. Gated on governance.write, the SAME gate the credit
//     routes above use — this is a read, and credit management (unlike identity
//     removal) was never one of D-01's two administrative capabilities. Under D-01
//     that gate no longer distinguishes an admin from a member, so this route's real
//     protection is that it exposes account-wide reconciliation data any identity in
//     the deployment could already see reflected in its own roster row and credit
//     panel — nothing here is new information an identity couldn't already infer,
//     just aggregated. Following the credit routes' own precedent rather than
//     inventing a stricter gate for a read.
//
// And the in-app restart:
//
//   - POST /api/admin/restart — ends the daemon as SIGTERM does, so the container's
//     restart policy brings it back (restart_api.go). Gated on governance.write, the
//     gate of the settings writes whose boot-bound rows it exists to apply.

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

const (
	meRoute                 = "GET /api/me"
	adminIdentitiesRoute    = "GET /api/admin/identities"
	adminGrantRoute         = "POST /api/admin/identities/{id}/capabilities"
	adminRevokeRoute        = "DELETE /api/admin/identities/{id}/capabilities/{capability}"
	adminAuditRoute         = "GET /api/admin/audit"
	adminCreditGetRoute     = "GET /api/admin/identities/{id}/credit"  // #nosec G101 -- a route pattern, not a credential.
	adminCreditSetRoute     = "POST /api/admin/identities/{id}/credit" // #nosec G101 -- a route pattern, not a credential.
	adminRemoveRoute        = "DELETE /api/admin/identities/{id}"
	adminSpendOverviewRoute = "GET /api/admin/spend/overview"
	adminRestartRoute       = "POST /api/admin/restart"
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
	mux.Handle(adminCreditGetRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(adminCreditSetRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	// The ONE route on this surface gated on identity.CapIdentityDelete rather than
	// governance.write — see the file header for why.
	mux.Handle(adminRemoveRoute, agui.RequireCapability(aguiHandler, auth, identity.CapIdentityDelete))
	mux.Handle(adminSpendOverviewRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(adminRestartRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
}
