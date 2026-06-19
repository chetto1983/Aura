# Phase 27: Neo4j Graph Explorer - Pattern Map

**Mapped:** 2026-06-19
**Files analyzed:** 18 (7 backend new/edit, 11 frontend new/edit) + tests
**Analogs found:** 17 / 18 (one true greenfield: `web/src/graph/SigmaCanvas.tsx` — no in-repo WebGL renderer; external `D:/tmp/llm_wiki` reference only)

> Every analog path below was opened and confirmed to exist at the cited location this session. Where CONTEXT cited stale line ranges, the corrected current location is captured. This phase is **additive + read-only**: no migration, no new Go dependency, no `messages[0]`/SSE touch.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/knowledge/graphview.go` (NEW) | service/normalizer | request-response (intent→Cypher→contract) | `internal/agent/display/normalize.go` + `payload.go` (flat-struct contract) + `internal/reasoningstore/store.go` (GraphClient seam + APOC list idiom) | role+flow match (composite) |
| `internal/knowledge/graphview_test.go` (NEW) | test | — | `internal/agent/display/normalize_test.go` (table-driven) | exact |
| `internal/knowledge/graphview_integration_test.go` (NEW) | test (live) | — | `internal/knowledge/client_test.go` (`//go:build neo4j_integration && db_integration`) | exact |
| `internal/agui/graph_api.go` (NEW) | controller (thin REST adapter) | request-response | `internal/agui/conversations_api.go` + `image_proxy.go` | exact |
| `internal/agui/graph_api_test.go` (NEW) | test | — | `internal/agui/conversations_api*_test.go` (httptest) | role match |
| `internal/agui/server.go` (EDIT) | route registration + setter | — | `Mux()` + `SetImageProxy`/`SetApprovalStore` (same file) | exact |
| `cmd/aura/serve.go` (EDIT) | composition root | — | `aguiServer.SetImageProxy(...)` wiring (same file ~298) | exact |
| `cmd/aura/serve_webui.go` (EDIT) | route mount (carve-out) | — | `imageProxyRoute`/`conversationsRoutePrefix` const + `mux.Handle` (same file) | exact |
| `web/src/graph/GraphExplorer.tsx` (NEW, lazy) | component (workspace shell) | — | `web/src/AppShell.tsx` center `<section>` + `SourceExplorerProvider` state pattern | role match |
| `web/src/graph/SigmaCanvas.tsx` (NEW) | component (WebGL renderer) | — | **none in-repo** — `D:/tmp/llm_wiki/src/components/graph/graph-view.tsx` (external) | **greenfield** |
| `web/src/graph/SeedFilterPanel.tsx` (NEW) | component (left panel) | — | `web/src/chat/displays/SourceExplorerSheet.tsx` (panel + controls layout) | role match |
| `web/src/graph/NodeInspector.tsx` (NEW) | component (right inspector) | — | `web/src/chat/displays/SourceExplorerSheet.tsx` + `SourceExplorerContext.tsx` (`openSources()` cross-link) | role match |
| `web/src/graph/PathStrip.tsx` (NEW) | component (a11y DOM mirror) | — | `web/src/chat/displays/SourceExplorerSheet.tsx` (semantic list + focus mgmt) | partial |
| `web/src/graph/graphIntent.ts` (NEW, pure) | utility (intent/filter/color logic) | transform | `web/src/chat/displays/sourceExplorerData.ts` (pure logic off the .tsx) | exact |
| `web/src/graph/graphApi.ts` (NEW) | utility (fetch wrappers) | request-response | existing `fetch`-based hooks in `web/src` (TanStack Query) | role match |
| `web/src/graph/__tests__/*.test.ts` (NEW) | test | — | `web/src/chat/displays/__tests__/sourceExplorerData.test.ts` (Vitest, pure-logic) | exact |
| `web/src/AppShell.tsx` (EDIT) | mount seam | — | center `<section>` swap (same file ~212-236) | exact |
| `web/src/i18n/resources.ts` + new `resources.graph.ts` (EDIT/NEW) | config (i18n) | — | `resources.display.ts` + the spread in `resources.ts` | exact |
| `web/e2e/graph.spec.ts` + `graph-a11y.spec.ts` (NEW) | test (e2e) | — | `web/e2e/displays.spec.ts` (Playwright) | exact |

---

## Pattern Assignments

### `internal/knowledge/graphview.go` (service/normalizer, request-response)

Composite analog — three sources, one new file.

**Analog A — the narrow consumer-side `GraphClient` seam** (`internal/reasoningstore/store.go:17-28`, VERIFIED current):
```go
// GraphClient is the narrow Cypher seam Store needs. *knowledge.Client satisfies
// it (Read/Write decode rows to []map[string]any).
type GraphClient interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
	Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}
```
Copy this idiom but READ-ONLY: declare a `GraphReader` with ONLY `Read` (the write-verb guard + read-only milestone forbid surfacing `Write`). `*knowledge.Client` satisfies it. This is the mockable seam that lets `graphview_test.go` inject canned rows with zero live Neo4j.

**Analog B — the `apoc.convert.toJson` list idiom + the mcp list-NULL constraint** (`internal/reasoningstore/store.go:30-42`, VERIFIED current — this is the load-bearing Cypher-shape rule):
```go
const (
	// The mcp-neo4j-cypher read tool returns NULL for list-valued columns
	// (scalars and indexed elements come back fine, full lists do not), so we
	// serialize the embedding to a JSON string with APOC and parse it in Go.
	loadQuery = `MATCH (e:ReasoningExample) WHERE e.embedding IS NOT NULL
RETURN e.tier AS tier, apoc.convert.toJson(e.embedding) AS embedding`
)
```
Apply directly: every `labels(n)` (a Cypher LIST) MUST be `apoc.convert.toJson(labels(n)) AS labels_json` then `json.Unmarshal` in Go. NEVER `RETURN n` (loses labels + elementId through mcp serialization). Project explicit scalars: `elementId(n) AS id, properties(n) AS props, type(r) AS rel_type, elementId(startNode(r)) AS src, elementId(endNode(r)) AS dst`.

**Analog C — the flat tagged-struct contract** (`internal/agent/display/payload.go:34-47`, VERIFIED current):
```go
// Payload is the flat tagged union a normalizer emits (R1). It is a struct, not an
// interface, so decode(encode) is identity and JSON omitempty keeps the wire lean
type Payload struct {
	Type       Kind          `json:"type"`
	ToolCallID string        `json:"tool_call_id"`
	Title      string        `json:"title,omitempty"`
	WebResults []WebItem     `json:"web_results,omitempty"`
	Document   *Document     `json:"document,omitempty"`
	...
	Sources    []Source      `json:"sources,omitempty"`
}
```
Mirror this discipline for `{nodes, edges, paths, schema, query}`: a flat struct (NOT an interface), `omitempty` so decode(encode) is identity, sub-structs (`GraphNode`/`GraphEdge`/`GraphPath`/`GraphSchema`) defined in the same file with the same comment-density. `Source` (payload.go:132-141) is the shape to mirror for a node's per-item fields (RefID/Index/Confidence/etc.).

**The underlying read path it wraps** (`internal/knowledge/client.go:226-232`, VERIFIED — this is the ONLY runtime graph read; CLAUDE.md bans a native Go driver for reads):
```go
func (c *Client) Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	result, err := c.Cypher(ctx, query, params, false)
	if err != nil {
		return nil, err
	}
	return decodeRows(result)
}
```
Note `buildRequest` (client.go:176-193) keeps `query` and `params` structurally separate — there is NO concatenation path. The parameterized-Cypher requirement (D-05) is satisfied by passing a `params` map here; never `fmt.Sprintf` a value into the query body.

**LOC discipline:** keep ≤600 LOC; split on touch into `graphview_intent.go` / `graphview_schema.go` / `graphview_normalize.go` (CLAUDE.md no-god-class), the `_<concern>.go` convention.

---

### `internal/agui/graph_api.go` (controller, request-response)

**Analog:** `internal/agui/conversations_api.go` (thin-handler-over-store) + `internal/agui/image_proxy.go` (read-GET-behind-RequireAuth + 503-when-unwired).

**JSON helpers to REUSE (do not redefine)** — `conversations_api.go:64-74` (VERIFIED current, package-level in `agui`):
```go
func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("agui: encode conversations response", "err", err)
	}
}
```
`sanitizeErr(err)` and `SanitizeString(...)` already exist in `server.go` (used at conversations_api.go:95, image_proxy.go:51) — every wire error string MUST pass through one (HARDEN-08 untrusted-output posture; graph node properties are untrusted).

**Route-registration sub-function pattern** — `conversations_api.go:47-60` (VERIFIED current):
```go
func (s *Server) registerConversationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations", s.handleListConversations)
	mux.HandleFunc("POST /api/conversations", s.handleCreateConversation)
	mux.HandleFunc("GET /api/conversations/search", s.handleSearchConversations)
	...
}
```
Mirror as `func (s *Server) registerGraphRoutes(mux *http.ServeMux)` with `mux.HandleFunc("GET /api/graph/schema", s.handleGraphSchema)` + `mux.HandleFunc("POST /api/graph/query", s.handleGraphQuery)`.

**The 503-when-unwired + body-cap + 400 shape** — `image_proxy.go:37-46` (VERIFIED) is the precise read-GET template:
```go
func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	if s.images == nil {
		http.Error(w, "image proxy not configured", http.StatusServiceUnavailable)
		return
	}
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing url query parameter", http.StatusBadRequest)
		return
	}
	...
}
```
For the POST handler, copy the body-cap + decode-guard from `conversations_api.go:124-129`:
```go
r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
var body createConversationBody
if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
	http.Error(w, "invalid request body", http.StatusBadRequest)
	return
}
```
(`maxRunBodyBytes` is already defined in the `agui` package.) Add `op` enum validation + caps-clamp + label/rel-type validation against the live schema set (V5 input validation).

**Consume the `GraphView` via a narrow interface field on `Server`** (mirror `s.images ImageFetcher` at image_proxy.go:15-17). Declare `GraphView` consumer-side in the `agui` package so the handler depends only on the methods it calls.

---

### `internal/agui/server.go` (EDIT — route registration + setter)

**Analog:** the same file's `Mux()` + the `SetImageProxy`/`SetApprovalStore` setters.

**Add the registration call inside `Mux()`** — `server.go:127-148` (VERIFIED current):
```go
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	...
	mux.HandleFunc("GET /api/image-proxy", s.handleImageProxy)
	s.registerConversationRoutes(mux)
	s.registerApprovalRoutes(mux)
	s.registerAssetRoutes(mux)
	return s.withCORS(mux)
}
```
Insert `s.registerGraphRoutes(mux)` alongside the other `register*Routes` calls.

**Add the setter** — `server.go:117-121` (VERIFIED, the exact narrow-seam pattern to copy verbatim):
```go
// SetImageProxy wires the SSRF-safe image fetcher (D-09) the /api/image-proxy route
// delegates to. ... until set, the route answers 503. Kept off the constructor so
// existing NewServer callers/tests stay unchanged (D-A2-02).
func (s *Server) SetImageProxy(images ImageFetcher) { s.images = images }
```
Add `func (s *Server) SetGraphView(gv GraphView) { s.graph = gv }` + a `graph GraphView` field on the `Server` struct. Keep it OFF the `NewServer` constructor (server.go:105-107) so existing callers/tests are untouched.

---

### `cmd/aura/serve.go` (EDIT — composition root)

**Analog:** the `SetImageProxy` wiring (same file ~292-298, VERIFIED current):
```go
aguiServer.SetApprovalStore(chat.pause)
// Wire the DISP-05/D-09 image-proxy fetcher: a fresh web.Client reusing the SAME
// SSRF-hardened transport web_search/web_fetch use ...
aguiServer.SetImageProxy(web.NewClient(chat.cfg))
```
Add `aguiServer.SetGraphView(...)` here, constructing a `GraphView` that wraps a `knowledge.Client`. **Open decision for the planner (RESEARCH §Open Q2 / A7):** open ONE `knowledge.Client` at boot and append its `Close` to the existing reverse-close teardown (`mcpClosers`), mirroring how `chat.go:221` conditionally opens one — vs lazy-open per request. Recommended: boot-time open. There is no long-lived graph client in serve.go today, so this is the only new lifecycle.

---

### `cmd/aura/serve_webui.go` (EDIT — route mount under the `/api/` carve-out)

**Analog:** the `imageProxyRoute` const + its `mux.Handle` (same file, VERIFIED current). This is the EXACT precedent for a read route under the carve-out.

**The const + mount** — serve_webui.go:132-138, 244-248:
```go
// imageProxyRoute is the DISP-05/D-09 SSRF-safe image relay ... a sibling of
// "/api/conversations/" + "/api/approvals" under the "/api/" exclusion carve-out —
// NEVER a bare "/api/" (which would shadow the integrations proxy, T-24-07 / T-25-05).
// It delegates to the AG-UI handler (the route lives on Server.Mux) and is a read GET,
// so it inherits RequireAuth from the whole-mux wrap with no capability gate.
const imageProxyRoute = "GET /api/image-proxy"
...
mux.Handle(imageProxyRoute, aguiHandler)
```
Add `const graphSchemaRoute = "GET /api/graph/schema"` and `const graphQueryRoute = "POST /api/graph/query"` as siblings, each `mux.Handle(graphSchemaRoute, aguiHandler)` / `mux.Handle(graphQueryRoute, aguiHandler)`. **Both are read-only → `aguiHandler` directly (RequireAuth only), NOT `RequireCapability`** — the read-only milestone has no capability gate (contrast the mutating routes at serve_webui.go:201/227/235 which wrap `agui.RequireCapability(...)`).

**Critical carve-out rule** (serve_webui.go:21-25, 249-253): `/api/` is an EXCLUSION prefix ONLY — it is in `fallbackExcludedPrefixes()` (line 90) but NEVER registered as a bare `mux.Handle("/api/", ...)`. The new graph routes need NO change to `fallbackExcludedPrefixes()` (the `/api/` carve-out at line 90 already covers them). Register specific method+path patterns only.

---

### `web/src/graph/graphIntent.ts` (utility, pure transform)

**Analog:** `web/src/chat/displays/sourceExplorerData.ts` — pure logic off the `.tsx`, directly mutation-tested. This is the testable seam that carries the Vitest ≥85% + Stryker ≥70% gate WITHOUT a real WebGL context (Pitfall 4).

The analog's test header states the discipline (`__tests__/sourceExplorerData.test.ts:11-13`, VERIFIED):
```ts
// Exhaustive unit coverage for the pure Source Explorer logic (every sort key, the
// search predicate, completeness, and the safeHost parse paths) — the toolStatus.ts
// idiom: logic off the .tsx, directly mutation-tested.
```
Put ALL of: the intent state reducer, the label/edge filter predicates, the schema-driven label-family color mapper (known family → brand token; unmapped → `hash(label) → ramp index`, deterministic), and the row→client-graph mapping HERE — with NO `sigma`/`@react-sigma` import. `SigmaCanvas.tsx` imports the renderer; this module never does.

---

### `web/src/graph/NodeInspector.tsx` + the Source Explorer cross-link (D-09)

**Analog:** `web/src/chat/displays/SourceExplorerContext.tsx` (`openSources()` is the deep-link target) + `SourceExplorerSheet.tsx` (panel layout).

**The cross-link contract** — `SourceExplorerContext.tsx:30-32` (VERIFIED current):
```ts
const openSources = useCallback((sources: readonly DisplaySource[], focusRefId?: string) => {
	setState({ open: true, sources, focusRefId });
}, []);
```
Consumed via `useSourceExplorer()` (from `sourceExplorerControls`). The inspector's "Open source" action calls `openSources(sourcesForNode(node), node.refId)` when a `:Document`/`:Source` node's URL matches a Phase-26 citation. Do NOT build a new sheet — reuse this read-only one. The provider already renders exactly one `SourceExplorerSheet` (SourceExplorerContext.tsx:43-48); the inspector is just another opener.

---

### `web/src/AppShell.tsx` (EDIT — center swap seam, D-11)

**Analog:** the center `<section>` (same file, VERIFIED current). Today it ALWAYS mounts `ExternalStoreChat` inside a `Suspense`.

**Seam** — AppShell.tsx:79 + 212-236:
```tsx
const { surface, setSurface } = useSurfaceIntent();
...
<section aria-label={t('shell.chatRegion')} className="flex min-h-[min(45svh,100%)] flex-col bg-bg">
	<div className="min-h-0 flex-1">
		<Suspense fallback={<div role="status" ...>{t('chat.loading')}</div>}>
			<ExternalStoreChat threadId={activeThreadId} ... />
		</Suspense>
	</div>
	<ThreadApprovalCards conversationId={activeThreadId} onResolved={redriveRun} />
</section>
```
When `surface === 'graph'`, swap `ExternalStoreChat` for a lazy `<GraphExplorer threadId={activeThreadId} />`. `surface` already comes from `useSurfaceIntent()` (AppShell.tsx:79) and `'graph'` is already a valid `SurfaceIntent` (`web/src/shell/modes.ts:1` — `MODES = ['chat', 'tree', 'graph', 'displays', 'settings']`, VERIFIED). The lazy import (Pitfall 7 bundle-weight):
```tsx
const GraphExplorer = React.lazy(() => import('./graph/GraphExplorer'));
```

---

### `web/src/i18n/resources.ts` + new `web/src/i18n/resources.graph.ts` (EDIT/NEW, i18n)

**Analog:** `web/src/i18n/resources.display.ts` — the bundle-split-to-stay-under-600-LOC precedent, spread into each language in `resources.ts`.

**The split pattern** — `resources.display.ts:8-9` header (VERIFIED current):
```ts
// The `display.*` i18n feature bundle ... extracted from resources.ts to keep that
// file under the 600-LOC cap (CLAUDE.md "no god class") ...
export const displayEn = { display: { type: { web_result: 'Web results', ... } } };
```
And the spread in `resources.ts` (VERIFIED at lines 1, 142, 415):
```ts
import { displayEn, displayIt } from './resources.display';
// ... en.translation: { ...displayEn, ... }   it.translation: { ...displayIt, ... }
```
Create `resources.graph.ts` exporting `graphEn`/`graphIt` (the `graph.*` keys from 27-UI-SPEC §Copywriting Contract — `graph.cta.seedConversation`, `graph.title`, `graph.inspector.*`, `graph.path.*`, `graph.a11y.*`, `graph.filter.*`, etc.) and spread both into `resources.ts`. **Add keys to BOTH `en` and `it`.** Rebuild `internal/webui/dist` after copy changes (CI freshness gate).

---

### Tests

**Backend unit** (`graphview_test.go`, `graph_api_test.go`) — analog `internal/agent/display/normalize_test.go:11-36` (VERIFIED), the table-driven shape:
```go
func TestNormalizeDispatch(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		result any
		want   Kind
	}{ ... }
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := Normalize(tc.tool, tc.result)
			...
		})
	}
}
```
Inject a fake `GraphReader` returning canned `[]map[string]any` rows. Test: intent→Cypher (params not interpolated, caps clamped), row→contract (labels via toJson, edge endpoints attach), the write-verb guard (each verb present/absent, verbs inside string literals must NOT trip, verbs inside `CALL { ... }`). `graph_api_test.go` uses `httptest` over a fake `GraphView` (route registration, 400 bad-intent, 401 unauth, contract JSON shape).

**Backend integration** (`graphview_integration_test.go`) — analog `internal/knowledge/client_test.go:1` (VERIFIED), the exact build-tag header:
```go
//go:build neo4j_integration && db_integration
```
Run via `make neo4j-migrate && go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/...`. This is the Wave-0 gate that pins the real mcp serialization shape (resolves Assumptions A1/A3). **No-skip-as-green:** the tier `t.Fatal`s under `$CI` when env is unset.

**Frontend unit** (`web/src/graph/__tests__/*.test.ts`) — analog `web/src/chat/displays/__tests__/sourceExplorerData.test.ts:1-8` (VERIFIED), Vitest pure-logic:
```ts
import { describe, expect, it } from 'vitest';
import { filterAndSortSources, safeHost, ... } from '../sourceExplorerData';
```
Test `graphIntent.ts` (filter/color/intent) with NO WebGL. Mock `@react-sigma/core` (or the whole `SigmaCanvas`) in any component test. Config: `web/vitest.config.ts` + `web/stryker.config.json` (extend Stryker scope to include `web/src/graph`).

**Frontend e2e** (`web/e2e/graph.spec.ts`, `graph-a11y.spec.ts`) — analog `web/e2e/displays.spec.ts` (VERIFIED present), Playwright. Config: `web/playwright.config.ts`. The a11y spec runs axe + keyboard traversal (the real WebGL render assertion lives here, not in jsdom).

---

## Shared Patterns

### Read-only graph access (MCP-only, no native driver)
**Source:** `internal/knowledge/client.go:226` `Client.Read` + `:8` package doc.
**Apply to:** `graphview.go` ONLY.
CLAUDE.md bans a native Go Neo4j driver for runtime reads. Every Cypher call rides `read_neo4j_cypher` (read-tx by construction). `schema.go`'s Go-driver path is schema-DDL ONLY — do NOT reuse it for the query surface. The narrow `GraphReader` (Read-only) seam (from `reasoningstore.GraphClient`) is the consumer-side interface; `*knowledge.Client` satisfies it.

### Parameterized Cypher, never interpolated (+ write-verb guard)
**Source:** `internal/knowledge/client.go:176-193` `buildRequest` (query/params structurally separate) + `internal/reasoningstore/store.go:30-42` (APOC list idiom).
**Apply to:** every Cypher in `graphview.go`.
Values ride the `params` map (`$session`, `$labels`, `$node_cap`). NEVER `fmt.Sprintf` a value into the query body. Labels/rel-types bound as data in `WHERE x IN $list`, not as Cypher identifiers. Belt-and-suspenders: an `assertReadOnly(cypher)` regex reject of `CREATE/MERGE/SET/DELETE/REMOVE/DROP/FOREACH` + `CALL { ...write }` before dispatch (strip string literals first to avoid false-positive on data).

### Thin JSON handler behind RequireAuth, under the `/api/` carve-out
**Source:** `internal/agui/conversations_api.go:64-74` (`writeJSON`/`writeJSONStatus`) + `internal/agui/server.go` `sanitizeErr`/`SanitizeString` + `cmd/aura/serve_webui.go:138/248` (`imageProxyRoute` precedent).
**Apply to:** `graph_api.go` + the `serve_webui.go` mount.
`/api/graph/schema` + `/api/graph/query` register as SPECIFIC siblings (never a bare `/api/`, which shadows `/api/integrations/`). Read-only → RequireAuth only, no `RequireCapability`. Every wire error string passes `sanitizeErr`/`SanitizeString` (HARDEN-08; node properties are untrusted content — render as text, never `dangerouslySetInnerHTML`).

### Narrow consumer-side setter seam (off the constructor)
**Source:** `internal/agui/server.go:117-121` `SetImageProxy` + `cmd/aura/serve.go:298` wiring.
**Apply to:** `SetGraphView` on `Server` + the `serve.go` composition.
Add the dependency via a `Set*` setter (503 until wired), NOT the `NewServer` constructor, so existing callers/tests stay unchanged (D-A2-02).

### Pure logic off the .tsx (the frontend quality seam)
**Source:** `web/src/chat/displays/sourceExplorerData.ts` + its `__tests__/sourceExplorerData.test.ts`.
**Apply to:** `graphIntent.ts` (+ its tests).
Vitest ≥85% + Stryker ≥70% is reachable ONLY if the intent/filter/color/normalization logic is in a pure module with no `sigma` import (jsdom has no WebGL — Pitfall 4). `SigmaCanvas.tsx` is mocked in unit, exercised in Playwright.

### i18n feature-bundle split (en + it, rebuild dist)
**Source:** `web/src/i18n/resources.display.ts` + the spread in `resources.ts:1,142,415`.
**Apply to:** new `resources.graph.ts`.
Split to stay under the 600-LOC cap; spread `graphEn`/`graphIt` into both languages; add every key to BOTH; rebuild `internal/webui/dist`.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `web/src/graph/SigmaCanvas.tsx` | component (WebGL renderer) | — | No WebGL/canvas renderer exists anywhere in `web/src` (the cockpit is DOM/SVG only — recharts was rejected in Phase 26). The ONLY template is the EXTERNAL curated reference `D:/tmp/llm_wiki/src/components/graph/graph-view.tsx` (sigma@3 + graphology + ForceAtlas2 on React 19) cited in RESEARCH §Pattern 2. The planner must treat the `SigmaContainer` + `useLoadGraph`/`useRegisterEvents`/`useSigma` wiring, the `nodeReducer`/`edgeReducer` highlight/dim, the `key={sigmaKey}` resize-remount crash workaround (Pitfall 1), and the ForceAtlas2 position-cache as greenfield code ported from that external file — NOT from this repo. |

> **Naming-drift note for the planner:** CONTEXT/scoping referenced `sourceExplorerData.ts` (exists ✓) but the Source Explorer's React context lives in `SourceExplorerContext.tsx` + `sourceExplorerControls.ts` (the `useSourceExplorer`/`openSources` seam), NOT a single `SourceExplorerContext.tsx` file alone. All three exist: `SourceExplorerSheet.tsx`, `SourceExplorerContext.tsx`, `sourceExplorerControls.ts`, `sourceExplorerData.ts`. The cross-link target is `openSources()` from `sourceExplorerControls` (re-exported via the context).

---

## Metadata

**Analog search scope:** `internal/knowledge/`, `internal/agui/`, `internal/agent/display/`, `internal/reasoningstore/`, `cmd/aura/`, `web/src/` (AppShell, shell, chat/displays, i18n), `web/e2e/`, test configs.
**Files scanned (opened):** 16 (client.go, conversations_api.go, server.go, image_proxy.go, payload.go, reasoningstore/store.go, serve_webui.go, normalize_test.go, modes.ts, SourceExplorerContext.tsx, AppShell.tsx, resources.display.ts, sourceExplorerData.test.ts, smoke_test.go, client_test.go, test_helpers_test.go) + Glob/Grep confirmation of 12 more.
**Pattern extraction date:** 2026-06-19
