// serve_webui.go wires the embedded operator SPA into the running daemon (FND-02 +
// WEB-01). The single-binary host serves the committed Vite build (internal/webui) at
// "/", additively, on the SAME loopback http.Server that already carries the AG-UI
// gateway — the embed mount adds no new listener and no new bind (T-23-06).
//
// Precedence is the whole design: a Go 1.22 http.ServeMux gives a longer/more
// specific registered pattern priority over the catch-all "/", so registering the
// explicit AG-UI route prefixes (/healthz, /readyz, /debug/vars, /metrics,
// /agent/run, /threads/) to the AG-UI handler and the integrations proxy subtree
// (/api/integrations/) ahead of "/" keeps those routes authoritative while everything
// else falls through to the embed.
//
// WEB-01: the "/" catch-all is an SPA-fallback, not a bare static tree. An unknown
// CLIENT route returns index.html (React Router resolves deep links); an excluded
// API/agent/health prefix returns a real 404 so the SPA shell never leaks to an API
// client (SC1). The exclusion set is SINGLE-SOURCED here — fallbackExcludedPrefixes()
// derives it from the AG-UI namespaces + the integrations subtree + the forward-compat
// "/api/" carve-out and passes a copy into webui.Handler, so the parent-mux
// registration and the fallback exclusion cannot drift (Pitfall 6 / T-24-08).
//
// The "/api/" carve-out is an EXCLUSION prefix ONLY — it is NOT registered on the mux.
// "/api/integrations/" is already mounted; a second mux.Handle("/api/", ...) would
// collide with / shadow that subtree (T-24-07). Adding "/api/" only to the fallback
// exclusion makes "/api/anything" 404 today and lets a real /api/* route register
// tomorrow without touching the fallback.
//
// internal/webui stays leaf-level (it imports no other internal/* package), an
// invariant scripts/agui_boundary_check.sh enforces via a dependency-closure
// assertion on the webui package.
package main

import (
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/webui"
)

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
		"/agent/",   // whole AG-UI agent namespace (mux registers the exact /agent/run)
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
// proxy, T-28-02-05). All six delegate to the AG-UI handler (the routes live on
// Server.Mux) and are interposed with RequireCapability(governance.read). These reads run
// after RequireAuth binds the principal. There is no write/PATCH/DELETE governance
// surface; onboarding create has its own identity.create gate.
const (
	governanceMCPListRoute     = "GET /api/governance/mcp"
	governanceMCPProbeRoute    = "GET /api/governance/mcp/{name}/probe"
	governanceSkillsRoute      = "GET /api/governance/skills"
	governanceSkillsAuditRoute = "GET /api/governance/skills/audit"
	governanceSchedulerRoute   = "GET /api/governance/scheduler"
	governanceSchedRunsRoute   = "GET /api/governance/scheduler/{id}/runs"
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
	governanceMCPInstallRoute = "POST /api/governance/mcp"
	governanceMCPEnvRoute     = "PATCH /api/governance/mcp/{name}/env"
	governanceMCPTrustRoute   = "POST /api/governance/mcp/{name}/trust"
	governanceMCPEnableRoute  = "POST /api/governance/mcp/{name}/enable"
	governanceMCPDisableRoute = "POST /api/governance/mcp/{name}/disable"
	governanceMCPRemoveRoute  = "DELETE /api/governance/mcp/{name}"
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
	governanceSkillInstallRoute = "POST /api/governance/skills/install"
	governanceSkillRestoreRoute = "POST /api/governance/skills/{name}/restore"
	governanceSkillArchiveRoute = "POST /api/governance/skills/{name}/archive"
	governanceSkillCreateRoute  = "POST /api/governance/skills"
	governanceSkillUpdateRoute  = "PATCH /api/governance/skills/{name}"
	governanceSkillDeleteRoute  = "DELETE /api/governance/skills/{name}"
	governanceSkillCatalogRoute = "GET /api/governance/skills/catalog"
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
// and fires AFTER RequireAuth binds the principal). step + telegram-status are non-create
// (the session is authz'd at start) and inherit RequireAuth from the whole-mux wrap with NO
// capability gate. All four are SPECIFIC method+path siblings under the "/api/" exclusion
// carve-out — NEVER a bare "/api/" (which would shadow /api/integrations/, T-28-05).
const (
	onboardingStatusRoute          = "GET /api/onboarding/status"
	onboardingProfileStartRoute    = "POST /api/onboarding/profile/start"
	onboardingProfileCompleteRoute = "POST /api/onboarding/{sessionToken}/profile"
	onboardingStartRoute           = "POST /api/onboarding/start"
	onboardingStepRoute            = "POST /api/onboarding/{sessionToken}/step"
	onboardingProvisionRoute       = "POST /api/onboarding/{sessionToken}/provision"
	onboardingTgStatusRoute        = "GET /api/onboarding/{sessionToken}/telegram-status"
)

// approvalsResolveRoute is the mutating resume/decline/cancel endpoint (APRV-02).
// Resuming or cancelling another thread's (possibly background) run is privileged
// (Security V4 / T-25-07), so it is interposed with RequireCapability exactly like
// "POST /agent/run": the method+path-specific pattern wins Go 1.22 longest-pattern
// precedence and the gate fires AFTER RequireAuth has bound the principal.
const approvalsResolveRoute = "POST /api/approvals/{token}/resolve"

const assetsRoutePrefix = "/api/assets"
const assetsSubtreeRoute = "/api/assets/"
const documentsRoutePrefix = "/api/documents"
const documentsSubtreeRoute = "/api/documents/"

const (
	assetsPresignRoute  = "POST /api/assets/presign"
	assetsFinalizeRoute = "POST /api/assets/{id}/finalize"
	assetsPromoteRoute  = "POST /api/assets/{id}/promote"
	assetsRetryRoute    = "POST /api/assets/{id}/retry"
	assetsDeleteRoute   = "DELETE /api/assets/{id}"
)

// newServeHandler builds the parent http.Handler for the daemon's single loopback
// server: the AG-UI route prefixes delegate to aguiHandler (the agui Server.Mux), the
// integrations proxy subtree mounts ahead of "/", and the catch-all "/" serves the
// SPA-fallback embed host (unknown client route -> index.html; excluded prefix -> 404).
// A webui.Handler failure (an embed sub error, which a committed dist makes
// unreachable) is returned so bootServe fails the daemon boot cleanly rather than
// mounting a half-wired host.
//
// WEB-03 (D-03/D-04): the whole returned subtree is wrapped in agui.RequireAuth so the
// origin is private when Authula auth is configured — the public-path exceptions (the
// login route + its assets + GET /healthz) are handled INSIDE the middleware, not by
// leaving routes unwrapped. POST /logout remains for cookie clearing. The only mutating
// route, POST /agent/run, is additionally interposed with
// agui.RequireCapability ahead of the AG-UI prefix loop (Go 1.22 method-pattern
// precedence makes "POST /agent/run" win over the bare "/agent/run") so the capability
// gate fires AFTER RequireAuth has bound the principal.
func newServeHandler(aguiHandler http.Handler, auth agui.AuthDeps, authulaProvider credentialProvider) (http.Handler, error) {
	static, err := webui.Handler(fallbackExcludedPrefixes())
	if err != nil {
		return nil, fmt.Errorf("webui handler: %w", err)
	}
	mux := http.NewServeMux()
	authulaEnabled := credentialProviderConfigured(authulaProvider)
	// The embedded Authula provider serves all credential flows under /auth/* (login,
	// totp/verify, logout, csrf token issuance). Registered as a subtree ahead of "/",
	// it wins Go 1.22 longest-pattern precedence over the embed catch-all; RequireAuth
	// marks the prefix public (AuthBasePath below) so the routes are reachable before a
	// session exists.
	if authulaEnabled {
		mux.Handle(authBasePath+"/", authulaProvider.Handler())
		auth.AuthBasePath = authBasePath
	}
	var bootstrapProvider bootstrapAvailabilityProvider
	if candidate, ok := authulaProvider.(bootstrapAvailabilityProvider); ok {
		bootstrapProvider = candidate
	}
	mux.HandleFunc("GET "+authConfigRoute, newAuthConfigHandler(bootstrapProvider))
	mux.Handle(bootstrapOperatorRoute, aguiHandler)
	mux.Handle(passwordResetStartRoute, aguiHandler)
	mux.Handle(passwordResetQuestionRoute, aguiHandler)
	mux.Handle(passwordResetVerifyRoute, aguiHandler)
	mux.Handle(passwordResetCompleteRoute, aguiHandler)
	// The mutating route is interposed with the capability gate FIRST: "POST /agent/run"
	// is a more specific pattern than the bare "/agent/run" the prefix loop registers, so
	// Go 1.22 longest-pattern precedence routes the POST through RequireCapability →
	// aguiHandler while other methods/paths under /agent/run fall to the prefix entry.
	mux.Handle("POST /agent/run", agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	for _, prefix := range aguiRoutePrefixes {
		mux.Handle(prefix, aguiHandler)
	}
	// Public credential endpoint for clearing the old Aura session cookie. Authula's own
	// logout flow lives under /auth/* on the provider subtree.
	mux.HandleFunc("POST /logout", auth.LogoutHandler())
	// The CHAT-02 conversation-management subtree (Phase 25) delegates to the AG-UI
	// handler, which carries the /api/conversations/ routes on its own Server.Mux. It
	// is registered as the SPECIFIC subtree — NEVER a bare "/api/", which would shadow
	// "/api/integrations/" below (T-24-07 / T-25-05). Go 1.22 longest-pattern precedence
	// keeps both "/api/conversations/" and "/api/integrations/" authoritative side by
	// side over the "/" embed catch-all, and the "/api/" fallback exclusion already
	// returns this as a backend route (no fallback change needed). RequireAuth wraps the
	// whole mux below, so the new reads inherit the whole-origin gate for free.
	// Both the trailing-slash subtree (the {id} routes + /search) AND the exact
	// "/api/conversations" (the list GET, no trailing slash) are registered so the list
	// endpoint is not 301-redirected into the subtree and lost.
	mux.Handle(conversationsRoutePrefix, aguiHandler)
	mux.Handle(conversationsListRoute, aguiHandler)
	// The D-09 mutating branch re-runs (edit / branch-select) are capability-gated like
	// POST /agent/run. Their method+path-specific patterns win longest-pattern precedence
	// over the "/api/conversations/" subtree above, so the gate fires on the re-run while
	// the read GET /branches stays under RequireAuth only.
	mux.Handle(branchEditRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(branchSelectRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	// The Phase-25 approval center (APRV-01/02/03) mounts beside the conversation
	// subtree. The mutating resolve is capability-gated exactly like POST /agent/run —
	// resuming/declining/cancelling another thread's run is privileged (V4/T-25-07) —
	// while the read inherits RequireAuth from the whole-mux wrap. Both delegate to the
	// AG-UI handler, which carries the routes on its Server.Mux (registerApprovalRoutes).
	// Method+path precedence keeps the resolve gate authoritative over the read path.
	mux.Handle(approvalsResolveRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(approvalsListRoute, aguiHandler)
	mux.Handle(assetsPresignRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsFinalizeRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsPromoteRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsRetryRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsDeleteRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsRoutePrefix, aguiHandler)
	mux.Handle(assetsSubtreeRoute, aguiHandler)
	mux.Handle(documentsRoutePrefix, aguiHandler)
	mux.Handle(documentsSubtreeRoute, aguiHandler)
	// The DISP-05/D-09 image-proxy delegates to the AG-UI handler (route on Server.Mux).
	// A read GET, it inherits RequireAuth from the whole-mux wrap below (never an open
	// relay) — no capability gate. Method+path-specific so it wins longest-pattern
	// precedence over the "/" embed catch-all.
	mux.Handle(imageProxyRoute, aguiHandler)
	// The Phase-27 GRAPH-01 graph-explorer routes delegate to the AG-UI handler (routes
	// on Server.Mux). Read-only, so they inherit RequireAuth from the whole-mux wrap below
	// with NO RequireCapability (contrast the mutating POST /agent/run + branch re-runs).
	// Method+path-specific so they win longest-pattern precedence over the "/" embed
	// catch-all; the "/api/" fallback exclusion already returns them as backend routes.
	mux.Handle(graphSchemaRoute, aguiHandler)
	mux.Handle(graphQueryRoute, aguiHandler)
	// The Phase-28 GOV-01/02/03 governance-board reads delegate to the AG-UI handler
	// (routes on Server.Mux) behind an explicit governance.read capability. Method+path-
	// specific so they win longest-pattern precedence over the "/" embed catch-all; the
	// "/api/" fallback exclusion already returns them as backend routes.
	mux.Handle(governanceMCPListRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceMCPProbeRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceSkillsRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceSkillsAuditRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceSchedulerRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceSchedRunsRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	// The Phase-29 MCPW-01/02/03 governance WRITE routes delegate to the AG-UI handler
	// (routes on Server.Mux) behind RequireCapability(governance.write) — strictly stronger
	// than governance.read (a write can install a new MCP server). Method+path-specific so
	// each wins Go 1.22 longest-pattern precedence over the bare "/api/" carve-out, the "/"
	// embed catch-all, AND the GET governance read siblings. CSRF: same-origin SameSite=Strict
	// covers these SPA writes (auth.go:18) — no cross-origin write path is introduced.
	mux.Handle(governanceMCPInstallRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceMCPEnvRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceMCPTrustRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceMCPEnableRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceMCPDisableRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceMCPRemoveRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	// The Phase-29 SKW-01/02/03 governance SKILLS WRITE routes — same governance.write gate.
	// The install mints the operator-origin /api/approvals pause (D-13); the gate is what keeps
	// that second paused_states writer capability-scoped. POST /api/governance/skills (create)
	// is method-distinct from the Phase-28 GET /api/governance/skills read board, so both
	// coexist under longest-pattern precedence; the {name}/restore + {name}/archive patterns win
	// over the {name} update/delete patterns; /skills/catalog + /skills/install are more specific
	// than the bare /skills subtree.
	mux.Handle(governanceSkillInstallRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceSkillRestoreRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceSkillArchiveRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceSkillCreateRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceSkillUpdateRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceSkillDeleteRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceSkillCatalogRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	// The SETTINGS-01 cockpit Settings routes delegate to the AG-UI handler (routes on
	// Server.Mux). GET is an operator read of the model-backend knobs (secrets redacted)
	// behind governance.read; PUT/DELETE mutate aura.settings behind governance.write — the
	// same gate as the MCP/skills writes. Method+path-specific so each wins Go 1.22
	// longest-pattern precedence over the bare "/api/" carve-out.
	mux.Handle("GET /api/settings", agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle("PUT /api/settings/{key}", agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle("DELETE /api/settings/{key}", agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(settingsTelegramCheckRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(settingsTelegramLinkRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(settingsTelegramStatusRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	// The cockpit "Connect" WhatsApp device-linking routes delegate to the AG-UI handler
	// (routes on Server.Mux) behind RequireCapability(governance.write) — operator
	// write-class actions (logout drops the session, a QR scan links a device), so they
	// share the governance.write gate. Method+path-specific so each wins Go 1.22 longest-
	// pattern precedence over the bare "/api/" carve-out and the "/" embed catch-all.
	mux.Handle(connectWhatsAppStatusRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectWhatsAppQRRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectWhatsAppLogoutRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMAccountsListRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMAccountsCreateRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMAccountDeleteRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMAccountStatusRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMGoogleStartRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMLogoutRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMDeviceStartRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMAuthStatusRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(connectPIMAuthCancelRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	// The Phase-28 ONBD-01/02 onboarding wizard. start + provision are the CREATE
	// mutations: interposed with RequireCapability(identity.create) exactly like POST
	// /agent/run, so the gate fires AFTER RequireAuth binds the principal (an operator
	// without identity.create is 403 with no session/identity created). step +
	// telegram-status are non-create — the session is authz'd at start — so they delegate
	// straight to the AG-UI handler and inherit RequireAuth from the whole-mux wrap. All
	// four are method+path-specific so they win longest-pattern precedence over the "/"
	// embed catch-all; the "/api/" fallback exclusion already returns them as backend routes.
	mux.Handle(onboardingStatusRoute, aguiHandler)
	mux.Handle(onboardingProfileStartRoute, aguiHandler)
	mux.Handle(onboardingProfileCompleteRoute, aguiHandler)
	mux.Handle(onboardingStartRoute, agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
	mux.Handle(onboardingProvisionRoute, agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
	mux.Handle(onboardingStepRoute, aguiHandler)
	mux.Handle(onboardingTgStatusRoute, aguiHandler)
	// The Phase-36 (MUSR-01 / D-03/D-26/D-28) admin/user-distinction mounts live in
	// serve_webui_musr.go to keep this file under the 600-LOC ceiling: GET /api/me
	// (self-scoped, RequireAuth only) + the /api/admin/* surface behind
	// RequireCapability(governance.write).
	registerMUSRRoutes(mux, aguiHandler, auth)
	// The 37C web-voice mounts (WEBVOICE-01/02/03) live in serve_webui_voice.go to keep
	// this file under the 600-LOC ceiling: POST /api/tts + POST /api/stt behind
	// RequireCapability(agentRunCapability) (cost-bearing), GET /api/voice/capabilities
	// RequireAuth-only (a SELF-scoped presence probe, like meRoute).
	registerVoiceRoutes(mux, aguiHandler, auth)
	// The integrations admin proxy (cockpit connect data plane) mounts ahead of the
	// "/" embed catch-all; Go 1.22 longest-pattern precedence keeps it authoritative.
	// NOTE: "/api/" is deliberately NOT registered here — it lives only in the
	// fallback exclusion set, so registering it would collide with this subtree.
	mux.Handle(integrationsRoutePrefix, newIntegrationsProxy())
	mux.Handle("/", static)
	// The login page's static assets (the shared SPA bundle/styles, PWA, icons) must be
	// reachable before a session exists so the login form can render (D-03). webui owns
	// the embedded-asset truth, so wire its predicate into the gate rather than letting
	// the auth layer guess asset paths.
	auth.PublicAsset = webui.IsPublicAsset
	previousPublicRoute := auth.PublicRoute
	auth.PublicRoute = func(r *http.Request) bool {
		if r.Method == http.MethodGet && r.URL.Path == authConfigRoute {
			return true
		}
		if isPublicPasswordResetRoute(r) {
			return true
		}
		if isPublicBootstrapRoute(r) {
			return true
		}
		return previousPublicRoute != nil && previousPublicRoute(r)
	}
	// Wrap the WHOLE parent mux in the WEB-03 whole-origin gate (D-03). The public-path
	// exceptions are handled inside RequireAuth; a no-op pass-through when no secret is
	// configured keeps loopback dev unauthenticated.
	return agui.RequireAuth(mux, auth), nil
}

func isPublicBootstrapRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/auth/bootstrap/operator"
}

func isPublicPasswordResetRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/auth/password-reset/start",
		"/api/auth/password-reset/question",
		"/api/auth/password-reset/verify",
		"/api/auth/password-reset/complete":
		return true
	default:
		return false
	}
}
