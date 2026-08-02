// serve_agui.go builds and wires the AG-UI gateway server object for `aura serve`
// (Slice 8b): the agui.NewServer config literal, its /readyz probe set, and the
// composition-root SetX wiring calls that need only the daemon's already-composed
// seams (no auth/onboarding state — bootServe wires the onboarding/bootstrap/
// password-reset providers in afterward, once the Authula provider exists). Split out
// of serve.go (refactor-on-touch, CLAUDE.md 600-LOC ceiling) when the idle SSE-comment
// heartbeat knob (fix-plan 1.3 Tier A) landed a new ServerConfig field there — a pure
// mechanical move, no behavior change.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/readiness"
	"github.com/chetto1983/aura/internal/web"
)

// wireAGUIServer builds the agui.Server over the daemon's already-composed seams
// (Runner, ConversationStore, object store, share/document/governance/voice/reasoning
// providers) and returns it wired, together with the detached-run RunRegistry (nil
// unless AURA_AGUI_RUN_DETACH=true) so runServe can own its shutdown. bootServe wires
// the remaining auth-dependent providers (onboarding/bootstrap/password-reset)
// afterward, once the Authula provider exists — those stay in bootServe rather than
// here so this function needs no auth state (D-A2-02 narrow seam).
func wireAGUIServer(chat *chatEnv, store *cron.Store, scheduler *cron.Scheduler, readinessState *readiness.Snapshot, ownerExports agui.ExportDestination, shareAPI agui.ShareService, objectStore objectstore.Store) (*agui.Server, *agui.RunRegistry) {
	// The AG-UI gateway (Slice 8b) reuses the already-composed Runner + conversations
	// store; it mounts on the same daemon and shares the graceful ctx-cancel drain
	// (Assumption A3). The bind may now be non-loopback (WEB-02/D-06 lifted the
	// hardcoded-loopback restriction); config.GuardWebBind (called later in bootServe
	// before httpSrv is built) refuses a non-loopback AURA_AGUI_BIND unless Authula
	// auth or AURA_WEB_TRUST_PROXY is set, so the auth boundary — not a hardcoded
	// bind — is the compensating control.
	serverCfg := agui.ServerConfig{
		BufferCap:       chat.cfg.AGUIBufferCap,
		SSEHeartbeatSec: chat.cfg.AGUISSEHeartbeatSec,
		// Detached-run knobs (fix-plan 1.3 Tier B, amendment #90), resolved like
		// SSEHeartbeatSec; the registry construction below is what activates them.
		RunDetach:          chat.cfg.AGUIRun.Detach,
		RunBufferEvents:    chat.cfg.AGUIRun.BufferEvents,
		RunLingerSec:       chat.cfg.AGUIRun.LingerSec,
		RunMaxWallclockSec: chat.cfg.AGUIRun.MaxWallclockSec,
		RunMaxLive:         chat.cfg.AGUIRun.MaxLive,
		ReadinessState:     readinessState,
		HealthDetails: func() map[string]any {
			// WEB-04/D-07: the read-only health panel reads bind + build from this
			// EXISTING /healthz body — no new backend endpoint. Both are non-secret
			// (a bind address and a build version, not a DSN/credential).
			buildVersion, _, _ := buildInfo()
			details := map[string]any{
				"bind_address":  chat.cfg.AGUIBind,
				"build_version": buildVersion,
			}
			last := scheduler.LastTick()
			if last.IsZero() {
				details["scheduler_last_tick"] = ""
			} else {
				details["scheduler_last_tick"] = last.Format(time.RFC3339)
			}
			return details
		},
		// /readyz reflects the daemon's REQUIRED backends (O-05/AP-14): Postgres (the
		// open pool) and memory. When any required dep is unreachable /readyz answers
		// 503 so an orchestrator stops routing to this instance; /healthz stays cheap
		// process liveness.
		ReadinessProbes: serveReadinessProbes(chat),
	}
	aguiServer := agui.NewServer(chat.run, chat.conv, serverCfg)
	// Fix-plan 1.3 Tier B (amendment #90 point 1): the detached-run registry exists
	// ONLY behind the flag — a nil registry keeps the pre-Tier-B request-scoped
	// /agent/run byte-identical and the resume/cancel routes hidden (404). Returned
	// to runServe so drainShutdown Close()s it (cancel-walk every detached run +
	// reaper join) BEFORE the HTTP drain.
	var runRegistry *agui.RunRegistry
	if chat.cfg.AGUIRun.Detach {
		runRegistry = agui.NewRunRegistry(serverCfg)
		aguiServer.SetRunRegistry(runRegistry)
	}
	aguiServer.SetOperationRegistry(chat.operations)
	aguiServer.SetAssetService(chat.assets)
	aguiServer.SetOwnerExportDestination(ownerExports)
	aguiServer.SetShareService(shareAPI)
	aguiServer.SetDocumentCatalog(buildDocumentCatalogService(chat))
	aguiServer.SetDocumentEvents(buildDocumentEventService(chat))
	aguiServer.SetStorageOrphans(buildStorageOrphanService(chat, objectStore))
	// Wire the cross-thread HITL approval read (APRV-01 / D-04). Without this the
	// GET /api/approvals poll answers 503 and the whole approval center is dead in
	// production — SetApprovalStore was only ever called in tests, so the live daemon
	// shipped with s.approvals == nil. chat.pause is the same askuser.Store the Runner
	// resumes through, so the badge/list read the identical pending set.
	aguiServer.SetApprovalStore(chat.pause)
	// Wire the DISP-05/D-09 image-proxy fetcher: a fresh web.Client reusing the SAME
	// SSRF-hardened transport web_search/web_fetch use (hostname blocklist → DNS-pin →
	// classify → image content-type allowlist + size cap). Without this the
	// GET /api/image-proxy route answers 503 and the cockpit's web_result thumbnails/
	// favicons never load. It mounts behind the RequireAuth whole-origin gate (the
	// parent mux below), so it is never an open relay.
	aguiServer.SetImageProxy(web.NewClient(chat.cfg))
	// Wire the cockpit "Connect" WhatsApp device-linking bridge URL (AURA_WHATSAPP_BRIDGE_URL,
	// default the sibling aura-whatsapp sidecar). The three /api/connect/whatsapp/* routes
	// forward to its management REST; an empty value leaves them at 503 (graceful — a stack
	// without the sidecar boots fine). The routes mount behind RequireCapability(governance.
	// write) in serve_webui.go, so the proxy is never an open relay.
	aguiServer.SetWhatsAppBridge(chat.cfg.WhatsAppBridgeURL)
	// Wire the cockpit "Connect Google Calendar" admin-proxy: the aura-pim-mcp sidecar base URL
	// (AURA_PIM_MCP_URL) + the /admin Bearer token (AURA_PIM_MCP_ADMIN_TOKEN). The five
	// /api/connect/pim/* routes forward to its token-gated /admin REST, injecting the token
	// server-side (it never reaches the client); an empty URL leaves them at 503 (graceful — a
	// stack without the calendar sidecar boots fine). The routes mount behind RequireCapability(
	// governance.write) in serve_webui.go, so the proxy is never an open relay.
	aguiServer.SetCalendarMCP(chat.cfg.CalendarMCPURL, chat.cfg.CalendarMCPAdminToken)
	// Wire the read-only graph explorer over ArcadeDB (buildArcadeGraphView). It is
	// SCHEMA-ONLY: one identity's type catalogue, never a drawable canvas — see
	// agui.ArcadeGraphView. Best-effort — an unconfigured memory server or a missing
	// tenant secret leaves the two /api/graph/* routes at 503 and MUST NOT abort serve
	// boot. Nothing to close: the view builds a stateless HTTP client per request.
	if view := buildArcadeGraphView(chat.cfg); view != nil {
		aguiServer.SetGraphView(view)
	}
	// Wire the Phase-28 read-only governance boards (GOV-01/02/03): the MCP registry +
	// per-row live probe, the skills lifecycle + audit ledger, the scheduler tasks + run
	// history. Built best-effort over the existing seams (the managed MCP config, the
	// skills loader/stage-reader/audit store, the cron Store) — a provider that cannot be
	// constructed is left nil so its board answers 503, MUST NOT abort boot (the
	// SetGraphView best-effort precedent). The reads inherit RequireAuth from the parent
	// mux; no capability gate (read-only).
	aguiServer.SetGovernanceProviders(buildGovernanceProviders(chat.cfg, chat.pool, store))
	wireSettingsProviders(aguiServer, chat.pool)
	// Wire the 37C web-voice providers (WEBVOICE-01/02/03, D-12/D-13): a DEDICATED mp3
	// web TTSClient (Format="mp3", distinct from Telegram's opus client) + a cloud-only
	// STTClient, each built ONLY when its cloud model is configured, injected via
	// SetVoice. With neither model set the three voice routes degrade (POSTs 503,
	// GET /api/voice/capabilities reports {false,false}); the Telegram opus path
	// (multimodalConfig) is untouched.
	wireVoiceProviders(aguiServer, chat.cfg)
	// Wire the 37E reasoning-capability source (WEBMODEL-01/D-13): the active model's advertised
	// effort set, selected by llm.ReasoningTarget and warmed once at boot (never blocking). It
	// backs the composer reasoning-capabilities endpoint AND Stage-2 of the /agent/run effort
	// governance from one TTL cache; an unrecognized backend leaves it nil so both degrade to the
	// safe floor {auto,off}.
	wireReasoningCapabilities(aguiServer, chat.cfg.LLM)
	// Wire the Phase-36 (MUSR-01 / D-26/D-28) admin/user-distinction stores: the per-user
	// audit read (over the identity-keyed mcp_audit/skill_audit/tool_invocations ledgers)
	// and the capability-management + roster seam (the SAME *identity.Store the auth
	// boundary + CLI use). Without these, GET /api/me + the /api/admin/* routes answer 503.
	aguiServer.SetAuditStore(agui.NewPgAuditStore(chat.pool))
	aguiServer.SetIdentityAdmin(chat.identity)
	aguiServer.SetContextWindow(chat.cfg.LLM.ContextWindow)
	// Wire the Phase-29 MCP WRITE provider (MCPW-01/02/03): install/env-edit/trust/enable/
	// disable/remove, each atomic with its mcp_audit row (WriteConfigWithAudit) and re-probed
	// for the live tool count. Built best-effort over the shared pool + the managed-config
	// path; a nil pool or unresolvable path leaves the provider nil so the six write routes
	// answer 503, MUST NOT abort boot (the SetGovernanceProviders precedent). The mounts
	// (RequireCapability(governance.write)) live in serve_webui.go.
	// The Phase-29 SKILLS WRITE provider (SKW-01/02/03): install (stage→validate→pending +
	// install (Claude-Code parity: fetch→validate→ACTIVATE directly, no approval pause),
	// restore(409-collision-guard)/archive, create/update/delete. Built best-effort over the
	// SAME newSkillWriter wiring the CLI + model path use. A nil pool / missing skills dir leaves
	// it nil so the routes answer 503 — never aborts boot. Both write providers are wired in ONE
	// SetGovernanceWriteProviders call so the bundle stays the single seam (no second setter).
	var mcpWrite agui.MCPWriteProvider
	if mw, werr := buildMCPWriteProvider(chat.pool); werr != nil {
		slog.Warn("aura serve: governance mcp write unavailable", "err", werr)
	} else {
		mcpWrite = mw
	}
	skillsWrite := buildSkillsWriteProvider(chat.cfg, chat.pool)
	if mcpWrite != nil || skillsWrite != nil {
		aguiServer.SetGovernanceWriteProviders(agui.GovernanceWriteProviders{MCP: mcpWrite, Skills: skillsWrite})
	}
	return aguiServer, runRegistry
}

// serveReadinessProbes builds the /readyz probe set over the daemon's required
// backends (O-05/AP-14): Postgres (the open pool's Ping) and memory (a functional
// search through the mounted MCP). The shared global deadline in the agui handler
// bounds them. A dependency handle that is absent (nil pool) is omitted rather
// than reported as a false failure.
func serveReadinessProbes(chat *chatEnv) []agui.ReadinessProbe {
	probes := make([]agui.ReadinessProbe, 0, 2)
	if chat.pool != nil {
		probes = append(probes, agui.ReadinessProbe{
			Name:  "postgres",
			Code:  readiness.CodePostgresUnavailable,
			Check: func(ctx context.Context) error { return chat.pool.Ping(ctx) },
		})
	}
	if probe, required := memoryReadinessProbe(chat); required {
		probes = append(probes, probe)
	}
	return probes
}
