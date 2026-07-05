package agui

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"iter"
	"log/slog"
	"net/http"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
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
// (AURA_AGUI_*). CORSPermissive gates the `Access-Control-Allow-Origin: *` header
// (default restrictive, T-12-13); BufferCap is the cap of the per-connection SSE
// pump channel (drop-on-full, never block the Loop, T-12-09). A non-positive
// BufferCap falls back to the fanout default.
// ReadinessProbes are the required-dependency checks /readyz runs (O-05/AP-14):
// /healthz stays a cheap LIVENESS check, /readyz reflects whether the required
// backends (PG + Neo4j) are reachable. An empty list reports ready (the daemon was
// started without gated deps).
type ServerConfig struct {
	CORSPermissive  bool
	BufferCap       int
	HealthCheck     func(context.Context) error
	HealthDetails   func() map[string]any
	ReadinessProbes []ReadinessProbe
}

// Runner is the narrow agent-driver surface the server consumes (D-A2-02; *runner.Runner
// satisfies it implicitly). Turn drives one round over a thread and yields the agent
// Event stream; SubmitAnswers resolves protocol-native HITL resume entries (resolved→accept,
// cancelled→cancel) before the Turn. Declared consumer-side so the server depends only on
// the two methods it calls, not the whole Runner — and so unit tests pass scripted fakes.
type Runner interface {
	Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error]
	SubmitAnswers(ctx context.Context, answers map[string]runner.ResponseInput) (int, error)
	NewConversation(ctx context.Context) (string, error)
	// TurnBranch is the D-09 / CHAT-05 re-run-from-a-point: it drives a fresh agent
	// round over the SELECTED branch path (leafSeq) with no fresh user message — the
	// branch's turns were already persisted by ForkBranch. *runner.Runner satisfies it.
	TurnBranch(ctx context.Context, convID string, leafSeq int) iter.Seq2[*agent.Event, error]
}

type threadTryLocker interface {
	TryLockThread(threadID string) (func(), bool)
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
	// resolve handler consults (a hit ⇒ owned, proceed; a miss ⇒ probe GetByToken for the
	// 403-vs-404 split). *askuser.Store satisfies these implicitly.
	ListPendingAllForIdentity(ctx context.Context, identityID string, limit int) ([]askuser.Pending, error)
	GetByTokenForIdentity(ctx context.Context, token, identityID string) (askuser.Pending, error)
	GetByToken(ctx context.Context, token string) (askuser.Pending, error)
}

// Server is the minimal AG-UI HTTP gateway (Slice 8b): POST /agent/run streams a
// translated agent turn as SSE, GET /threads/{id}/messages returns the persisted
// history as a MESSAGES_SNAPSHOT JSON body. It is the thinnest glue over EXISTING
// seams — the narrow Runner + ConversationStore, the translator, and the SDK SSE
// writer. The bind is hardcoded loopback by the daemon (auth deferred this phase,
// amendment #35); the loopback bind IS the compensating control (T-12-08).
type Server struct {
	run              Runner
	conv             ConversationStore
	approvals        ApprovalStore
	assets           AssetService
	documentCatalog  DocumentCatalogService
	documentEvents   DocumentEventService
	storageOrphans   StorageOrphanService
	images           ImageFetcher
	graph            GraphView
	governance       GovernanceProviders
	governanceWrite  GovernanceWriteProviders
	settings         settingsStore
	audit            auditReader
	idAdmin          identityAdmin
	telegramProbe    TelegramBotProbe
	onboarding       OnboardingService
	onboardingStatus OnboardingStatusSource
	bootstrap        BootstrapService
	passwordReset    *PasswordResetService
	idgen            IDGenerator
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
	cfg              ServerConfig
	// probeTimeout bounds a single live MCP probe (GOV-01). Zero falls back to
	// defaultProbeTimeout (3s); tests shrink it to exercise the deadline-honoring path
	// quickly. Kept off the constructor so production uses the 3s default.
	probeTimeout time.Duration
}

// NewServer builds the gateway over the supplied driver + store + config. The
// IDGenerator is the default uuid-v4 minter; tests inject a deterministic one via
// the exported field for stable frame ids. The cross-thread approval read store is
// wired separately via SetApprovalStore (optional, kept off the constructor so the
// existing NewServer callers/tests stay unchanged — D-A2-02 narrow seams).
func NewServer(run Runner, conv ConversationStore, cfg ServerConfig) *Server {
	return &Server{run: run, conv: conv, idgen: NewIDGenerator(), cfg: cfg}
}

// SetApprovalStore wires the cross-thread HITL read store (APRV-01). It is set by the
// daemon composition root after NewServer; until set, the approvals read route answers
// 503 (the resolve route only needs the Runner and works regardless).
func (s *Server) SetApprovalStore(store ApprovalStore) { s.approvals = store }

// SetAssetService wires the upload/finalize/list asset API used by web and channels.
func (s *Server) SetAssetService(service AssetService) { s.assets = service }

// SetImageProxy wires the SSRF-safe image fetcher (D-09) the /api/image-proxy route
// delegates to. Set by the daemon composition root after NewServer (the *web.Client
// already wired for web_search/web_fetch); until set, the route answers 503. Kept off
// the constructor so existing NewServer callers/tests stay unchanged (D-A2-02).
func (s *Server) SetImageProxy(images ImageFetcher) { s.images = images }

// SetGraphView wires the read-only Phase-27 graph normalizer (GRAPH-01) the
// /api/graph/schema + /api/graph/query routes delegate to. Set by the daemon composition
// root after NewServer (boot-time knowledge.Client → knowledge.NewGraphView); until set,
// both routes answer 503 (a missing graph client must not abort serve boot). Kept off the
// constructor so existing NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetGraphView(gv GraphView) { s.graph = gv }

// Mux registers the two routes using Go 1.22+ method-pattern routing (no chi/gorilla
// — matches the no-router codebase posture). When CORSPermissive is on (the dev knob)
// the handler is wrapped in withCORS so a browser cross-origin POST works end to end:
// the preflight OPTIONS is answered and ACAO is set on every response, including errors.
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /agent/run", s.handleRun)
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
	s.registerDocumentRoutes(mux)
	s.registerStorageOrphanRoutes(mux)
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
	// ONBD-01/02 onboarding wizard routes (Phase 28 plan 28-05): POST /api/onboarding/start
	// + /{token}/step + /{token}/provision + GET /{token}/telegram-status. Colocated with
	// their handlers; the parent-mux mount (RequireCapability(identity.create) on start +
	// provision, RequireAuth on step + telegram-status) lives in cmd/aura/serve_webui.go.
	s.registerOnboardingRoutes(mux)
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
	return s.withCORS(mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]any{"ok": true}
	if s.cfg.HealthDetails != nil {
		for k, v := range s.cfg.HealthDetails() {
			body[k] = v
		}
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

// handleRun parses RunAgentInput, resolves the thread (404), applies any protocol-native
// resume entries, drives Runner.Turn over the last user message, and streams the translated
// AG-UI events as SSE. The body is size-capped (T-12-12); a malformed/empty payload is a 400.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	req, err := decodeRunAgentRequest(json.NewDecoder(r.Body))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	in := req.RunAgentInput
	if err := ValidateRunInput(in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	// A syntactically-invalid thread id can never identify an existing conversation:
	// resolve it to 404 BEFORE the store round-trip so a malformed id (e.g. a non-UUID
	// "does-not-exist") returns a clean 404 instead of leaking the store's parse error
	// as a 500 (T-12-11 chokepoint; caught by the live agui smoke).
	if _, err := uuid.Parse(in.ThreadID); err != nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	// Owner-scoped thread resolution (MUSR-01 / D-06): a thread the caller does not own is
	// 404 (hide existence) — B can neither read nor DRIVE a turn on A's conversation.
	if _, err := s.conv.GetForIdentity(ctx, in.ThreadID, scopedIdentityID(ctx)); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	userMsg, err := lastUserMessage(in.Messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Prepend per-turn context blocks (attachments + the thread's searchable-doc catalog)
	// to the user turn; see buildTurnUserMessage (server_context.go).
	modelUserMsg, code, emsg := s.buildTurnUserMessage(ctx, r, in.ThreadID, req.Aura.AttachmentIDs, userMsg)
	if code != 0 {
		http.Error(w, emsg, code)
		return
	}

	var unlock func()
	if locker, ok := s.run.(threadTryLocker); ok {
		var locked bool
		unlock, locked = locker.TryLockThread(in.ThreadID)
		if !locked {
			http.Error(w, runner.ErrThreadBusy.Error(), http.StatusConflict)
			return
		}
		defer unlock()
		ctx = runner.WithThreadLockHeld(ctx)
	}

	if len(in.Resume) > 0 {
		if _, err := s.run.SubmitAnswers(ctx, resumeAnswers(in.Resume)); err != nil {
			http.Error(w, sanitizeErr(err), http.StatusBadRequest)
			return
		}
	}

	runID := "run-" + uuid.NewString()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// ACAO is set centrally by withCORS when CORSPermissive is on (WR-05) so it is present
	// on the error responses above too, not only this 200 stream.

	turn := s.run.Turn(ctx, in.ThreadID, modelUserMsg)
	if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg {
		if split, ok := s.run.(modelUserMessageRunner); ok {
			turn = split.TurnWithModelUserMessage(ctx, in.ThreadID, *userMsg, *modelUserMsg)
		}
	}
	// D-01: the cockpit SSE stream surfaces live REASONING_* delta text (showReasoning
	// =true). This is a deliberate operator override of the conservative redacted web
	// default, justified by the whole-origin-private single-operator cockpit (Phase 24
	// D-03): the ONLY viewer is the authenticated operator. This is the live cockpit
	// STREAM, distinct from trace persistence — the reasoning trace still does NOT
	// persist verbatim by default (HARDEN-05 is unchanged). The flip is cockpit-scoped:
	// Telegram has its OWN agui.Translate(…, t.deps.ShowReasoning) call site (a per-
	// channel config-driven flag, agui_subscriber.go) and the programmatic pump uses
	// Subscribe/NewFanout (client.go) — neither flows through handleRun, so neither
	// posture is affected by this true.
	s.streamSSE(ctx, w, Translate(in.ThreadID, runID, s.idgen, turn, true))
}

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
	hist, err := s.conv.LoadHistory(ctx, id)
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
	if err := json.NewEncoder(w).Encode(projectDisplaySnapshot(hist)); err != nil {
		slog.Warn("agui: encode messages snapshot", "err", err)
	}
}

// streamSSE pumps the translated event stream to the client over SSE through a
// cap-N buffered channel: the producer goroutine ranges the stream (the sole sender,
// drop+WARN on a full buffer so it never blocks the Loop, T-12-09) while the handler
// goroutine drains the channel onto the wire via the SDK writer. On client disconnect
// (ctx.Done) both unwind — the producer stops yielding, the channel closes, the drain
// loop returns (goleak-clean, Pitfall 4). A translated RUN_ERROR is sanitized at the
// translator boundary already; sanitizeErr is the belt-and-suspenders for the pump's
// own error frame.
func (s *Server) streamSSE(ctx context.Context, w http.ResponseWriter, stream iter.Seq2[events.Event, error]) {
	writer := sse.NewSSEWriter()
	out := make(chan events.Event, s.bufferCap())
	go func() {
		defer close(out)
		for ev, err := range stream {
			if err != nil {
				s.pumpSend(ctx, out, events.NewRunErrorEvent(sanitizeErr(err)))
				return
			}
			if !s.pumpSend(ctx, out, ev) {
				return
			}
		}
	}()

	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-out:
			if !ok {
				return
			}
			ev = redactEvent(ev)
			if err := writer.WriteEventWithType(ctx, w, ev, string(ev.Type())); err != nil {
				return // client gone — let the producer drain via ctx (Pitfall 4)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// pumpSend delivers one event to the SSE channel without ever blocking the producer
// indefinitely: deliver if there is room, abort on ctx-cancel. A run-lifecycle frame
// (RUN_STARTED/RUN_FINISHED/RUN_ERROR) that cannot fit falls back to a blocking send
// (still abortable on ctx-cancel) so the terminal frame is never dropped — an AG-UI
// consumer waits on RUN_FINISHED, and silently dropping it is a protocol violation, not
// graceful degradation (WR-01). A non-lifecycle delta that cannot fit is DROPPED with a
// WARN (T-12-09: the Loop must never stall on a slow client). Returns false only on
// ctx-cancel so the producer unwinds and closes the channel.
func (s *Server) pumpSend(ctx context.Context, out chan events.Event, ev events.Event) bool {
	if isLifecycleFrame(ev.Type()) {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	default:
		recordSSEDropped()
		slog.Warn("agui server: SSE client slow, dropping event", "type", ev.Type())
		return true
	}
}

// isLifecycleFrame reports whether an event is a protocol boundary frame that cannot
// be dropped under backpressure. Dropping START/END/RESULT/CUSTOM/SNAPSHOT frames can
// leave delivered deltas without their protocol parent, causing events.ValidateSequence
// to reject the surviving sub-sequence. Shared by the SSE pump and in-process fanout.
func isLifecycleFrame(t events.EventType) bool {
	switch t {
	case events.EventTypeRunStarted,
		events.EventTypeRunFinished,
		events.EventTypeRunError,
		events.EventTypeTextMessageStart,
		events.EventTypeTextMessageEnd,
		events.EventTypeToolCallStart,
		events.EventTypeToolCallEnd,
		events.EventTypeToolCallResult,
		events.EventTypeReasoningStart,
		events.EventTypeReasoningMessageStart,
		events.EventTypeReasoningMessageEnd,
		events.EventTypeReasoningEnd,
		events.EventTypeCustom,
		events.EventTypeStateDelta,
		events.EventTypeStateSnapshot:
		return true
	default:
		return false
	}
}

// bufferCap resolves the per-connection SSE channel cap, falling back to the fanout
// default when the config knob is non-positive.
func (s *Server) bufferCap() int {
	if s.cfg.BufferCap > 0 {
		return s.cfg.BufferCap
	}
	return fanoutBuffer
}
