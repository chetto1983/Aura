# Architecture Research

**Domain:** Operator web cockpit ("Aura Deep Search") integration over an already-shipped Go single-binary AG-UI/SSE substrate
**Researched:** 2026-06-15
**Confidence:** HIGH (every integration claim grounded in real backend source; graph contract verified against llm-graph-builder + internal/knowledge; classifier pattern verified against elysia-frontend)

> **Scope.** This file answers *how the cockpit wires into the existing backend and in what order to build it*. It does NOT relitigate the stack (Vite 8 + React 19 + embedded SPA — see STACK.md) or the feature taxonomy (see FEATURES.md). It designs the five integration mechanisms — (1) typed-display protocol GAP-1, (2) graph payload contract, (3) serve/embed, (4) web auth GAP-2, (5) observability + (6) ui_control lane — and gives a (7) dependency-ordered build order as vertical slices.

---

## Standard Architecture

### System Overview

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  BROWSER — embedded SPA (Vite 8 / React 19, served by the binary)              │
│  ┌────────────┐  ┌──────────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ chat lane  │  │ display router   │  │ graph canvas │  │ shell/ui_control │  │
│  │ assistant- │  │ (typed payloads) │  │ @neo4j-nvl   │  │ Zustand store +  │  │
│  │ ui+ag-ui   │  │ web/doc/code/... │  │ /react       │  │ allowlist reducer│  │
│  └─────┬──────┘  └────────┬─────────┘  └──────┬───────┘  └────────┬─────────┘  │
│        │ SSE (POST /agent/run)  │ same SSE stream  │ REST (graph)  │ SSE CUSTOM │
└────────┼────────────────────────┼──────────────────┼───────────────┼───────────┘
         │                        │                  │               │
══════════════════════ HTTP boundary (loopback OR reverse proxy + auth, GAP-2) ════
         │                        │                  │               │
┌────────▼────────────────────────▼──────────────────▼───────────────▼───────────┐
│  internal/agui — HTTP gateway (one http.Server in cmd/aura/serve.go)            │
│  ┌──────────────┐  ┌──────────────────────────┐  ┌───────────────────────────┐ │
│  │ static SPA   │  │ POST /agent/run (SSE)     │  │ REST read API (NEW)       │ │
│  │ handler(NEW) │  │ streamSSE + Translate     │  │ /api/{convs,graph,mcp,    │ │
│  │ embed.FS     │  │ + typed-display emit (GAP1)│  │  skills,tasks,health}     │ │
│  └──────────────┘  └────────────┬─────────────┘  └─────────────┬─────────────┘ │
├──────────────────────────────────┼──────────────────────────────┼──────────────┤
│  internal/agent — Event stream (iter.Seq2[*agent.Event,error])  │  read stores  │
│  Actions{ToolInvocation, ArtifactDelta, StateDelta, AwaitingInput, Display(NEW)}│
├──────────────────────────────────┼──────────────────────────────┼──────────────┤
│  TOOLS / SOURCES                  │                              │              │
│  web_search/web_fetch  knowledge(mcp-neo4j-cypher)  swarm  mcp/manager  cron    │
│  internal/web          internal/knowledge   internal/swarm  ...    askuser/store│
└──────────────────────────────────┴──────────────────────────────┴──────────────┘
```

### Component Responsibilities

| Component | Responsibility | New vs Modified | File |
|-----------|----------------|-----------------|------|
| `agent.Event.Actions.Display` | NEW typed-display payload slot on the agent Event (the GAP-1 spine) | **NEW field** | `internal/agent/event.go` |
| `agui.Translate` typed-display branch | Map `Actions.Display` → namespaced AG-UI `CUSTOM` event (`aura.display`) | **Modified** | `internal/agui/translator.go` |
| Display classifier (Go) | Normalize each tool result into a typed `Display` payload, keyed by tool name + Meta | **NEW** | `internal/agent/display/` (new pkg) |
| Graph normalizer | `[]map[string]any` Cypher rows → `{nodes,edges,paths,schema,query}` NVL contract | **NEW** | `internal/knowledge/graphview.go` |
| REST read API | Non-stream reads (conversations, graph, mcp, skills, tasks, health) | **NEW** | `internal/agui/api_*.go` |
| Static SPA handler | `embed.FS` + SPA-fallback excluding API routes | **NEW** | `internal/webui/` + `agui.Mux` |
| Auth middleware | Session/proxy boundary; protects writes (GAP-2) | **NEW** | `internal/agui/auth.go` |
| `ui_control` lane | Allowlist-validated CUSTOM event, run-log replayable | **NEW** | `internal/agui` + frontend reducer |
| Observability surface | `runtime_status` aggregation behind `/api/health/runtime` | **Modified** (extend `/healthz`) | `cmd/aura/serve.go`, `internal/agui` |

---

## 1. Typed-Display Event Protocol (GAP-1) — the spine

### The decision: normalize in the Go translator, emit a typed CUSTOM event

The design's Implementation Model is `Aura event -> display classifier -> typed renderer`. Two places could host the classifier:

- **Frontend classifier** (bootstrap-only): infer the display type from the existing `TOOL_CALL_START.toolCallName` + `TOOL_CALL_RESULT` text. This works *today with zero backend change* (the translator already stamps tool name on `TOOL_CALL_START` via `emitToolInvocation`, `translator.go:303`), but it forces the browser to re-parse opaque tool-result text and re-derive structure the backend already had.
- **Go translator normalization** (the recommendation, GAP-1 proper): the backend already holds the structured result — `ToolInvocation.ToolName`, `ToolInvocation.Meta`, `ToolInvocation.ResultSidecarPath`, `Actions.ArtifactDelta`, and the typed `web.WebError` enum. Normalize there, emit a typed payload, and the frontend becomes a pure renderer.

**Recommendation: build the Go-side normalizer as the contract; keep a thin frontend classifier only as the v0 fallback for tool families not yet normalized.** Rationale: (a) the structure exists backend-side and re-deriving it client-side from prose is exactly the "regex on natural language" anti-pattern the project forbids; (b) the typed payload is *replayable from the run log* (a hard `ui_control` requirement that bleeds into displays); (c) it keeps AG-UI protocol compatibility because the payload rides a single namespaced `CUSTOM` event, exactly like the shipped `aura.artifact` (`translator.go:19`).

### Go-level design (protocol-compatible)

Add ONE typed slot to the agent `Event` — mirroring the existing `ArtifactDelta` precedent (`event.go:71`), purely additive (omitempty so existing events are byte-identical):

```go
// internal/agent/event.go — Actions
type Actions struct {
    // ...existing: Escalate, StateDelta, ArtifactDelta, AwaitingInput, ToolInvocation, DiscardStreamed
    Display *DisplayPayload `json:"display,omitempty"` // NEW — typed-display payload (GAP-1)
}

// DisplayPayload is the channel-agnostic typed-display envelope. Type names are a
// wire contract (mirror ux-spec lines 420-438); Data is the type-specific body.
type DisplayPayload struct {
    Type       string          `json:"type"`              // web_result|document|code|local_artifact|table|chart|swarm_report|graph_chunk|graph_path|graph_schema|system_event|mcp_*|skill_*|background_job|ui_control
    ToolCallID string          `json:"tool_call_id,omitempty"`
    Source     string          `json:"source,omitempty"`  // originating tool/collection (for merged-tab grouping)
    Title      string          `json:"title,omitempty"`
    Data       json.RawMessage `json:"data"`              // type-specific structured body
    Citations  []Citation      `json:"citations,omitempty"`
}
```

Translator change — one new branch, slotted before the generic StateDelta branch (same position as the artifact branch at `translator.go:115`), emitting a namespaced CUSTOM event so any AG-UI consumer that does not understand it simply ignores it (protocol-compatible by construction):

```go
// internal/agui/translator.go
const DisplayEventName = "aura.display" // sibling of ArtifactEventName "aura.artifact"

if ev.Actions.Display != nil {
    if !closeRuns() { return }
    if !yield(events.NewCustomEvent(DisplayEventName, events.WithValue(ev.Actions.Display)), nil) { return }
    continue
}
```

> `events.EventTypeCustom` is ALREADY in the `isLifecycleFrame` allowlist (`server.go:340`) so a display event is never dropped under SSE backpressure — the existing artifact branch relies on this same guarantee.

### Where each display type is sourced (Go normalizer, `internal/agent/display/`)

The classifier runs at the same seam that builds `toolResultEvent` (`llm_agent_events.go:95`), reading `run.ToolName` + `run.Result.Meta`:

| Display type | Backend source (file) | Normalization |
|--------------|----------------------|---------------|
| `web_result` | `web_search` tool, `internal/web/searxng.go` results | tool result JSON → `{results:[{title,url,snippet,domain,cached}]}` |
| `document` | `web_fetch` markdown, `internal/web/fetcher.go` | markdown body → `{markdown, url, title, truncated, sidecar_path}` |
| `code` / `local_artifact` | `sandbox_exec`/`shell_exec` sidecar, `ToolInvocation.ResultSidecarPath` + `ExitCode` | `{stdout, stderr, exit_code, sidecar_path, host_vs_sandbox}`; `send_file` rides existing `aura.artifact` (do NOT duplicate) |
| `swarm_report` | `swarm.ChildReport`, `internal/swarm/report.go:32` | reports JSON → `{children:[{goal_index,child_id,status,summary,error,question,options}]}` (no inter-agent chat — anti-feature) |
| `graph_chunk`/`graph_path`/`graph_schema`/`table` | `knowledge` MCP rows, `internal/knowledge/client.go` | see §2 (graph contract) |
| `system_event` | `web.WebError` stable enum, `internal/web/errors.go:11-30` | `{error, reason, message, status_code}` — the enum is ALREADY a safe contract; pass through verbatim, never re-derive |
| `mcp_*` | `manager.SnapshotStatus`, `internal/mcp/manager/status.go:40` | status snapshot → `mcp_server`/`mcp_tool`/`mcp_doctor_result`/`mcp_mount_warning` (these are REST reads, not stream events — see build order) |
| `skill_*` | `aura.skill_audit` + pending roots, `internal/agent/tools/skill*.go` | governance reads → REST, mutation requests → `approval_gate` (interrupt) |
| `background_job` | research/compare progress | rides `STATE_DELTA` (already emitted) or a `background_job` display; updates rail + dock chip client-side |
| `ui_control` | agent-requested safe UI change | see §6 (allowlist lane) |

### Normalization split (definitive)

- **Backend (Go translator/normalizer):** structure extraction, the safe web-error enum, the graph contract, swarm reports, sidecar pointers, citations. Everything that has structure server-side OR carries a safety contract (web errors, ui_control allowlist).
- **Frontend (classifier/renderer):** display-type → React component routing (the elysia `switch (payload.type)` pattern, verified in `RenderDisplay.tsx`), merged-tab grouping by `Source`, pagination, raw/code toggle, citation hover. The frontend is a pure renderer over the typed envelope; it classifies *only* when `Display == nil` (the v0 fallback path, decommissioned per tool family as the Go normalizer covers it).

**Data flow (typed display):**
```
tool executes → toolResultEvent (llm_agent_events.go) reads ToolName+Meta
  → display.Classify(run) builds Actions.Display
  → Translate emits aura.display CUSTOM event (translator.go)
  → SSE → @ag-ui/client → frontend onCustomEvent("aura.display")
  → RenderDisplay switch(payload.type) → typed React card
```

---

## 2. Neo4j Graph Payload Contract

### The seam

`internal/knowledge/client.go` `Read(ctx, query, params)` already returns `[]map[string]any` decoded from the `mcp-neo4j-cypher` `{"content":[{"type":"text","text":"<json-array>"}]}` envelope (`decodeRows`, `client.go:291`). Schema introspection uses APOC `get-schema` (per neo4j-mcp-skill) via the same Read path; `SchemaExecutor` (`schema.go`) is DDL-only and is NOT the read path. The whole `NEO4J_READ_ONLY=true` posture (neo4j-mcp-skill) is the right default for the cockpit — the graph explorer reads, it does not mutate.

### The normalizer (NEW: `internal/knowledge/graphview.go`)

Map raw Cypher rows → the ux-spec contract (lines 446-454), which is exactly the shape `@neo4j-nvl/react` consumes. Verified against `llm-graph-builder/frontend/src/utils/Utils.ts:processGraphData`: NVL nodes are `{id, caption, color, labels, properties, size}` and relationships are `{id, from, to, caption}`, mapped from Neo4j `element_id` / `start_node_element_id` / `end_node_element_id`. **Do this mapping in Go** so the frontend gets a stable contract and the safety/redaction posture stays server-side:

```go
// internal/knowledge/graphview.go
type GraphView struct {
    Nodes  []GraphNode  `json:"nodes"`
    Edges  []GraphEdge  `json:"edges"`
    Paths  []GraphPath  `json:"paths"`
    Schema GraphSchema  `json:"schema"`
    Query  GraphQuery   `json:"query"`
}
type GraphNode struct {
    ID         string         `json:"id"`          // element_id (stable)
    Labels     []string       `json:"labels"`      // → NVL color by labels[0] (source/claim/entity/agent/topic/conflict)
    Title      string         `json:"title"`       // → NVL caption
    Subtitle   string         `json:"subtitle,omitempty"`
    Properties map[string]any `json:"properties"`
    Confidence float64        `json:"confidence,omitempty"` // → NVL size
    Degree     int            `json:"degree,omitempty"`
    SourceRefs []string       `json:"source_refs,omitempty"`
}
type GraphEdge struct {
    ID       string   `json:"id"`                  // element_id
    Source   string   `json:"source"`              // start_node_element_id → NVL from
    Target   string   `json:"target"`              // end_node_element_id → NVL to
    Type     string   `json:"type"`                // relationship type → NVL caption
    Weight   float64  `json:"weight,omitempty"`
    EvidenceRefs []string `json:"evidence_refs,omitempty"`
}
type GraphPath struct { NodeIDs []string `json:"node_ids"`; EdgeIDs []string `json:"edge_ids"`; Summary string `json:"summary"`; SupportCount int `json:"support_count"`; ConflictCount int `json:"conflict_count"` }
type GraphSchema struct { LabelCounts map[string]int `json:"label_counts"`; RelCounts map[string]int `json:"rel_counts"`; IndexedProperties []string `json:"indexed_properties"`; Warnings []string `json:"warnings"` }
type GraphQuery struct { Prompt string `json:"prompt"`; Cypher string `json:"cypher"`; Params map[string]any `json:"params,omitempty"`; ExecMS int64 `json:"exec_ms"`; ResultCount int `json:"result_count"`; SafetyNotes []string `json:"safety_notes,omitempty"` }
```

### Delivery: REST, not the chat stream

The graph explorer (Frame 06) is a *workspace mode*, not a chat turn. Serve it as a REST read endpoint, not an SSE display event:

```
GET  /api/graph/schema          → GraphSchema (APOC get-schema, cached)
POST /api/graph/query           → {prompt|cypher, params} → GraphView (read-only Cypher)
```

The cockpit's NL-to-Cypher path can reuse the agent (a turn that calls knowledge), but the *visualization* read is a direct REST call so the canvas is not coupled to a chat run. Cypher executed from the cockpit must go through a **read-only guard** (reject `CREATE/MERGE/SET/DELETE/DROP`; the MCP client's `write` flag stays `false`) — the `NEO4J_READ_ONLY` posture made explicit at the HTTP boundary. Deterministic layout (ux-spec rule) is a frontend NVL concern (seed by node id); the backend's job is the stable `element_id`-keyed contract that makes layout reproducible.

When a graph result *does* arrive as part of a chat turn (the agent queried Neo4j mid-conversation), emit it as a `graph_chunk`/`graph_path` typed display (§1) carrying a compact `GraphView` so the chat can show an inline preview with a "open in Graph mode" affordance.

---

## 3. Serving + Embedding Model

### Embed package (NEW: `internal/webui/`)

Mirrors the four existing `//go:embed` packages in the codebase (`db/migrate.go`, `knowledge/migrate.go`, `skills/builtin.go`, `conversations/tiktoken.go`) — established pattern, no new dependency:

```go
// internal/webui/embed.go
package webui
import "embed"
//go:embed all:dist
var Assets embed.FS
```

### Mount on the existing http.Server

`cmd/aura/serve.go` already builds ONE `http.Server` (`env.httpSrv`, `serve.go:245`) over `aguiServer.Mux()`. The SPA handler registers on that SAME mux (`agui.Server.Mux`, `server.go:90`) so the cockpit is "just another route family" on the daemon already running — single port, single process, single binary invariant holds.

```go
// internal/agui/server.go — Mux(), add LAST (catch-all is lowest priority)
sub, _ := fs.Sub(webui.Assets, "dist")
fileSrv := http.FileServer(http.FS(sub))
mux.Handle("GET /", s.spaFallback(sub, fileSrv)) // catch-all SPA host
```

### SPA-fallback that excludes API routes

Go 1.22 method-pattern routing already gives explicit precedence to the registered routes (`POST /agent/run`, `GET /threads/{id}/messages`, `/healthz`, `/readyz`, `/metrics`, `/debug/vars`, and the new `/api/*`). The `GET /` catch-all only fires for unmatched paths. The fallback serves the asset if it exists, else `index.html` (client-side routing), **but never for an API prefix** so an API typo returns a real 404 instead of HTML:

```go
func (s *Server) spaFallback(sub fs.FS, fileSrv http.Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p := strings.TrimPrefix(r.URL.Path, "/")
        // API prefixes must 404 as API, never fall back to index.html
        for _, api := range []string{"agent/", "threads/", "api/", "healthz", "readyz", "metrics", "debug/"} {
            if strings.HasPrefix(p, api) { http.NotFound(w, r); return }
        }
        if p == "" { p = "index.html" }
        if _, err := fs.Stat(sub, p); err != nil { p = "index.html" } // deep-link → SPA shell
        r.URL.Path = "/" + p
        fileSrv.ServeHTTP(w, r)
    }
}
```

### Build pipeline (single-binary invariant)

Per STACK.md: `cd web && npm ci && npm run build` → copy `web/dist` → `internal/webui/dist` → `go build` bakes it in. Add a Makefile `web` target and a Node-24 Docker stage. Gate the Node build so Go-only contributors are not forced into Node (ship a checked-in placeholder `dist/` with a single `index.html`, or a `web-build` CI artifact, so `//go:embed all:dist` always compiles).

---

## 4. Web Auth / Session Model (GAP-2)

### Current posture (do not silently break it)

The gateway is **deliberately unauthenticated**: loopback bind IS the compensating control (`server.go:71`, `serve.go:221`, amendment #35). PROJECT.md §Out of Scope: "Multi-user con auth/RBAC reale … Niente login, niente sessioni HTTP autenticate, niente OAuth in v1." This milestone is a **flagged scope expansion** — it must add the *minimum* boundary, not OAuth/RBAC/multi-tenant.

### The three options (increasing in-binary cost)

| Option | Shape | Fit | Protects writes how |
|--------|-------|-----|---------------------|
| **Reverse-proxy-enforced** (recommended start) | Aura stays loopback; Caddy/oauth2-proxy terminates TLS + auth | Best fit: zero change to the auth-deferred Go posture, aligns with the documented Caddy on-demand TLS (commit `5f70703f`) and the single-operator DGX-Spark bundle | Proxy gates the whole origin; Aura trusts the proxy on loopback. All mutations are behind the proxy by definition. |
| **In-binary session cookie** (recommended upgrade path) | Go middleware on the mux issues/validates a signed `HttpOnly + Secure + SameSite=Strict` cookie | The right in-binary choice when the proxy is undesired; pairs naturally with a browser SPA (no token in JS) | Middleware rejects unauthenticated requests on mutating routes; the session principal maps to an `identity` row |
| **Bearer token** | static operator token in `Authorization` header | Reserve for machine/API callers (`HttpAgent` supports custom headers), NOT the human cockpit | XSS-exposed in the browser — inferior to a cookie for a browser app |

### Recommendation + the identity seam

**Ship behind the reverse proxy first** (no Go change, aligns with the existing Caddy story), and **build a thin in-binary session-cookie middleware** as the documented upgrade — because mutating governance actions (MCP install/remove, skill install/delete, task approve, Cypher) must be gated regardless of whether a proxy is present (defense in depth, per golang-security: "every layer should protect itself").

Concrete in-binary design (`internal/agui/auth.go`, NEW):
- A `RequireAuth` middleware wrapping the mutating mux subtree; reads SPA cookies, never client headers (golang-security: "trusting client headers" is an anti-pattern).
- Login issues a signed session bound to an **`identity` row** (PROJECT.md CORE-03 `capability_grants` is the existing seam). Session validation is constant-time (`crypto/subtle.ConstantTimeCompare`); fail closed.
- **Read vs write split:** GET reads can stay loopback/proxy-gated; **all mutations (POST/DELETE governance routes) require the auth principal + a `capability_grants` check** — this is where identity/capability_grants finally earns its keep (it was scaffolding until now). The agent's own mutating tool calls (skill install, MCP add) already route through `ask_user`/approval gates; the HTTP mutation endpoints must enforce the SAME gate (an operator-initiated install still lands a pending state + audit row, never a direct activation — ux-spec Non-Goal: "Do not activate installed or generated skills directly").

**What protects writes (summary):** proxy boundary (origin) → in-binary auth middleware (principal) → capability_grants (authorization) → existing risk/approval gate (RISKY/DESTRUCTIVE → pending + audit). Four layers; the cockpit adds the middle two.

---

## 5. Observability Wiring (`runtime_status` surface)

### What already exists (do NOT rebuild)

The gateway already ships the observability primitives the cockpit's health panels read:

| Endpoint | Emits | File |
|----------|-------|------|
| `GET /healthz` | `{ok, scheduler_last_tick, ...}` — PG ping liveness + scheduler last tick | `server.go:101`, `serve.go:225-237` |
| `GET /readyz` | `{ready, deps:{postgres, neo4j}}` — required-dep readiness probes | `readiness.go:33`, `serve.go:281` |
| `GET /metrics` | Prometheus (`promhttp`) — incl. `aura_agui_sse_dropped_total` | `server.go:95`, `metrics.go` |
| `GET /debug/vars` | expvar | `server.go:94` |
| OTel tracer + JSON slog (redacted) | spans + structured logs (`obs.Init`, OtelExporter/Endpoint config) | `internal/obs/init.go`, `serve.go:110` |

### The gap: an aggregated `runtime_status` read

The design's `runtime_status` (ux-spec lines 503-505) wants one panel-ready snapshot: `{daemon_state, agui_bind, scheduler_tick_state, registry_valid, mcp_mounted/failed_count, cache_hit_rate}`. Today these are scattered. Add ONE aggregation endpoint that composes existing sources (no new collection):

```
GET /api/health/runtime → RuntimeStatus
```

| Panel | Read from (existing) |
|-------|---------------------|
| daemon / AG-UI health | `/healthz` body + `cfg.AGUIBind` |
| scheduler tick | `scheduler.LastTick()` (already in `HealthDetails`, `serve.go:232`); heartbeat from `cron.Store` |
| registry validity | `reg.Validate()` result (boot-time, `main.go:179`) |
| MCP mounted/failed | `manager.SnapshotStatus` (`status.go:40`) + the boot mount-warning count (`buildRegistryWithMCP` WARN drops, `main.go:224`) |
| cache hit rate | `aura cache-stats` data (`cache_metrics` table); `STATE_DELTA` usage already carries per-turn `cache_hit_tokens` (`llm_agent_events.go:224`) |
| tool ledger | `internal/toolinvocations` store (append-only `aura.tool_invocations`) → `GET /api/tools/ledger` |

> **Decision:** the cockpit's daemon/scheduler/MCP/cache/tool-ledger panels read from the EXISTING `/healthz` + `/readyz` + `/metrics` + a NEW thin `/api/health/runtime` aggregator + `/api/tools/ledger`. Do not add a new metrics system — the Prometheus + OTel + expvar trio is shipped; the cockpit just renders it. A panel that needs live numbers polls `/api/health/runtime` via React Query (REST), not SSE.

---

## 6. ui_control Allowlist + Audit Lane + Background-Job/Dock State

### The contract (ux-spec lines 464-475)

`ui_control` lets the agent drive the UI — the highest-abuse-risk surface in the milestone. The contract is strict: allowlisted verbs only, no raw CSS/scripts/URLs/unbounded DOM, replayable from the run log.

Allowlisted verbs: `open_panel` (panel id from allowlist), `highlight_source` (source/internal target id), `set_mode` (`chat|tree|graph|displays|settings`), `show_job` (job id owned by active run/user), `set_density` (`compact|operator|review`), `theme_preview` (schema-validated token object).

### Two-layer validation (defense in depth)

1. **Backend emit + validate (`internal/agui`):** the agent emits a `ui_control` via `Actions.Display{Type:"ui_control", Data:...}`. A backend validator rejects any verb/target not on the allowlist BEFORE it reaches the wire, and writes an audit row (the run log) — `crypto/subtle`-free but strict enum + id-shape checks. Emit as the `aura.display` CUSTOM event (type `ui_control`).
2. **Frontend reducer allowlist (Zustand):** the client applies a ui_control ONLY through an allowlist reducer (STACK.md). The reducer maps a verb to a state mutation; an unknown verb is dropped + logged. `set_mode` maps to the 5-mode allowlist; `theme_preview` validates the token object against a schema; `highlight_source`/`show_job` resolve against ids the client already holds for the active run (no arbitrary DOM).

### Run-log replay

Because `ui_control` rides the same typed-display envelope and is recorded server-side, a session's UI events are replayable: re-streaming the run's events reconstructs "what the operator saw". This is the SAME property the typed-display normalization buys (§1) — another reason to normalize server-side, not client-side.

### Background-job / dock state

Long jobs (Deep Research, Compare) expose running/queued/error/completed outside the modal that created them (ux-spec Frame 07). Backend: a `background_job` display (or `STATE_DELTA`) carries job id + status + progress; the existing `STATE_DELTA` stream already coalesces deterministically (`stateDeltaOps`, `translator.go:341`). Frontend: the Zustand store routes job state to BOTH the icon rail and the relevant dock chip; dock chips "preserve state" (selected sources, trace) across minimize/restore — pure client state, no backend dependency beyond the job-status events. `show_job` (a ui_control verb) focuses a chip; the job id must be owned by the active run/user (validated both ends).

---

## 7. Suggested Build Order / Phase Decomposition

**Principle:** the two gating dependencies (GAP-1 protocol + GAP-2 auth) and the serve/embed infra must land BEFORE the UI surfaces that consume them. Organize as vertical slices that each ship a working end-to-end sliver. Memory `feedback_no_atomic_bombs`: each slice is the minimal industrial shape that satisfies its success criteria.

### Phase A — Serve + Embed + Auth boundary (infra; no UI surfaces yet)
**Why first:** nothing is reachable until the binary serves a page and the boundary exists. Pure infra; unblocks every later slice.
- `internal/webui` embed package + Makefile `web` target + Node-24 Docker stage (§3)
- SPA-fallback handler on `agui.Mux` excluding API routes (§3)
- TLS reverse-proxy story (Caddy) documented + the in-binary `RequireAuth` middleware skeleton on the mutating subtree (§4)
- **Ships:** a static "hello cockpit" shell behind the boundary, health panel reading existing `/healthz`/`/readyz`. Proves the single-binary serve + auth boundary end to end.

### Phase B — Typed-display protocol (GAP-1 backend spine)
**Why second:** ~6 of 8 frames depend on it; it is the single biggest backend build (FEATURES.md). Must precede every typed-display UI surface.
- `agent.Event.Actions.Display` slot + `DisplayPayload` type (§1)
- `internal/agent/display/` Go normalizer for the first tool families (`web_search`→`web_result`, `web_fetch`→`document`, `web.WebError`→`system_event`)
- `agui.Translate` `aura.display` CUSTOM branch (§1)
- **Ships:** chat lane (assistant-ui + ag-ui, near-free per STACK.md) + the FIRST typed displays (web_result, document, system_event) rendering from the live stream. The classifier fallback path for un-normalized tools.

### Phase C — REST read API + observability aggregation
**Why third:** the governance/health surfaces are REST-shaped reads (React Query), independent of the display stream. Composes existing stores.
- `/api/conversations`, `/api/conversations/{id}/search` (over `conversations.Store`)
- `/api/health/runtime` aggregator + `/api/tools/ledger` (§5)
- `/api/mcp/*`, `/api/skills/*`, `/api/tasks/*` read endpoints (over `manager.SnapshotStatus`, skill audit, `cron.Store`)
- **Ships:** conversation list/history, runtime-status panel, read-only MCP/skills/scheduler boards.

### Phase D — Graph explorer (Frame 06)
**Why fourth:** depends on B (graph_chunk inline displays) + C (REST shape) but is otherwise self-contained; the normalizer is a discrete build.
- `internal/knowledge/graphview.go` normalizer (§2) + read-only Cypher guard
- `GET /api/graph/schema`, `POST /api/graph/query`
- `@neo4j-nvl/react` canvas + inspector + path strip (frontend)
- **Ships:** the Neo4j evidence graph mode.

### Phase E — Approval center + remaining typed displays (swarm, code/artifact)
**Why fifth:** HITL protocol already exists (interrupt/resume); this is mostly UI + the remaining normalizers.
- `swarm_report`, `code`/`local_artifact` normalizers (§1)
- "list all pending across threads" endpoint over `askuser.Store`
- approval-center component (priority, resume-token, accept/decline/cancel)
- **Ships:** swarm worker-report table, execution/artifact displays, the approval queue.

### Phase F — Operator-OS shell + ui_control lane + governance mutations
**Why last:** highest-risk surface; depends on A (auth), B (display envelope), C (governance reads). Build the abuse-prone `ui_control` once everything it touches exists.
- `ui_control` allowlist validator (backend) + Zustand allowlist reducer (frontend) + run-log replay (§6)
- adaptive icon rail, dockable windows, dock chips, command palette (cmdk), background-job feedback
- **mutating governance endpoints** (MCP install/remove, skill install/delete, task approve) behind `RequireAuth` + `capability_grants` + existing approval/audit gate (§4)
- **Ships:** the full operator cockpit with safe agent-driven UI + governed mutations.

**Ordering rationale:** A (serve/auth) → B (protocol) → C (REST reads) are the three infra/protocol pillars; D/E/F are the consuming UI surfaces, each a vertical slice that lights up a frame group. GAP-1 (B) and GAP-2 (A) both precede every UI surface that depends on them, satisfying the hard ordering constraint. Mutations (F) land last, after auth + reads + protocol are all proven.

---

## Architectural Patterns

### Pattern 1: Additive typed slot on the Event (not a new event type)
**What:** GAP-1 rides ONE new `Actions.Display` field + ONE namespaced `CUSTOM` event, exactly like the shipped `aura.artifact`. **When:** any new structured payload from the agent loop to a consumer. **Trade-offs:** keeps AG-UI protocol compatibility (unknown consumers ignore the custom event) + keeps `decode(encode())==identity` (omitempty); the cost is the frontend must understand the envelope, which is the point.

### Pattern 2: Normalize server-side, render client-side
**What:** structure extraction + safety contracts (web-error enum, graph contract, ui_control allowlist) live in Go; React is a pure renderer over a typed envelope. **When:** the structure exists server-side OR carries a safety guarantee. **Trade-offs:** more Go code vs a thin frontend; buys run-log replay + no "regex on prose" + a stable contract across ~50 components.

### Pattern 3: REST for state, SSE for the run
**What:** the live agent turn is SSE (`POST /agent/run`); everything else (conversation list, graph query, governance reads, runtime status) is REST (React Query). **When:** the data is a snapshot, not a turn. **Trade-offs:** two transports, but each is the right tool — SSE for streaming deltas, REST for cacheable reads.

---

## Anti-Patterns

### Anti-Pattern 1: Client-side re-parsing of tool-result prose
**What people do:** infer display type/structure by regexing the `TOOL_CALL_RESULT` text in the browser. **Why wrong:** the backend already held the structure (`ToolInvocation.Meta`, the `web.WebError` enum); re-deriving from prose is the forbidden "regex on natural language" anti-pattern and breaks run-log replay. **Instead:** normalize in the Go translator (Pattern 2); keep client classification only as the v0 fallback for not-yet-normalized tools.

### Anti-Pattern 2: A second metrics/health system for the cockpit
**What people do:** add a new health collector for the panels. **Why wrong:** `/healthz` + `/readyz` + `/metrics` + `/debug/vars` + OTel + slog are ALL shipped. **Instead:** add ONE thin `/api/health/runtime` aggregator that composes the existing sources.

### Anti-Pattern 3: Exposing graph writes (or unauthenticated mutations) over HTTP
**What people do:** let the cockpit run arbitrary Cypher or hit governance mutations on the loopback-trusting mux. **Why wrong:** the gateway trusts loopback; off-loopback that is an injection + privilege surface. **Instead:** read-only Cypher guard (`NEO4J_READ_ONLY` posture at the HTTP boundary); all mutations behind `RequireAuth` + `capability_grants` + the existing approval/audit gate.

### Anti-Pattern 4: Pushing the whole graph as a chat-stream event
**What people do:** stream a 5k-node graph through SSE display events. **Why wrong:** SSE is for turn deltas; a graph workspace is a snapshot read. **Instead:** REST `/api/graph/query` returning a bounded `GraphView` (20-80 evidence nodes per ux-spec); inline chat graph results carry only a compact preview.

---

## Integration Points

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `agui` ↔ `agent` | one-way: `agui` imports `agent`, never reverse (CI-enforced `scripts/agui_boundary_check.sh`) | GAP-1's `Actions.Display` lives in `agent`; `agui` translates it — the boundary is preserved |
| `agui` ↔ `knowledge` | `agui`/REST API calls `knowledge.Read` + the new `graphview` normalizer | read-only Cypher; MCP subprocess holds the only bolt conn |
| `agui` ↔ `webui` | `agui.Mux` mounts `webui.Assets` (embed.FS) | new dependency edge; trivial (static serving) |
| `agui` ↔ `conversations`/`mcp/manager`/`cron`/`askuser`/`toolinvocations` | REST read endpoints over the existing stores (consumer-declared narrow interfaces, the `ConversationStore` pattern at `types.go:26`) | accept interfaces, return structs |
| HTTP mutations ↔ `identity`/`capability_grants` | `RequireAuth` middleware → capability check | the seam that finally activates the auth scaffolding (GAP-2) |

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| `mcp-neo4j-cypher` subprocess | stdio JSON-RPC via `knowledge.Client` (existing) | `NEO4J_READ_ONLY=true` posture for the cockpit graph reads (neo4j-mcp-skill) |
| Reverse proxy (Caddy) | terminates TLS + auth in front of the loopback daemon | the recommended GAP-2 boundary; existing Caddy on-demand TLS story (commit `5f70703f`) |
| Browser (`@ag-ui/client` HttpAgent) | SSE over `POST /agent/run` | same AG-UI protocol as the vendored Go SDK — wire-compatible by construction |

## Sources

- **Backend source (read in full):** `internal/agui/{server.go,translator.go,types.go,readiness.go,metrics.go,fanout.go}`, `internal/agent/{event.go,llm_agent_events.go}`, `cmd/aura/{serve.go,main.go}`, `internal/knowledge/{client.go,schema.go}`, `internal/mcp/manager/status.go`, `internal/cron/dispatch.go`, `internal/swarm/report.go`, `internal/askuser/store.go`, `internal/web/errors.go`, `internal/obs/init.go`, `internal/toolinvocations/store.go`, `internal/config/config.go`.
- **Design package (truth-source):** `docs/design/aura-deep-search-figma/{ux-spec.md, BACKEND_CAPABILITY_MAP.md}` — Implementation Model, display-type mapping (420-438), graph payload contract (446-454), ui_control contract (464-475), backend capability model (503-519).
- **Integration references (D:/tmp):** `llm-graph-builder/frontend/src/utils/Utils.ts` (`processGraphData` → NVL node/edge contract, verified `element_id`/`start_node_element_id`/`end_node_element_id` mapping); `elysia-frontend/app/components/chat/RenderDisplay.tsx` (the `switch(payload.type)` frontend classifier pattern).
- **Parallel research (built upon, not relitigated):** `.planning/research/STACK.md` (embedded SPA decision, integration table, auth options), `.planning/research/FEATURES.md` (GAP-1/GAP-2 framing, feature→backend evidence).
- **Skills:** `neo4j-mcp-skill` (read-only mode, get-schema/read-cypher), `golang-security` (cookie flags, client-header anti-pattern, fail-closed), `golang-context`/`golang-concurrency` (SSE pump/ctx propagation already in place).

---
*Architecture research for: Aura operator web cockpit (cockpit↔backend integration + build order)*
*Researched: 2026-06-15*
