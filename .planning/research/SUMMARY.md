# Project Research Summary

**Project:** Aura Deep Search - Operator Web Cockpit + Supporting Infra  
**Domain:** Rich agentic operator cockpit (embedded Vite/React SPA) over an existing Go single-binary AG-UI/SSE substrate  
**Researched:** 2026-06-15  
**Confidence:** HIGH

---

## Executive Summary

This milestone adds the full "Aura Deep Search" operator cockpit and its two cross-cutting backend gaps onto the already-shipped Go agentic substrate. The substrate is feature-complete (AG-UI SSE gateway, tool registry, conversations, HITL/ask_user, web tools, swarm, Neo4j/MCP, scheduler, skills, observability); the cockpit is the rendering and governance surface that makes it operator-visible. The recommended shape is an **embedded Vite 8 + React 19 + TypeScript SPA** served from `aura serve` via `//go:embed`, connecting to the existing `POST /agent/run` SSE endpoint through `@assistant-ui/react-ag-ui` (chat lane) and raw `@ag-ui/client` (everything else). This preserves the single-binary deploy invariant -- the cockpit becomes just another route family on the daemon already running. The app code is shape-agnostic: pivoting to a separate standalone SPA later is a serving-layer change, not a rewrite.

The milestone is gated by two backend gaps that almost everything else depends on. **GAP-1** (typed-display protocol): the AG-UI translator today emits only one custom event (`aura.artifact`); the cockpit needs a second namespaced CUSTOM event (`aura.display`) carrying a structured `DisplayPayload` envelope, produced by a new Go normalizer (`internal/agent/display/`) that reads the structured tool results the backend already has. **GAP-2** (web auth): the gateway is deliberately loopback-only (amendment #35); the cockpit requires at minimum a reverse-proxy boundary (Caddy, zero Go change) plus a documented in-binary session-cookie upgrade path, and a fail-fast boot guard that blocks any non-loopback bind unless auth is configured. This is a **flagged PROJECT.md scope expansion** -- web auth was explicitly out of scope in v1.

The single most important architectural decision is that **normalization lives in Go, rendering lives in React**. Every structured payload (web results, documents, Neo4j graph paths, swarm reports, system-event enums, `ui_control` verbs) is extracted server-side where the structure already exists, then streamed as typed CUSTOM events. The frontend is a pure renderer -- it never re-parses prose or re-derives structure from tool-result text. This enforces the safe web-error enum, enables run-log replay of `ui_control` events, and avoids the "regex on natural language" anti-pattern. The top build risks are security-load-bearing: CSRF on state-changers, secret leakage through MCP edit forms, `ui_control` becoming arbitrary frontend automation, and the pending skill backend/spec contradiction (P5 in-box auto-activation vs ux-spec non-goal). Each has a specific phase assignment below.

---

## Key Findings

### Recommended Stack

All choices verified live against the npm registry (2026-06-15) and cross-checked against D:/tmp source-level inspection of three reference codebases (elysia-frontend, llm-graph-builder, assistant-ui).

| Technology | Version | Role |
|---|---|---|
| Vite | 8.0.x | Build tool; Rolldown (10-30x faster); dist/ drops straight into embed.FS |
| React | 19.2.7 | UI framework; required by both NVL and assistant-ui |
| TypeScript | 5.9.x | Type safety for display router, ui_control allowlist, graph contract |
| Tailwind CSS | 4.3.1 | CSS-first @theme maps tokens.json 1:1; @tailwindcss/vite plugin only |
| @ag-ui/client + @ag-ui/core | 0.0.57 | SSE transport + TS event types; wire-compatible with vendored Go SDK by construction |
| @assistant-ui/react + @assistant-ui/react-ag-ui | 0.14.21 / 0.0.40 | Chat lane accelerator -- MIT, 10.6k stars, released 2026-06-15. useAgUiRuntime turns the existing SSE gateway into a working chat UI with HITL interrupt/resume, generative UI (typed React components from tool call JSON), and inline human approvals. Also contributes to the typed-display router (GAP-1 client side) and inline approval gates |
| @neo4j-nvl/react + base + layout-workers | 1.2.0 | WebGL graph canvas; NVL is the engine behind Neo4j Bloom; llm-graph-builder uses InteractiveNvlWrapper over mcp-neo4j-cypher -- exactly Aura''s stack. **CUSTOM NEO4J LICENSE** (not MIT) -- must be reviewed before commercial bundling |
| @xyflow/react + dagre | 12.11.0 / 0.8.5 | Decision-tree canvas (Frame 02 only, NOT the graph); Elysia''s exact pairing |
| Zustand | 5.0.14 | Shell/dock/window/selection state; ui_control allowlist reducer |
| @tanstack/react-query | 5.101.0 | All REST-shaped reads (conversations, graph, governance, health) |
| cmdk | 1.1.1 | Command palette; the exact library Elysia uses |
| shadcn/ui | copy-in | Button/Input/Dialog/Panel/Table primitives; not an npm dep |
| //go:embed all:dist in internal/webui/ | Go 1.26.4 | Established codebase pattern (4 existing embed packages); serves from agui.Mux as a catch-all |
| Node 24 Docker build stage | -- | Matches Aura release posture; multi-stage, no Node in runtime image |

**Explicitly rejected:** Next.js (SSR dead weight in a Go single-binary product), Shape C (templ+htmx), React Flow for the Neo4j graph (DAG renderer, not network-graph), Socket.IO (wrong transport), OAuth/RBAC/multi-tenant (out of scope per PROJECT.md).

**NVL fallback (open decision):** If NVL custom license blocks the commercial DGX-Spark bundle, Cytoscape.js (MIT, 3.34.0) is the fallback -- more work (hand-built Neo4j label/degree model), fully permissive. Must be resolved in discuss-phase.

### Expected Features

**Must have (table stakes -- P1, backend mostly exists):**
- aura serve static SPA host + TLS story + GAP-2 web auth + app shell + health/readyz panel
- Chat stream (SSE, POST /agent/run), conversation list/search/rename/archive/delete (thin HTTP adapters over conversations.Store)
- Reasoning drawer + tool-activity stream (events already emitted by translator)
- Approval center -- cross-thread pending list + accept/decline/cancel over the existing Interrupt/Resume[] protocol
- Read-only runtime health panel (aggregating existing /healthz, /readyz, /metrics, /debug/vars)
- Cost/cache footer (per-turn cache_hit_tokens already in STATE_DELTA)

**Should have (differentiators -- P2, all require GAP-1):**
- Typed display router (web_result/document/code/local_artifact/table/chart/system_event/swarm_report/graph_*)
- Neo4j Graph Explorer -- WebGL canvas, path strip, node inspector, Cypher preview, read-only guard (Frame 06)
- Swarm worker-report table (ChildReport -- minimal, no inter-agent chat theater)
- Read-only governance boards: MCP server list+status, skills library (active/pending/archived/audit), scheduler board
- Source Explorer with Table/Metadata/Configuration views

**Defer (v2+ -- P3, highest risk or lowest urgency):**
- Governance write surfaces (MCP install/remove, skill install/delete, task approve via HTTP) -- needs GAP-2 hardened + settled auth model
- ui_control + operator-OS shell (adaptive icon rail, dockable windows, command palette) -- highest abuse surface
- Full web setup/onboarding wizard (overlaps Phase 14/17 Planned surfaces)
- Observability dashboard render panel

**Anti-features enforced per ux-spec Non-Goals (13 guards across phases B-F):**
Swarm talk/mailbox; generic cards for all payloads; Neo4j as a decorative hairball; arbitrary ui_control automation; raw MCP secrets in the UI; silent destructive MCP tool mounting; model-direct skill activation; multimodal user input on /agent/run (endpoint rejects it today at server.go:33).

### Architecture Approach

The cockpit integrates as a second route family on the same http.Server in cmd/aura/serve.go. One port, one process, one binary. Six integration mechanisms in dependency order:

**Major components:**
1. **internal/webui/ embed package** -- //go:embed all:dist (established pattern, 4 existing usages); agui.Mux SPA-fallback handler excluding API/agent/health prefixes so API 404s stay real errors
2. **Auth middleware (internal/agui/auth.go)** -- RequireAuth wraps the mutating route subtree; start with Caddy reverse-proxy (zero Go change); upgrade to in-binary signed session cookie (HttpOnly + Secure + SameSite=Strict) bound to an identity row, activating the capability_grants scaffolding dormant since Phase 1.7; non-loopback bind fails fast at boot unless auth is configured
3. **Typed-display event protocol** -- ONE additive Actions.Display *DisplayPayload field (omitempty, same precedent as ArtifactDelta); ONE translator branch emitting aura.display CUSTOM event (mirrors shipped aura.artifact pattern); Go normalizer classifying tool results by ToolName+Meta; frontend switch(payload.type) renderer; v0 fallback classifier for un-normalized tools only
4. **REST read API (internal/agui/api_*.go)** -- conversation list/search, /api/health/runtime aggregator composing existing /healthz+/readyz+manager.SnapshotStatus, graph queries with read-only Cypher guard, MCP/skills/scheduler board reads; React Query handles caching/retry on the client
5. **Graph normalizer (internal/knowledge/graphview.go)** -- []map[string]any Cypher rows to {nodes, edges, paths, schema, query} NVL contract; delivered as REST not SSE (graph explorer is a workspace mode, not a chat turn)
6. **ui_control allowlist lane** -- backend validator + frontend Zustand reducer; closed allowlist of 6 verbs; run-log replay; lands last in Phase F

**Key patterns:** normalize server-side / render client-side; REST for state / SSE for the run; additive typed slot not a new event type; four-layer write protection (proxy -> auth middleware -> capability_grants -> existing approval gate).

