// serve_webui.go wires the embedded operator SPA into the running daemon (FND-02 +
// WEB-01). The single-binary host serves the committed Vite build (internal/webui) at
// "/", additively, on the SAME loopback http.Server that already carries the AG-UI
// gateway — the embed mount adds no new listener and no new bind (T-23-06).
//
// Precedence is the whole design: a Go 1.22 http.ServeMux gives a longer/more
// specific registered pattern priority over the catch-all "/", so registering the
// explicit AG-UI route prefixes (/healthz, /readyz, /debug/vars, /metrics,
// /agent/run, /threads/) to the AG-UI handler ahead of "/" keeps those routes
// authoritative while everything else falls through to the embed.
//
// WEB-01: the "/" catch-all is an SPA-fallback, not a bare static tree. An unknown
// CLIENT route returns index.html (React Router resolves deep links); an excluded
// API/agent/health prefix returns a real 404 so the SPA shell never leaks to an API
// client (SC1). The exclusion set is SINGLE-SOURCED here — fallbackExcludedPrefixes()
// derives it from the AG-UI namespaces + the forward-compat "/api/" carve-out and
// passes a copy into webui.Handler, so the parent-mux
// registration and the fallback exclusion cannot drift (Pitfall 6 / T-24-08).
//
// The "/api/" carve-out is an EXCLUSION prefix ONLY — it is NOT registered on the mux.
// Adding it only to the fallback exclusion makes "/api/anything" 404 today and lets a
// real /api/* route register tomorrow without touching the fallback.
//
// internal/webui stays leaf-level (it imports no other internal/* package), an
// invariant scripts/agui_boundary_check.sh enforces via a dependency-closure
// assertion on the webui package.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/webui"
)

// newServeHandler builds the parent http.Handler for the daemon's single loopback
// server: the AG-UI route prefixes delegate to aguiHandler (the agui Server.Mux), and
// the catch-all "/" serves the SPA-fallback embed host (unknown client route ->
// index.html; excluded prefix -> 404).
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
	registerMCPOAuthRoutes(mux, authulaProvider)
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
	// is registered as the SPECIFIC subtree — NEVER a bare "/api/". Go 1.22
	// longest-pattern precedence keeps "/api/conversations/" authoritative over the
	// "/" embed catch-all, and the "/api/" fallback exclusion already
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
	mux.Handle(approvalGrantsRoute, aguiHandler)
	mux.Handle(approvalGrantsRevokeRoute, aguiHandler)
	mux.Handle(assetsPresignRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsFinalizeRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsPromoteRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsRetryRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsDeleteRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(assetsRoutePrefix, aguiHandler)
	mux.Handle(assetsSubtreeRoute, aguiHandler)
	mux.Handle(fileManagerRoutePrefix, aguiHandler)
	mux.Handle(fileManagerSubtreeRoute, aguiHandler)
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
	mux.Handle(governanceMCPAuthStateRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceMCPAuthFlowRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	// The OAuth callback is the ONE route here that carries no capability, because it
	// cannot: it is a cross-site top-level navigation from the provider's consent screen,
	// and the session cookie is `__Host-` + SameSite, so the browser does not send it on
	// that hop. Mounted behind a capability it did exactly what a missing session always
	// does — bounced the human to the login page, discarding a consent they had already
	// given (measured against Slack, 2026-08-24).
	//
	// What authenticates it instead is `state`: 128 bits of SDK-generated randomness,
	// single-use, TTL-bounded, and matched against a flow THIS deployment started. The
	// handler reads nothing else — no identity from context — and an unknown state gets a
	// 409, so the route grants nothing to anyone who cannot already produce the secret.
	// This is what LibreChat does too (api/server/routes/mcp.js: the callback carries no
	// auth middleware and resolves state → flow).
	mux.Handle(governanceMCPAuthCbRoute, aguiHandler)
	mux.Handle(governanceSkillsRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceSkillsAuditRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle(governanceSkillsBodyRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
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
	mux.Handle(governanceMCPAuthStartRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	mux.Handle(governanceMCPAuthRevokeRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
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
	mux.Handle(governanceSkillValidateRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))

	mountGovernanceSchedulerWriteRoutes(mux, aguiHandler, auth)
	// The SETTINGS-01 cockpit Settings routes delegate to the AG-UI handler (routes on
	// Server.Mux). GET is an operator read of the model-backend knobs (secrets redacted)
	// behind governance.read; PUT/DELETE mutate aura.settings behind governance.write — the
	// same gate as the MCP/skills writes. Method+path-specific so each wins Go 1.22
	// longest-pattern precedence over the bare "/api/" carve-out.
	mux.Handle("GET /api/settings", agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
	mux.Handle("PUT /api/settings/llm-profile", agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
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
	// without identity.create is 403 with no session/identity created). status, the
	// seed-form submit and telegram-status are self-scoped, so they delegate straight to
	// the AG-UI handler and inherit RequireAuth from the whole-mux wrap. All five are
	// method+path-specific so they win longest-pattern precedence over the "/" embed
	// catch-all; the "/api/" fallback exclusion already returns them as backend routes.
	mux.Handle(onboardingStatusRoute, aguiHandler)
	mux.Handle(onboardingProfileSubmitRoute, aguiHandler)
	mux.Handle(onboardingStartRoute, agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
	mux.Handle(onboardingProvisionRoute, agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
	mux.Handle(onboardingTgStatusRoute, aguiHandler)
	// The profile editor: what onboarding wrote once, the operator can revise forever.
	mux.Handle(profileGetRoute, aguiHandler)
	mux.Handle(profilePutRoute, aguiHandler)
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
	// The 37D WEBSKILL-01 composer skill-picker mount (GET /api/composer/skills) lives in
	// serve_webui_composer.go to keep this file under the 600-LOC ceiling: bare aguiHandler,
	// RequireAuth-only (like voiceCapabilitiesRoute/meRoute) — deliberately NOT
	// governance.read-gated so an ordinary identity gets the global picker list (D-03).
	registerComposerRoutes(mux, aguiHandler, auth)
	registerMCPViewRoutes(mux, aguiHandler, auth)
	// 37F WEBSHARE-02 share routes live in serve_webui_share.go to keep this file ≤600 LOC.
	registerShareRoutes(mux, aguiHandler, auth)
	mux.Handle("/", static)
	// The login page's static assets (the shared SPA bundle/styles, PWA, icons) must be
	// reachable before a session exists so the login form can render (D-03). webui owns
	// the embedded-asset truth, so wire its predicate into the gate rather than letting
	// the auth layer guess asset paths.
	auth.PublicAsset = webui.IsPublicAsset
	previousPublicRoute := auth.PublicRoute
	auth.PublicRoute = func(r *http.Request) bool {
		if isPublicMCPOAuthRoute(r) {
			return true
		}
		if r.Method == http.MethodGet && r.URL.Path == authConfigRoute {
			return true
		}
		if isPublicPasswordResetRoute(r) {
			return true
		}
		if isPublicBootstrapRoute(r) {
			return true
		}
		if isPublicShareRoute(r) {
			return true
		}
		// The MCP OAuth callback arrives as a cross-site top-level navigation from the
		// provider's consent screen, so the browser withholds the `__Host-` SameSite
		// session cookie on that one hop. Behind the gate it did what a missing session
		// always does — bounced the human to the login page and threw away a consent
		// they had just given at Slack (measured 2026-08-24). `state` authenticates it
		// instead: single-use, TTL-bounded, and matched against a flow this deployment
		// started; an unknown one gets a 409 and nothing else. LibreChat mounts its
		// equivalent with no auth middleware for the same reason.
		if r.Method == http.MethodGet && r.URL.Path == mcpOAuthCallbackAPIPath {
			return true
		}
		// The MCP Apps sandbox proxy is fetched from the SECOND origin and carries no
		// data of its own — everything it relays arrives by postMessage from the
		// cockpit. Gating it on a cookie scoped to the cockpit's origin would couple
		// the two origins the extension exists to keep apart.
		if r.Method == http.MethodGet && r.URL.Path == agui.MCPSandboxPath {
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

// reasoningCapsTTL is the TTL of one route snapshot's 37E reasoning-capability cache. A Settings
// route change replaces the whole source; within one snapshot the advertised effort set changes
// rarely, so a long TTL avoids a per-turn /models fetch.
const reasoningCapsTTL = 6 * time.Hour

// wireReasoningCapabilities injects the 37E reasoning-capability source (WEBMODEL-01 / D-13) into
// the agui Server after NewServer (called from the serve composition root). The source is selected
// by llm.ReasoningTarget: OpenRouter → a TTL-cached GET /models client; llama.cpp → the
// provider+ops-contract source; Ollama → its OpenAI-compatible effort set; any other
// backend → nil (the endpoint then degrades to the safe
// floor {auto,off}). The composer reasoning-capabilities endpoint AND Stage-2 of the /agent/run
// effort governance share this one cached source (no per-turn fetch). The cache is warmed once in
// a bounded fire-and-forget goroutine so the first hit is memory-served — serve boot NEVER blocks
// on the (possibly slow or unreachable) /models fetch.
func wireReasoningCapabilities(server *agui.Server, cfg llm.Config) {
	src := llm.NewReasoningCapabilitySource(cfg, reasoningCapsTTL)
	var backend string
	switch llm.ReasoningTarget(cfg.Provider, cfg.BaseURL) {
	case llm.ReasoningTargetOpenRouter:
		backend = "openrouter"
	case llm.ReasoningTargetLlamaCpp:
		backend = "llamacpp"
	case llm.ReasoningTargetOllama:
		backend = "ollama"
	}
	server.SetReasoningCapabilitySource(src, backend)
	if src == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _, _ = src.AllowedEfforts(ctx) // warm the TTL cache; result intentionally discarded
	}()
}
