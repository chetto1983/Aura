package agui

import "github.com/chetto1983/aura/internal/llm"

// The dependency seams the daemon composition root wires after NewServer. Each is kept off
// the constructor so existing NewServer callers and tests stay unchanged (D-A2-02); until one
// is set, the routes that need it answer 503 (or 404 where the feature hides itself).

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

// steerPusher is the narrow write-side contract handleRunSteer needs from the shared
// steer queue — defined HERE (agui-local), never importing internal/steer's concrete
// PostgresStore directly (Phase 51 plan 02, D-06's "adapt the new type to the shipped
// contract" — this seam is the adaptation point). *steer.PostgresStore satisfies it by
// construction (identical Push signature); a test fake needs neither a live Postgres
// connection nor a *steer.PostgresStore to exercise handleRunSteer's status-code ladder.
type steerPusher interface {
	Push(conv, source, text string) error
}

// SetSteerInbox wires the shared mid-turn steer inbox (amendment #132,
// AURA_AGUI_RUN_STEER). It is set by the daemon composition root ONLY when
// the steer flag is on, mirroring SetRunRegistry: until set (nil),
// handleRunSteer answers 404 rather than accepting a POST nothing drains.
func (s *Server) SetSteerInbox(inbox steerPusher) { s.steer = inbox }

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

// SetDataProxy wires the SSRF-safe data fetcher the /api/fetch route delegates to.
// Set by the daemon composition root after NewServer (the same *web.Client already
// wired for web_search/web_fetch and the image proxy); until set, the route answers
// 503. Kept off the constructor like every other seam (D-A2-02).
func (s *Server) SetDataProxy(data DataFetcher) { s.data = data }

// SetGraphView wires the read-only graph view the /api/graph/schema +
// /api/graph/query routes delegate to. Set by the daemon composition root after
// NewServer (NewArcadeGraphView over the per-identity memory database); until set, both
// routes answer 503 (a missing graph store must not abort serve boot). Kept off the
// constructor so existing NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetGraphView(gv GraphView) { s.graph = gv }

// SetContextWindow wires the active model's context window (tokens) so GET /api/me can
// report it to the cockpit footer gauge. Set by the daemon composition root after NewServer
// from llm.Config.ContextWindow; until set it stays 0 (existing NewServer callers/tests
// unchanged, D-A2-02 narrow seam).
func (s *Server) SetContextWindow(tokens int) {
	s.contextWindowMu.Lock()
	s.contextWindow = tokens
	s.contextWindowMu.Unlock()
}

// SetLLMRuntime makes model-dependent read surfaces use the same atomic snapshot
// as new turns. SetContextWindow remains the fallback for tests and static callers.
func (s *Server) SetLLMRuntime(runtime *llm.Runtime) { s.llmRuntime = runtime }

// SetStudio wires the cockpit Studio's live half: the media catalog, the two shared
// generation paths, the job store and the identity's image library. Set by the daemon
// composition root only when every one of them is configured (cmd/aura/serve_studio.go);
// until set, every Studio route answers 503 rather than half-serving a page that pays.
func (s *Server) SetStudio(backend StudioBackend) { s.studio = backend }

// SetBrowserRelay wires the in-box live-view relay the /api/browser/sessions routes open
// (cmd/aura over the sandbox router). Until set, the stream route answers 503.
func (s *Server) SetBrowserRelay(r BrowserRelay) { s.browserRelay = r }
