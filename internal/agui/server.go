package agui

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	runtimereadiness "github.com/chetto1983/aura/internal/readiness"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// maxRunBodyBytes caps the POST /agent/run request body (Tampering/DoS guard,
// T-12-12). The RunAgentInput payload is small (a thread id + a short message
// history); 1 MiB is generous headroom while bounding a hostile body before it
// reaches the SDK decoder.
const maxRunBodyBytes = 1 << 20

var errUnsupportedUserMessageContent = errors.New("agui: last user message content must be a non-empty string; multimodal input is not supported on this endpoint")

// ServerConfig carries the AG-UI server knobs the daemon resolves from config
// (AURA_AGUI_*). BufferCap is the cap of the per-connection SSE
// pump channel (drop-on-full, never block the Loop, T-12-09). A non-positive
// BufferCap falls back to the fanout default. SSEHeartbeatSec is the idle SSE-comment
// keepalive interval in seconds (fix-plan 1.3 Tier A, AURA_AGUI_SSE_HEARTBEAT_SEC);
// unlike BufferCap, <=0 DISABLES the heartbeat entirely rather than falling back to a
// default — see heartbeatIntervalFromConfig (server_sse.go).
// ReadinessProbes are the required-dependency checks /readyz runs (O-05/AP-14):
// /healthz stays a cheap LIVENESS check, /readyz reflects whether the required
// backends (Postgres + memory) are reachable. An empty list reports ready (the
// daemon was started without gated deps).
type ServerConfig struct {
	BufferCap       int
	SSEHeartbeatSec int
	HealthCheck     func(context.Context) error
	HealthDetails   func() map[string]any
	ReadinessProbes []ReadinessProbe
	ReadinessState  *runtimereadiness.Snapshot
	// SharePublicEnabled is the WEBSHARE-02/03 org kill-switch (AURA_SHARE_PUBLIC_ENABLED,
	// config.ShareConfig.PublicEnabled) re-checked INSIDE handleShareCreate (share_api.go,
	// plan 37F-10) — the R-08 gate that survives loopback dev, where RequireCapability
	// (auth.go:282) returns `next` unchanged because SecretConfigured is false. Default false
	// (fail-closed): a zero-value ServerConfig must never allow a public mint.
	SharePublicEnabled bool
	// Detached-run knobs (fix-plan 1.3 Tier B, AURA_AGUI_RUN_*): RunDetach gates the
	// context.WithoutCancel producer path (default false = today's request-scoped
	// runs); the ints size the RunRegistry via NewRunRegistry (runregistry.go), where
	// non-positive values fall back to the design defaults (2048/180s/3600s/16) —
	// the bufferCap convention, not the heartbeat's <=0-disables.
	RunDetach          bool
	RunBufferEvents    int
	RunLingerSec       int
	RunMaxWallclockSec int
	RunMaxLive         int
}

// Runner is the narrow agent-driver surface the server consumes (D-A2-02; *runner.Runner
// satisfies it implicitly). Turn drives one round over a thread and yields the agent
// Event stream; SubmitAnswers resolves protocol-native HITL resume entries (resolved→accept,
// cancelled→cancel) before the Turn. Declared consumer-side so the server depends only on
// the two methods it calls, not the whole Runner — and so unit tests pass scripted fakes.
type Runner interface {
	Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error]
	SubmitAnswers(ctx context.Context, answers map[string]runner.ResponseInput) (int, error)
	// SubmitAnswer resolves ONE pause and returns the runner's ResolveDirective — the
	// continue-vs-outcome decision the approval center renders instead of re-deriving it
	// client-side (Phase A: one resolver, transports only render). SubmitAnswers stays for the
	// protocol-native Resume[] path, which resolves a whole map at once and needs no directive.
	SubmitAnswer(ctx context.Context, token string, resp runner.ResponseInput) (runner.ResolveDirective, error)
	NewConversation(ctx context.Context) (string, error)
	// TurnBranch is the D-09 / CHAT-05 re-run-from-a-point: it drives a fresh agent
	// round over the SELECTED branch path (leafSeq) with no fresh user message — the
	// branch's turns were already persisted by ForkBranch. *runner.Runner satisfies it.
	TurnBranch(ctx context.Context, convID string, leafSeq int) iter.Seq2[*agent.Event, error]
	// DeleteConversationLifecycle is the single owner-scoped conversation-delete entry point
	// (MUSR-05 / D-22): it cancels active work, expires pauses, evicts session tools,
	// terminates background jobs, THEN deletes persistence — returning rows-affected (0 = not
	// owned → 403/404) so the DELETE route never performs a raw store delete. *runner.Runner
	// satisfies it.
	DeleteConversationLifecycle(ctx context.Context, identityID, convID string) (int64, error)
}

type threadTryLocker interface {
	TryLockThread(ctx context.Context, threadID string) (func(), bool)
}

// ApprovalStore is the narrow cross-thread HITL-read surface the approval center
// consumes (APRV-01 / D-04; *askuser.Store satisfies it implicitly). ListPendingAll
// aggregates every still-pending pause across ALL conversations in total order
// (priority DESC, created_at ASC, token ASC). Declared consumer-side so the server
// depends only on the one method the approvals read calls — the three-action resolve
// goes through the Runner (SubmitAnswers), not this store. A Server with no
// ApprovalStore wired answers the approvals routes 503 (the read is optional wiring;
// the daemon composition root sets it via SetApprovalStore).
type ApprovalStore interface {
	ListPendingAll(ctx context.Context, limit int) ([]askuser.Pending, error)
	// Phase 36 (MUSR-01 / D-06) owner-scoped surface: ListPendingAllForIdentity is the
	// principal-scoped approval queue; GetByTokenForIdentity is the ownership gate the
	// resolve handler consults (a hit ⇒ owned, proceed; a miss ⇒ 404). The unscoped
	// GetByToken that used to sit here for the 403-vs-404 split is gone with migration
	// 0089: no read can see another identity's pause, so the probe could only ever miss.
	ListPendingAllForIdentity(ctx context.Context, identityID string, limit int) ([]askuser.Pending, error)
	GetByTokenForIdentity(ctx context.Context, token, identityID string) (askuser.Pending, error)
}

// Server is the minimal AG-UI HTTP gateway (Slice 8b): POST /agent/run streams a
// translated agent turn as SSE, GET /threads/{id}/messages returns the persisted
// history as a MESSAGES_SNAPSHOT JSON body. It is the thinnest glue over EXISTING
// seams — the narrow Runner + ConversationStore, the translator, and the SDK SSE
// writer. The bind is hardcoded loopback by the daemon (auth deferred this phase,
// amendment #35); the loopback bind IS the compensating control (T-12-08).
type Server struct {
	run        Runner
	conv       ConversationStore
	operations operationRegistry
	approvals  ApprovalStore
	// approvalGrants serves the durable "always approve" grants (amendment #127). nil ⇒ the
	// grant routes answer 503, exactly like the pending read without an ApprovalStore.
	approvalGrants approvalGrantStore
	assets         AssetService
	ownerExports   ExportDestination
	// share is the WEBSHARE-02/03 share-lifecycle API (plan 37F-10) the share route
	// handlers call; nil until SetShareService wires it (D-A2-02 narrow seam).
	share            ShareService
	files            FileBrowser
	fileNames        FileNamer
	fileObjects      FileObjectOpener
	fileWrites       FileObjectWriter
	fileOps          FileOperations
	images           ImageFetcher
	graph            GraphView
	governance       GovernanceProviders
	governanceWrite  GovernanceWriteProviders
	mcpAuth          MCPAuthorizationProvider
	settings         settingsStore
	audit            auditReader
	idAdmin          identityAdmin
	telegramProbe    TelegramBotProbe
	onboarding       OnboardingService
	onboardingStatus OnboardingStatusSource
	// profiles is the operator profile read/write model (profile_api.go). The onboarding
	// seed writes it once; this is how it gets edited afterwards.
	profiles      ProfileEditor
	bootstrap     BootstrapService
	passwordReset *PasswordResetService
	idgen         IDGenerator
	// whatsappBridgeURL is the aura-whatsapp bridge management REST base URL the cockpit
	// connect routes forward to (AURA_WHATSAPP_BRIDGE_URL via SetWhatsAppBridge). Empty
	// (unwired) → the three /api/connect/whatsapp/* routes answer 503.
	whatsappBridgeURL string
	// calendarMCPURL/calendarMCPToken wire the aura-pim-mcp sidecar's token-gated /admin REST
	// API the cockpit calendar connect routes forward to (AURA_PIM_MCP_URL + ADMIN_TOKEN via
	// SetCalendarMCP). The token is injected server-side as Authorization: Bearer and NEVER
	// leaks to the client. An empty URL → the /api/connect/pim/* routes answer 503 (graceful).
	calendarMCPURL   string
	calendarMCPToken string
	// runs is the detached-run session registry (fix-plan 1.3 Tier B). Nil = flag
	// off (AURA_AGUI_RUN_DETACH unset/false) = today's request-scoped run path and
	// hidden resume/cancel routes; wired via SetRunRegistry only when the flag is on.
	runs          *RunRegistry
	cfg           ServerConfig
	readinessRuns readinessProbeCoordinator
	// probeTimeout bounds a single live MCP probe (GOV-01). Zero falls back to
	// defaultProbeTimeout (3s); tests shrink it to exercise the deadline-honoring path
	// quickly. Kept off the constructor so production uses the 3s default.
	probeTimeout time.Duration

	// heartbeatInterval is the resolved idle SSE-comment ticker period (fix-plan 1.3
	// Tier A), set at construction from cfg.SSEHeartbeatSec via heartbeatIntervalFromConfig
	// (server_sse.go). Zero disables the heartbeat (streamSSE allocates no ticker). Tests
	// override it directly for millisecond-granularity intervals — cfg.SSEHeartbeatSec is
	// seconds-only, mirroring the probeTimeout override seam above.
	heartbeatInterval time.Duration

	// tts/stt are the 37C web-voice clients injected via SetVoice (D-13) as narrow
	// seams (voice_api.go) so the handlers unit-test with no network. A nil client ⇒
	// that capability is absent: its POST answers 503 and GET /api/voice/capabilities
	// reports it false (D-11). ttsMaxChars is the soft rune cap POST /api/tts truncates
	// to (AURA_TTS_MAX_CHARS, default 4096, 37C-02).
	tts         ttsSynthesizer
	stt         sttTranscriber
	ttsMaxChars int

	// reasoningCaps is the active model's advertised reasoning-effort set (37E / WEBMODEL-01,
	// D-13), wired via SetReasoningCapabilitySource at the composition root. Stage-2 of
	// handleRun's effort governance AND handleReasoningCapabilities both read it from its
	// in-memory TTL cache — never a per-turn fetch. Nil ⇒ Stage-2 is skipped (a syntactically
	// valid fixed level passes) and the endpoint returns the safe floor {auto,off} (never 503).
	// reasoningBackend is the non-secret UI label ("openrouter"|"llamacpp"|"") the endpoint
	// reports; it is injected alongside the source (llm.ReasoningTarget is known only at boot).
	reasoningCaps    llm.ReasoningCapabilitySource
	reasoningBackend string

	// contextWindow is the active model's total context window (tokens), sourced from
	// llm.Config.ContextWindow (AURA_MODEL_CONTEXT_WINDOW) and wired via SetContextWindow at
	// the composition root. GET /api/me reports it so the cockpit footer gauge (RuntimeFooter)
	// shows the LIVE window instead of falling back to its DeepSeek-V4 1M default
	// (footerMetrics.DEFAULT_CONTEXT_WINDOW). Zero until wired — the DTO then carries 0 and the
	// frontend keeps its own fallback.
	contextWindow int

	// mcpViews holds the `ui://` documents mounted MCP servers served at boot, and
	// mcpSandboxOrigin is the DIFFERENT origin the cockpit must frame them from
	// (AURA_MCP_SANDBOX_ORIGIN). Both are wired by SetMCPViews; until then the view
	// route answers 503 — see mcp_views_api.go for why an unset or same-as-host
	// origin is a refusal rather than a degraded render.
	mcpViews         *mcp.ViewCatalog
	mcpSandboxOrigin string
	// mcpViewCaller runs the tools/call a rendered view asks for. Nil until wired;
	// the call route then answers 503, so a host that renders views but wired no
	// caller refuses the request instead of half-answering it.
	mcpViewCaller MCPViewToolCaller
}

// NewServer builds the gateway over the supplied driver + store + config. The
// IDGenerator is the default uuid-v4 minter; tests inject a deterministic one via
// the exported field for stable frame ids. The cross-thread approval read store is
// wired separately via SetApprovalStore (optional, kept off the constructor so the
// existing NewServer callers/tests stay unchanged — D-A2-02 narrow seams).
func NewServer(run Runner, conv ConversationStore, cfg ServerConfig) *Server {
	return &Server{
		run:               run,
		conv:              conv,
		idgen:             NewIDGenerator(),
		cfg:               cfg,
		heartbeatInterval: heartbeatIntervalFromConfig(cfg.SSEHeartbeatSec),
	}
}

// SetApprovalStore wires the cross-thread HITL read store (APRV-01). It is set by the
// daemon composition root after NewServer; until set, the approvals read route answers
// 503 (the resolve route only needs the Runner and works regardless).
func (s *Server) SetApprovalStore(store ApprovalStore) { s.approvals = store }

// SetRunRegistry wires the detached-run RunSession registry (fix-plan 1.3 Tier B,
// AURA_AGUI_RUN_DETACH). It is set by the daemon composition root ONLY when the
// detach flag is on; until set (nil), handleRun keeps today's byte-identical
// request-scoped path and the run resume/cancel routes hide themselves (404) —
// D-A2-02 narrow seam, mirroring SetApprovalStore.
func (s *Server) SetRunRegistry(registry *RunRegistry) { s.runs = registry }

// SetOperationRegistry installs the process-wide durable mutation registry.
// Keeping this as a narrow consumer-side seam lets tests leave it nil while the
// daemon protects every route inventoried by httpMutationRoutes.
func (s *Server) SetOperationRegistry(registry operationRegistry) { s.operations = registry }

// SetAssetService wires the upload/finalize/list asset API used by web and channels.
func (s *Server) SetAssetService(service AssetService) { s.assets = service }

// SetOwnerExportDestination wires durable owner-scoped archive storage used by
// export-delete and resumable downloads.
func (s *Server) SetOwnerExportDestination(destination ExportDestination) {
	s.ownerExports = destination
}

// SetShareService wires the WEBSHARE-02/03 share-lifecycle API (plan 37F-10) the share route
// handlers call. Set by the daemon composition root after NewServer (cmd/aura/
// serve_webui_share.go, plan 37F-12); until set, every share route answers 503 — mirroring
// SetAssetService (D-A2-02 narrow seam, existing NewServer callers unchanged).
func (s *Server) SetShareService(service ShareService) { s.share = service }

// SetImageProxy wires the SSRF-safe image fetcher (D-09) the /api/image-proxy route
// delegates to. Set by the daemon composition root after NewServer (the *web.Client
// already wired for web_search/web_fetch); until set, the route answers 503. Kept off
// the constructor so existing NewServer callers/tests stay unchanged (D-A2-02).
func (s *Server) SetImageProxy(images ImageFetcher) { s.images = images }

// SetGraphView wires the read-only graph view the /api/graph/schema +
// /api/graph/query routes delegate to. Set by the daemon composition root after
// NewServer (NewArcadeGraphView over the per-identity memory database); until set, both
// routes answer 503 (a missing graph store must not abort serve boot). Kept off the
// constructor so existing NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetGraphView(gv GraphView) { s.graph = gv }

// SetReasoningCapabilitySource wires the active model's reasoning-capability source (37E /
// WEBMODEL-01, D-13) plus its non-secret backend label, both selected at boot by
// llm.ReasoningTarget. It backs Stage-2 of the /agent/run effort governance AND the
// GET /api/composer/reasoning-capabilities endpoint, which share the one in-memory TTL-cached
// set (no per-turn fetch). Set by the daemon composition root after NewServer; a nil source
// degrades to the safe floor {auto,off} (never 503), mirroring SetSettingsStore.
func (s *Server) SetReasoningCapabilitySource(src llm.ReasoningCapabilitySource, backend string) {
	s.reasoningCaps = src
	s.reasoningBackend = backend
}

// SetContextWindow wires the active model's context window (tokens) so GET /api/me can
// report it to the cockpit footer gauge. Set by the daemon composition root after NewServer
// from llm.Config.ContextWindow; until set it stays 0 (existing NewServer callers/tests
// unchanged, D-A2-02 narrow seam).
func (s *Server) SetContextWindow(tokens int) { s.contextWindow = tokens }

// parseEffortSymbol maps a Composer UI effort SYMBOL to the internal llm.ReasoningEffort,
// returning (effort, isFixed, ok). It is the WEBMODEL-03 no-bypass gate (T-37E-06-ENUM): the
// client sends ONLY one of the seven symbols, never a ReasoningConfig. "" and "auto" are valid
// with no override (isFixed=false → today's adaptive path, D-04); off/low/mid/high/extra/max are
// fixed levels — the UI symbols mid/extra/max translate to the internal medium/xhigh/max HERE —
// and anything else is ok=false so handleRun returns 400 (enum-injection rejected).
func parseEffortSymbol(symbol string) (effort llm.ReasoningEffort, isFixed, ok bool) {
	switch symbol {
	case "", "auto":
		return "", false, true
	case "off":
		return llm.ReasoningEffortNone, true, true
	case "low":
		return llm.ReasoningEffortLow, true, true
	case "mid":
		return llm.ReasoningEffortMedium, true, true
	case "high":
		return llm.ReasoningEffortHigh, true, true
	case "extra":
		return llm.ReasoningEffortXHigh, true, true
	case "max":
		return llm.ReasoningEffortMax, true, true
	default:
		return "", false, false
	}
}

// Mux registers routes using Go 1.22+ method-pattern routing (no chi/gorilla).
// The cockpit and privileged APIs are same-origin only and emit no CORS grants.
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /agent/run", s.handleRun)
	// Fix-plan 1.3 Tier B run resume + explicit-Stop (amendment #90 points 3-4).
	// Colocated with their handlers (server_run_resume.go); both answer 404 while
	// the registry is nil (flag off — the surface hides itself entirely).
	mux.HandleFunc("GET /agent/runs/{runID}/events", s.handleRunEvents)
	mux.HandleFunc("POST /agent/runs/{runID}/cancel", s.handleRunCancel)
	mux.HandleFunc("GET /threads/{id}/messages", s.handleMessages)
	// DISP-05/D-09 image-proxy: a same-origin SSRF-safe relay for web_result
	// thumbnails/favicons. Mounted under /api/ so it inherits the parent-mux
	// RequireAuth whole-origin gate (cmd/aura/serve_webui.go); never an open relay.
	mux.HandleFunc("GET /api/image-proxy", s.handleImageProxy)
	// CHAT-02 conversation-management subtree (Phase 25). The parent-mux mount
	// behind RequireAuth lives in cmd/aura/serve_webui.go; here the routes are
	// colocated with their handlers so the agui Server.Mux answers them.
	s.registerConversationRoutes(mux)
	// APRV-01/02/03 approval-center routes (Phase 25 plan 25-02). Colocated with their
	// handlers; the parent-mux mount behind RequireAuth (+ RequireCapability on the
	// mutating resolve) lives in cmd/aura/serve_webui.go.
	s.registerApprovalRoutes(mux)
	s.registerAssetRoutes(mux)
	// WEBSHARE-02/03 (Phase 37F plan 37F-10): the eight share-lifecycle routes across three
	// trust boundaries — owner-scoped CRUD (POST/GET /api/shares, PATCH .../snapshot, DELETE),
	// the D-10 bearer-within-auth internal reads (GET .../data, .../asset/{assetID} —
	// RequireAuth ONLY, no capability, no owner predicate), and the unauthenticated public
	// token routes (GET /s/{token}/data, /s/{token}/asset/{id} — the ONLY unauthenticated
	// handlers in this package). Colocated with their handlers (share_api.go/
	// share_api_internal.go/share_api_public.go); the parent-mux mount (RequireAuth
	// whole-origin + the isPublicShareRoute allowlist admitting /s/... unauthenticated) lives
	// in cmd/aura/serve_webui_share.go (plan 37F-12) — none of that wiring is in this file.
	// share.public is NOT a mount-level RequireCapability wrap (a single POST /api/shares
	// entry answers both the internal and public tier via the JSON body, and Go's ServeMux
	// cannot dispatch on body content) — it is an IN-HANDLER check inside
	// handleShareCreate itself (share_api.go, 37F-13/WEBSHARE-04 SC4 row 8), over the same
	// idAdmin.HasCapability seam RequireCapability would call.
	s.registerShareRoutes(mux)
	// WEBVOICE-01/02/03 web-voice routes (37C-03): POST /api/tts (audio/mpeg + soft
	// char cap), POST /api/stt (transcribe-and-discard, D-08 — no asset/DB row), GET
	// /api/voice/capabilities (SELF-scoped presence probe, never 503). Colocated with
	// their handlers (voice_api.go); the parent-mux mounts (the two POSTs behind
	// agentRunCapability, capabilities RequireAuth-only) live in serve_webui_voice.go.
	s.registerVoiceRoutes(mux)
	// WEBSKILL-01 composer skill-picker read route (37D-02): GET /api/composer/skills, the
	// global active-skills snapshot behind plain RequireAuth (D-03/D-04). Colocated with its
	// handler (composer_api.go); the parent-mux mount (bare aguiHandler, RequireAuth-only —
	// NOT governance.read) lives in cmd/aura/serve_webui_composer.go.
	s.registerComposerRoutes(mux)
	// MCP Apps view read route: GET /api/mcp/view, the `ui://` document a mounted
	// server declared, behind the same plain RequireAuth the composer routes inherit.
	// Colocated with its handler (mcp_views_api.go).
	s.registerMCPViewRoutes(mux)
	s.registerFileRoutes(mux)
	// GRAPH-01 read-only graph-explorer routes (Phase 27 plan 27-02): GET /api/graph/schema
	// + POST /api/graph/query. Colocated with their handlers; the parent-mux mount behind
	// RequireAuth (no RequireCapability — read-only milestone) lives in
	// cmd/aura/serve_webui.go.
	s.registerGraphRoutes(mux)
	// GOV-01/02/03 read-only governance-board routes (Phase 28 plan 28-02): the six
	// /api/governance/* GETs (MCP list + per-row probe, skills list + audit, scheduler
	// tasks + run history). Colocated with their handlers; the parent-mux mount behind
	// RequireAuth (no RequireCapability — read-only) lives in cmd/aura/serve_webui.go.
	s.registerGovernanceRoutes(mux)
	// The per-identity MCP authorization routes (start/poll/callback/revoke) sit beside
	// the board's reads: same prefix, same capability mounts in cmd/aura.
	s.registerMCPAuthorizationRoutes(mux)
	// MCPW-01/02/03 governance WRITE routes (Phase 29 plan 29-02): POST /api/governance/mcp
	// (install) + PATCH .../{name}/env + POST .../{name}/{trust,enable,disable} + DELETE
	// .../{name}. Colocated with their handlers; the parent-mux mount behind
	// RequireCapability(governance.write) lives in cmd/aura/serve_webui.go.
	s.registerGovernanceWriteRoutes(mux)
	// SETTINGS-01 cockpit Settings page: GET /api/settings (effective model-backend
	// knobs, secrets redacted) + PUT/DELETE /api/settings/{key}. Colocated with their
	// handlers; the parent-mux mount (RequireCapability(governance.read) on GET,
	// governance.write on PUT/DELETE) lives in cmd/aura/serve_webui.go.
	s.registerSettingsRoutes(mux)
	// MUSR-01 admin/user distinction (Phase 36 plan 10, D-03/D-26/D-28): GET /api/me
	// (self-scoped capabilities the SPA reads to hide admin surfaces) + the
	// /api/admin/{identities,audit} + capability grant/revoke surface. Colocated with
	// their handlers; the parent-mux mount (RequireAuth on /api/me, RequireCapability(
	// governance.write) on the admin routes) lives in cmd/aura/serve_webui_musr.go.
	s.registerAuditRoutes(mux)
	// ONBD-01/02 onboarding routes: POST /api/onboarding/start + /{token}/provision (the
	// identity-provisioning saga) and GET /api/onboarding/status + POST /api/onboarding/
	// profile + GET /{token}/telegram-status (self-scoped). Colocated with their handlers;
	// the parent-mux mount lives in cmd/aura/serve_webui.go and gates ONLY start + provision
	// with RequireCapability(identity.create) — status, the Amendment #95 seed-form submit
	// and telegram-status carry NO capability gate and inherit RequireAuth from the whole-mux
	// wrap, because each acts only on the caller's own identity.
	s.registerOnboardingRoutes(mux)
	// The operator profile editor: GET/PUT /api/profile, self-scoped like the onboarding
	// status read (no id in the path), so RequireAuth from the whole-mux wrap is the gate.
	s.registerProfileRoutes(mux)
	// Authula password-reset routes: public credential-recovery JSON endpoints. These
	// handlers intentionally do not read a principal; Authula/session auth happens after
	// the reset completes.
	s.registerBootstrapRoutes(mux)
	s.registerPasswordResetRoutes(mux)
	// Cockpit "Connect" device-linking proxy (connect_api.go): the three
	// /api/connect/whatsapp/{status,qr.png,logout} routes forward to the aura-whatsapp
	// bridge management REST. Colocated with their handlers; the parent-mux mount behind
	// RequireCapability(governance.write) (operator write-class actions) lives in
	// cmd/aura/serve_webui.go.
	s.registerConnectRoutes(mux)
	// Cockpit "Connect" calendar account-linking proxy (connect_pim_api.go): the five
	// /api/connect/pim/* routes forward to the aura-pim-mcp sidecar's token-gated /admin REST
	// API, injecting the admin Bearer token server-side. Colocated with their handlers; the
	// parent-mux mount behind RequireCapability(governance.write) (operator write-class actions
	// — an operator enters their own Google OAuth client and links an account) lives in
	// cmd/aura/serve_webui.go.
	s.registerConnectPIMRoutes(mux)
	if s.operations == nil {
		return mux
	}
	guarded := http.NewServeMux()
	for pattern, meta := range httpMutationRoutes {
		guarded.Handle(pattern, s.idempotencyMutation(mux, meta))
	}
	guarded.Handle("/", mux)
	return guarded
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]any{"ok": true}
	if s.cfg.HealthDetails != nil {
		maps.Copy(body, s.cfg.HealthDetails())
	}
	if s.cfg.HealthCheck != nil {
		if err := s.cfg.HealthCheck(r.Context()); err != nil {
			status = http.StatusServiceUnavailable
			body["ok"] = false
			body["error"] = SanitizeString(err.Error())
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("agui: encode healthz response", "err", err)
	}
}

// handleRun (POST /agent/run) lives in server_run.go together with its detached-path
// twin (server_run_detach.go) — moved out on the fix-plan 1.3 Tier B touch to keep
// this file (routing + config + seams) under the 600-LOC ceiling.

// handleMessages resolves the thread (404) and returns the persisted history as a
// MESSAGES_SNAPSHOT JSON body (one-shot read, NOT SSE — OQ2). Each persisted
// llm.Message is projected to an events.Message.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	// A malformed thread id (non-UUID) is definitionally not an existing thread —
	// 404 before the store round-trip rather than a 500 from the id parse failure
	// (T-12-11; the live smoke's `does-not-exist` chokepoint).
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	// Owner-scoped thread resolution (MUSR-01 / D-06): a foreign/absent thread is 404 before
	// the history read, so a MESSAGES_SNAPSHOT never leaks another identity's conversation.
	if _, err := s.conv.GetForIdentity(ctx, id, scopedIdentityID(ctx)); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	hist, err := s.conv.LoadHistory(scopedCtx(ctx), id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// D-06: emit the display-aware MESSAGES_SNAPSHOT — each tool-result turn re-derives
	// its DisplayPayload through the SAME normalizer the live stream uses, so a reopened
	// thread renders typed displays identically to live. The envelope is byte-compatible
	// with the SDK MESSAGES_SNAPSHOT plus the additive per-tool-call `display` key the
	// cockpit replay reads.
	snap := projectDisplaySnapshot(hist)
	// Amendment #91 (fix-plan 1.12) display rehydration: merge the persisted per-turn
	// CoT onto the assistant answer messages so the ReasoningDrawer survives reload.
	// Fail-soft: reasoning is additive display data — a read failure degrades to a
	// drawer-less snapshot (the NULL-column posture), never a 500 for the whole thread.
	if reasonings, rerr := s.conv.ListTurnReasoning(scopedCtx(ctx), id); rerr != nil {
		slog.Warn("agui: list turn reasoning (serving snapshot without it)", "thread", id, "err", rerr)
	} else {
		attachTurnReasoning(&snap, reasonings)
	}
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		slog.Warn("agui: encode messages snapshot", "err", err)
	}
}

// The per-connection SSE pump (streamSSE / pumpSend / isLifecycleFrame / bufferCap) lives in
// server_sse.go to keep this file — routing + the run/messages handlers + the 37E effort
// governance — under the 600-LOC ceiling.
