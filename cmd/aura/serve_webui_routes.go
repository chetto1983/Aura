// serve_webui_routes.go is the single source of the parent-mux route surface for the
// embedded-SPA daemon host: every route-name constant, the AG-UI prefix registration
// list (aguiRoutePrefixes), the SPA-fallback exclusion set (fallbackExcludedPrefixes),
// and the capability-grant names. Split out of serve_webui.go (600-LOC refactor-on-
// touch); the precedence/carve-out doctrine lives in serve_webui.go's header.
package main

// authBasePath is the route prefix the embedded Authula provider serves its
// credential flows under (its BasePath, spec §4 / config.WithBasePath). Mounted as a
// subtree on the parent mux and marked public in RequireAuth (login/TOTP happen before
// a session exists). Kept here as the single source so the mount, the public-path
// marking, and the fallback exclusion cannot drift.
const authBasePath = "/auth"

// authConfigRoute is the tiny public bootstrap contract the SPA login page reads for
// the Authula email/password + TOTP flow. It reveals no secret material and mints the
// double-submit CSRF token the next unsafe /auth/* request must echo.
const authConfigRoute = "/api/auth/config"

const (
	passwordResetStartRoute    = "POST /api/auth/password-reset/start"    // #nosec G101 -- route pattern, not credential material.
	passwordResetQuestionRoute = "POST /api/auth/password-reset/question" // #nosec G101 -- route pattern, not credential material.
	passwordResetVerifyRoute   = "POST /api/auth/password-reset/verify"   // #nosec G101 -- route pattern, not credential material.
	passwordResetCompleteRoute = "POST /api/auth/password-reset/complete" // #nosec G101 -- route pattern, not credential material.
	bootstrapOperatorRoute     = "POST /api/auth/bootstrap/operator"
)

// aguiRoutePrefixes are the route patterns the AG-UI gateway owns. Registered on
// the parent mux ahead of the "/" catch-all, Go 1.22 ServeMux precedence keeps them
// authoritative — a request to any of these reaches the AG-UI handler, never the
// embed. The trailing-slash "/threads/" is a subtree pattern (matches
// /threads/{id}/messages); the rest are exact paths.
var aguiRoutePrefixes = []string{
	"/healthz",
	"/readyz",
	"/debug/vars",
	"/metrics",
	"/agent/run",
	// Run-scoped subtree (fix-plan 1.3 Tier B, amendment #90 points 3-4): the SSE
	// resume GET /agent/runs/{runID}/events and the Stop POST /agent/runs/{runID}/cancel
	// live on the AG-UI mux — without this subtree entry they fell to the embed's
	// backend-404 (the live E2E caught exactly that). Auth: RequireAuth wraps the whole
	// subtree; the cancel mutation is governed by the idempotency inventory + the
	// owner-scoped 404 ladder (per the amendment), not by a capability grant.
	"/agent/runs/",
	"/threads/",
}

// fallbackExcludedPrefixes is the SINGLE source of the SPA-fallback exclusion set:
// any request path under one of these returns a real 404 from internal/webui rather
// than the SPA shell (WEB-01 / SC1). It mirrors aguiRoutePrefixes (with the whole
// /agent/ namespace excluded — a typo like /agent/typo must 404 as backend, not fall
// back to the shell), adds the integrations proxy subtree, and adds the forward-compat
// "/api/" carve-out (which also subsumes /api/integrations/). It is passed into
// webui.Handler so the fallback never hard-codes a second list that could drift from
// the mux registration above (Pitfall 6 / T-24-08).
func fallbackExcludedPrefixes() []string {
	return []string{
		"/healthz",
		"/readyz",
		"/debug/vars",
		"/metrics",
		"/agent/",   // whole AG-UI agent namespace (mux registers /agent/run + the /agent/runs/ subtree)
		"/threads/", // AG-UI threads subtree
		integrationsRoutePrefix,
		"/api/",            // forward-compat carve-out; exclusion-only, never a mux registration
		authBasePath + "/", // Authula credential subtree — a backend route, never the SPA shell
	}
}

// agentRunCapability is the capability_grants name the mutating POST /agent/run route
// is gated on (D-04 / WEB-03). The seeded `local` identity holds the `*` wildcard so it
// passes regardless of the exact name; the name only becomes load-bearing when real
// grants arrive in Phase 28. It invents no governance write routes — those land later.
const agentRunCapability = "agent.run"

// governanceReadCapability gates the Phase-28 governance board read surface. The boards
// are read-only, but scheduler and audit rows can reveal cross-identity operational
// metadata, so they require an explicit grant instead of authentication alone.
const governanceReadCapability = "governance.read"

// governanceWriteCapability gates every Phase-29 governance WRITE surface (MCP config
// mutation + skill install). It is strictly stronger than governance.read: a write can
// install a new MCP server or a RISKY supply-chain skill, so it requires its own grant.
// The seeded `local` identity holds the `*` wildcard so it passes regardless of the exact
// name (the name becomes load-bearing once real grants arrive). Plan 29-02 mounts the six
// MCP write routes behind it (governanceMCP*Route); plan 29-03 adds the skill-install
// mounts. The auth_test.go:494 `governance.write` 403 assertion and these mounts agree on
// one string.
const governanceWriteCapability = "governance.write"

// conversationsRoutePrefix is the CHAT-02 conversation-management subtree (Phase 25),
// registered on the parent mux as a SPECIFIC subtree delegating to the AG-UI handler.
// It MUST stay a sibling of "/api/integrations/" under the "/api/" exclusion carve-out
// — never a bare "/api/", which would shadow the integrations proxy (T-24-07 / T-25-05).
const conversationsRoutePrefix = "/api/conversations/"

// conversationsListRoute is the exact (no trailing slash) list endpoint. Go 1.22
// ServeMux 301-redirects "/api/conversations" into the "/api/conversations/" subtree
// unless the exact path is also registered, so registering it explicitly keeps the
// list GET reachable without a redirect hop.
const conversationsListRoute = "/api/conversations"

// branchEditRoute / branchSelectRoute are the D-09 / CHAT-05 MUTATING branch re-run
// endpoints (plan 25-07). Re-running a (possibly background) thread is privileged
// (T-25-25 / V4), so both are interposed with RequireCapability exactly like POST
// /agent/run — the method+path-specific pattern wins Go 1.22 longest-pattern precedence
// over the bare "/api/conversations/" subtree (which carries the read GET /branches under
// RequireAuth only). The fork+re-run (edit) and the branch-select re-run both fire AFTER
// RequireAuth has bound the principal.
const (
	branchEditRoute   = "POST /api/conversations/{id}/edit"
	branchSelectRoute = "POST /api/conversations/{id}/branches/{branchSeq}/select"
)

// approvalsListRoute is the exact (no trailing slash) cross-thread pending read
// (APRV-01), registered as a SPECIFIC parent-mux path delegating to the AG-UI handler
// — a sibling of "/api/conversations/" + "/api/integrations/" under the "/api/"
// exclusion carve-out, NEVER a bare "/api/" (which would shadow the integrations proxy,
// T-25-05). It inherits RequireAuth from the whole-mux wrap below.
const approvalsListRoute = "/api/approvals"

// The standing-approval grant routes (amendment #127). approvalsListRoute is an EXACT path,
// so /api/approvals/grants does not inherit it — these are their own specific parent-mux
// entries, siblings under the same "/api/" carve-out, never a bare "/api/approvals/" prefix
// (which would swallow the capability-gated resolve route).
//
// Neither is capability-gated beyond RequireAuth, and that is deliberate: both are scoped
// server-side to the AUTHENTICATED principal's own grants, and revoking is a
// DE-escalation — gating the only way to close a standing permission is how an operator
// ends up unable to close one.
const (
	approvalGrantsRoute       = "/api/approvals/grants"
	approvalGrantsRevokeRoute = "/api/approvals/grants/revoke"
)

// imageProxyRoute is the DISP-05/D-09 SSRF-safe image relay (web_result thumbnails/
// favicons). It is a sibling of "/api/conversations/" + "/api/approvals" under the
// "/api/" exclusion carve-out — NEVER a bare "/api/" (which would shadow the
// integrations proxy, T-24-07 / T-25-05). It delegates to the AG-UI handler (the route
// lives on Server.Mux) and is a read GET, so it inherits RequireAuth from the whole-mux
// wrap with no capability gate.
const imageProxyRoute = "GET /api/image-proxy"

// graphSchemaRoute / graphQueryRoute are the Phase-27 GRAPH-01 read-only graph-explorer
// routes (the schema overview + the structured-intent query). Like imageProxyRoute they
// are SPECIFIC method+path siblings under the "/api/" exclusion carve-out — NEVER a bare
// "/api/" (which would shadow the integrations proxy, T-27-03). Both delegate to the AG-UI
// handler (the routes live on Server.Mux) and inherit RequireAuth from the whole-mux wrap
// below with NO RequireCapability — this is the read-only milestone (no write/PATCH/DELETE
// graph surface; the mutating add-note route is deferred to Phase 29).
const (
	graphSchemaRoute = "GET /api/graph/schema"
	graphQueryRoute  = "POST /api/graph/query"
)

// governance* are the Phase-28 GOV-01/02/03 read-only governance-board routes (the MCP
// registry + per-row live probe, the skills lifecycle + audit ledger, the scheduler tasks
// + run history). Like the graph routes they are SPECIFIC method+path siblings under the
// "/api/" exclusion carve-out — NEVER a bare "/api/" (which would shadow the integrations
// proxy, T-28-02-05). All seven delegate to the AG-UI handler (the routes live on
// Server.Mux) and are interposed with RequireCapability(governance.read). These reads run
// after RequireAuth binds the principal. There is no write/PATCH/DELETE governance
// surface; onboarding create has its own identity.create gate.
//
// A route registered on Server.Mux but NOT listed here is unreachable — it falls through
// to the SPA catch-all and answers index.html, not the handler. The pair is the contract:
// agui registers, this mounts it behind its capability.
const (
	governanceMCPListRoute  = "GET /api/governance/mcp"
	governanceMCPProbeRoute = "GET /api/governance/mcp/{name}/probe"
	// The per-identity MCP authorization surface. The state read and the flow poll are
	// governance.read; starting a flow and revoking a grant are governance.write. The
	// callback is a READ mount on purpose: it is the human's own browser coming back
	// from a consent screen, and requiring the stronger capability there would refuse
	// the redirect of an operator who may read the board but not rewrite it — after
	// they had already consented at the provider.
	governanceMCPAuthStateRoute = "GET /api/governance/mcp/{name}/authorization"
	governanceMCPAuthFlowRoute  = "GET /api/governance/mcp/authorization/flow/{id}"
	governanceMCPAuthCbRoute    = "GET /api/governance/mcp/authorization/callback"
	governanceSkillsRoute       = "GET /api/governance/skills"
	governanceSkillsAuditRoute  = "GET /api/governance/skills/audit"
	governanceSkillsBodyRoute   = "GET /api/governance/skills/{name}/body"
	governanceSchedulerRoute    = "GET /api/governance/scheduler"
	governanceSchedRunsRoute    = "GET /api/governance/scheduler/{id}/runs"
)

// governance*WriteRoute are the Phase-29 MCPW-01/02/03 governance WRITE routes (install a
// recipe/custom server, edit one server's env in place, operator-trust-approve, reversibly
// enable/disable, confirm-remove). Each is a SPECIFIC method+path sibling under the "/api/"
// exclusion carve-out — NEVER a bare "/api/" (which would shadow /api/integrations/,
// Pitfall 5). All six delegate to the AG-UI handler (the routes live on Server.Mux) and are
// interposed with RequireCapability(governance.write) — strictly stronger than
// governance.read because a write can install a new MCP server. Go 1.22 longest-pattern
// precedence keeps these method+path siblings authoritative over the bare "/api/" carve-out
// and the "/" embed catch-all, and over the GET governance read siblings.
//
// CSRF posture (auth.go:18 Phase-29 note): these SPA writes are same-origin
// (credentials:'same-origin') under the SameSite=Strict session cookie — no cross-origin
// write path is introduced, so SameSite=Strict remains the sufficient CSRF control (the
// TanStack useMutation calls are same-origin); no double-submit token is added.
const (
	governanceMCPInstallRoute    = "POST /api/governance/mcp"
	governanceMCPEnvRoute        = "PATCH /api/governance/mcp/{name}/env"
	governanceMCPTrustRoute      = "POST /api/governance/mcp/{name}/trust"
	governanceMCPAuthStartRoute  = "POST /api/governance/mcp/{name}/authorization"
	governanceMCPAuthRevokeRoute = "DELETE /api/governance/mcp/{name}/authorization"
	governanceMCPEnableRoute     = "POST /api/governance/mcp/{name}/enable"
	governanceMCPDisableRoute    = "POST /api/governance/mcp/{name}/disable"
	governanceMCPRemoveRoute     = "DELETE /api/governance/mcp/{name}"
)

// governanceSkills*WriteRoute are the Phase-29 SKW-01/02/03 governance SKILLS WRITE routes
// (install a skill from a source field → the operator-origin /api/approvals pause; restore/
// archive across tabs; create/update/delete; the external-catalog search). Each is a SPECIFIC
// method+path sibling under the "/api/" exclusion carve-out — NEVER a bare "/api/" (Pitfall 5).
// All delegate to the AG-UI handler (routes on Server.Mux) and are interposed with
// RequireCapability(governance.write) — the SAME gate as the MCP write routes, and what makes
// the operator-origin paused_states mint safe (D-13 / T-04-19 widening is capability-scoped:
// only a governance.write caller can mint the install pause). The catalog GET inherits the same
// gate (it triggers an external network fetch when the discovery flag is on — privileged, not a
// bare read). Go 1.22 longest-pattern precedence keeps the {name}/restore + {name}/archive
// patterns authoritative over the {name} update/delete patterns.
const (
	governanceSkillInstallRoute  = "POST /api/governance/skills/install"
	governanceSkillRestoreRoute  = "POST /api/governance/skills/{name}/restore"
	governanceSkillArchiveRoute  = "POST /api/governance/skills/{name}/archive"
	governanceSkillCreateRoute   = "POST /api/governance/skills"
	governanceSkillUpdateRoute   = "PATCH /api/governance/skills/{name}"
	governanceSkillDeleteRoute   = "DELETE /api/governance/skills/{name}"
	governanceSkillCatalogRoute  = "GET /api/governance/skills/catalog"
	governanceSkillValidateRoute = "POST /api/governance/skills/validate"
)

// connect* are the cockpit "Connect" WhatsApp device-linking routes (connect_api.go). Each
// is a SPECIFIC method+path sibling under the "/api/" exclusion carve-out — NEVER a bare
// "/api/" (which would shadow /api/integrations/, Pitfall 5). All three delegate to the AG-UI
// handler (routes on Server.Mux) and are interposed with RequireCapability(governance.write):
// a logout drops the paired session and a QR scan links a device, so these are operator
// write-class actions — the SAME gate as the MCP/skills write routes, not a bare read. Go
// 1.22 longest-pattern precedence keeps each method+path sibling authoritative over the bare
// "/api/" carve-out and the "/" embed catch-all.
const (
	connectWhatsAppStatusRoute = "GET /api/connect/whatsapp/status"
	connectWhatsAppQRRoute     = "GET /api/connect/whatsapp/qr.png"
	connectWhatsAppLogoutRoute = "POST /api/connect/whatsapp/logout"
)

// connectPIM* are the cockpit "Connect Google Calendar" routes (connect_pim_api.go). Each is a
// SPECIFIC method+path sibling (with {id} path values) under the "/api/" carve-out — NEVER a bare
// "/api/" (Pitfall 5). All five delegate to the AG-UI handler (routes on Server.Mux) and are
// interposed with RequireCapability(governance.write): creating an account stores the operator's
// own Google OAuth client and a delete drops the linked account, so these are operator write-class
// actions — the SAME gate as the WhatsApp/MCP write routes. Go 1.22 longest-pattern precedence keeps
// each method+path sibling authoritative over the bare "/api/" carve-out and the "/" embed catch-all.
const (
	connectPIMAccountsListRoute   = "GET /api/connect/pim/accounts"
	connectPIMAccountsCreateRoute = "POST /api/connect/pim/accounts"
	connectPIMAccountDeleteRoute  = "DELETE /api/connect/pim/accounts/{id}"
	connectPIMAccountStatusRoute  = "GET /api/connect/pim/accounts/{id}/status"
	connectPIMGoogleStartRoute    = "GET /api/connect/pim/accounts/{id}/google/start"
	connectPIMLogoutRoute         = "POST /api/connect/pim/accounts/{id}/logout"
	connectPIMDeviceStartRoute    = "POST /api/connect/pim/accounts/{id}/auth/start"
	connectPIMAuthStatusRoute     = "GET /api/connect/pim/accounts/{id}/auth/status"
	connectPIMAuthCancelRoute     = "POST /api/connect/pim/accounts/{id}/auth/cancel"
)

const (
	settingsTelegramCheckRoute  = "POST /api/settings/telegram/check"
	settingsTelegramLinkRoute   = "POST /api/settings/telegram/link"
	settingsTelegramStatusRoute = "GET /api/settings/telegram/{sessionToken}/status"
)

// identityCreateCapability is the capability_grants name the onboarding CREATE mutations
// (start + provision) are gated on (ONBD-01a / D-04, parity with agentRunCapability). The
// seeded `local` identity holds the '*' wildcard so it passes; the name becomes load-
// bearing for provisioned identities (which never get '*' nor identity.create unless the
// creator explicitly grants it AND holds it). It mirrors the agui-side const of the same
// value (onboarding_provision.go) so the gate name is one truth across the mount + the
// service re-check.
const identityCreateCapability = "identity.create"

// onboarding* are the Phase-28 ONBD-01/02 onboarding wizard routes. start + provision are
// the CREATE mutations — interposed with RequireCapability(identity.create) exactly like
// POST /agent/run (the method+path-specific pattern wins Go 1.22 longest-pattern precedence
// and fires AFTER RequireAuth binds the principal). status, the seed-form submit and
// telegram-status are self-scoped and inherit RequireAuth from the whole-mux wrap with NO
// capability gate. All five are SPECIFIC method+path siblings under the "/api/" exclusion
// carve-out — NEVER a bare "/api/" (which would shadow /api/integrations/, T-28-05).
const (
	onboardingStatusRoute        = "GET /api/onboarding/status"
	onboardingProfileSubmitRoute = "POST /api/onboarding/profile"
	onboardingStartRoute         = "POST /api/onboarding/start"
	onboardingProvisionRoute     = "POST /api/onboarding/{sessionToken}/provision"
	onboardingTgStatusRoute      = "GET /api/onboarding/{sessionToken}/telegram-status"
)

// profile* are the operator profile editor (profile_api.go). Self-scoped like the
// onboarding status read — no id in the path, no way to name another identity — so they
// carry no capability gate and inherit RequireAuth from the whole-mux wrap.
const (
	profileGetRoute = "GET /api/profile"
	profilePutRoute = "PUT /api/profile"
)

// approvalsResolveRoute is the mutating resume/decline/cancel endpoint (APRV-02).
// Resuming or cancelling another thread's (possibly background) run is privileged
// (Security V4 / T-25-07), so it is interposed with RequireCapability exactly like
// "POST /agent/run": the method+path-specific pattern wins Go 1.22 longest-pattern
// precedence and the gate fires AFTER RequireAuth has bound the principal.
const approvalsResolveRoute = "POST /api/approvals/{token}/resolve"

const assetsRoutePrefix = "/api/assets"
const assetsSubtreeRoute = "/api/assets/"

// The file-manager subtree (/api/filemanager/{files,direct,upload}). Like every other
// backend surface it must be registered HERE as well as on the AG-UI mux: the parent mux
// falls everything it does not recognise through to the embedded SPA, so a route that
// exists only on the inner mux answers the SPA's 404 with an HTML body — which is exactly
// what the live cockpit showed ("Unexpected non-whitespace character after JSON").
// Read and write both inherit RequireAuth from the whole-mux wrap; the handlers scope every
// operation to the authenticated principal's own bucket, so no capability gate is added.
const fileManagerRoutePrefix = "/api/filemanager"
const fileManagerSubtreeRoute = "/api/filemanager/"

const (
	assetsPresignRoute  = "POST /api/assets/presign"
	assetsFinalizeRoute = "POST /api/assets/{id}/finalize"
	assetsPromoteRoute  = "POST /api/assets/{id}/promote"
	assetsRetryRoute    = "POST /api/assets/{id}/retry"
	assetsDeleteRoute   = "DELETE /api/assets/{id}"
)
