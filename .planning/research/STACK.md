# Stack Research

**Domain:** Operator web cockpit ("Aura Deep Search") — a typed-display agentic frontend + serving infra over an existing Go single-binary AG-UI/SSE backend
**Researched:** 2026-06-15
**Confidence:** HIGH (versions verified live against npm registry + Context7 + design specs read in full; D:/tmp sources inspected at source level)

---

## TL;DR — The load-bearing decision

**Primary recommendation: Shape (A) — Embedded SPA in the Go binary.**
A **Vite 8 + React 19 + TypeScript** SPA, built to static assets and embedded via Go `embed.FS`, served by `aura serve` with an SPA-fallback handler. It connects to the *existing* `internal/agui` SSE gateway through the official `@ag-ui/client` `HttpAgent` (the TypeScript twin of the Go SDK Aura already vendors).

**Runner-up: Shape (B) — Separate SPA.** Same JS stack, deployed standalone behind the reverse proxy. Identical app code; only the serving + auth boundary differs.

**Rejected: Shape (C) — templ + htmx + vanilla JS.** It cannot meet the design's hard requirements — an interactive Neo4j node-link graph canvas (NVL/WebGL), a stateful dockable-window manager that survives minimize/restore, a typed-payload display router, and `cmdk` — without re-implementing a component framework in vanilla JS. Odysseus (the cited static-first source) proves vanilla *can* do an operator shell, but at the cost of ~40 hand-rolled JS modules with no type safety; that is the opposite of "minimal industrial shape" for a surface this rich.

**The decisive trade-off (A vs B):** A preserves Aura's single-binary deploy and the loopback-bind compensating control the gateway already relies on (no new public attack surface); the price is adding a Node build stage to CI and the Docker image. B avoids touching the Go build but breaks the single-binary promise (two artifacts, CORS, a separate deploy/TLS story) for zero functional gain. The design ethos ("single-binary, minimal industrial shape, no atomic bombs") points at A. Crucially, **the app code is identical** — picking A does not lock out B; flipping to B later is a serving-layer change, not a rewrite.

---

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **React** | 19.2.7 | UI framework | Every curated source (Elysia, llm-graph-builder, assistant-ui) is React. The two accelerators that make this milestone cheap — `@neo4j-nvl/react` and `@assistant-ui/react-ag-ui` — are React-only. React 19 stable since Dec 2024; 19.2 (Oct 2025) is the floor for the current ecosystem. |
| **TypeScript** | 5.9.x | Type safety | The design is a *typed*-display router + a `ui_control` allowlist + a graph payload contract with strict node/edge/path schemas. Types are the cheapest way to keep those contracts honest across ~50 components. Vanilla JS (Shape C) forfeits this. |
| **Vite** | 8.0.x | Build tool + dev server | The 2026 default for SPAs. v8 (2026-03-12) ships Rolldown (Rust bundler), 10–30× faster builds — matters for the CI build stage on a mini-PC. `output: dist/` is a flat static tree that drops straight into `embed.FS`. llm-graph-builder (Neo4j's own tool) uses Vite. **Deliberately NOT Next.js** — see "What NOT to Use". |
| **`@vitejs/plugin-react`** | 6.0.x | React/JSX + Fast Refresh for Vite | Standard React-on-Vite plugin. |
| **Tailwind CSS** | 4.3.1 | Styling | The design ships `tokens.json` (colors, radius, spacing, typography, density modes). Tailwind v4's CSS-first `@theme` maps those tokens 1:1 with zero JS config. v4 install is `@tailwindcss/vite` (4.3.1) — one plugin, no PostCSS chain. Elysia (v3) and llm-graph-builder (v4) both use Tailwind. |
| **`@ag-ui/client`** | 0.0.57 | SSE transport to Aura's gateway | The official TS client for the AG-UI protocol. Its `HttpAgent` connects to an AG-UI SSE endpoint exactly like Aura's `POST /agent/run`. Aura's Go side already vendors `ag-ui-protocol/ag-ui/sdks/community/go` (go.mod) — client and server are the **same protocol's two reference SDKs**, so the wire is compatible by construction, not by adaptation. |

### Graph Visualization (hard requirement — Frame 06 Neo4j Graph Explorer)

| Library | Version | Purpose | Why Recommended |
|---------|---------|---------|-----------------|
| **`@neo4j-nvl/react`** + **`@neo4j-nvl/base`** | 1.2.0 | Interactive Neo4j node-link graph canvas + selected-node inspector | **This is the choice.** NVL (Neo4j Visualization Library) is the engine behind Neo4j Bloom and Explore — WebGL-rendered, built for exactly the labels/edges/paths/degree/confidence model the spec's graph payload contract describes. The Neo4j team's own `llm-graph-builder` uses `InteractiveNvlWrapper` (`@neo4j-nvl/react`) over a `mcp-neo4j-cypher`-shaped result — the *same backend Aura has*. Peer dep: React 18/19 (verified). It gives node colouring by label family, degree-based sizing, deterministic layout, hit-testing → inspector, and zoom/fit out of the box. Reaching parity with Cytoscape/sigma is weeks of work. |
| **`@neo4j-nvl/layout-workers`** | 1.2.0 | Off-main-thread force layout | Keeps the layout simulation off the UI thread (mini-PC CPU budget: do not saturate the main thread). |

### Chat surface accelerator

| Library | Version | Purpose | Why Recommended |
|---------|---------|---------|-----------------|
| **`@assistant-ui/react`** | 0.14.21 | Chat thread primitives (message list, composer, streaming, auto-scroll, branch/edit, interrupt handling) | MIT. Purpose-built React library for agent chat UIs. Saves the entire chat-stream lifecycle (the part of Frame 01 that is generic). |
| **`@assistant-ui/react-ag-ui`** | 0.0.40 | Wires an `@ag-ui/client` `HttpAgent` into the assistant-ui runtime | **The single biggest accelerator in this milestone.** `useAgUiRuntime({ agent: new HttpAgent({ url: "/agent/run" }) })` turns Aura's existing SSE gateway into a working chat UI with run-timeline, tool lifecycle, reasoning, and protocol-native interrupt/resume — i.e. the `ask_user` HITL the translator already emits as AG-UI `Interrupt`. README explicitly lists "custom Python/**Go**/TS agents" as supported backends. |

> **Adoption discipline (memory: feedback_no_atomic_bombs):** adopt assistant-ui for the *chat lane only* (Frame 01 message stream + composer + HITL). The typed-display router, graph mode, dockable tools, source explorer, MCP/skills governance, and `ui_control` lane are Aura-specific and built directly on the raw `@ag-ui/client` event stream — do NOT try to force them through assistant-ui's thread model. Treat assistant-ui as a removable accelerator behind Aura's own shell, not the shell itself.

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **`@ag-ui/core`** | 0.0.57 | AG-UI event type definitions (TS) | Type the raw event stream for the display router, `ui_control` lane, and graph/MCP/skill payloads. Mirror of the Go SDK's `events` package. |
| **`cmdk`** | 1.1.1 | Command palette (⌘K) + slash actions | MIT. The exact library Elysia uses; the spec's "command palette searches runs, sources, tools, actions". Unstyled, accessible, fast. |
| **`@xyflow/react`** (React Flow) | 12.11.0 | Tree/decision-trace canvas (Frame 02), MCP/flow diagrams | This is the DAG/flow renderer Elysia uses (`FlowDisplay.tsx` + `dagre`) for its decision tree. Use it for Frame 02 **only** — it is a layered-flow renderer, **not** a network-graph renderer; do not use it for Frame 06 (that is NVL's job). |
| **`dagre`** | 0.8.5 | Layered layout for the React Flow tree | Pairs with `@xyflow/react`; Elysia's exact pairing. |
| **Zustand** | 5.0.14 | Client state (shell/dock/window state, mode, density, selection) | The dockable-window manager, dock chips that "preserve state", active-selection guards, and density boot are client-side concerns. Zustand is the minimal-shape store (no boilerplate, no context churn). `ui_control` events mutate this store through an allowlist reducer. |
| **`@tanstack/react-query`** | 5.101.0 | Server-state for non-stream reads | Conversations list, MCP server rows, skills library, scheduler tasks, source-explorer rows, `GET /threads/{id}/messages` snapshot. SSE handles the live run; React Query handles the REST-shaped reads. Caching + retry/stale logic for free. |
| **`@tanstack/react-router`** | 1.170.15 | Type-safe routing | Primary modes (chat/tree/graph/displays/settings) + deep-links to a run/source/node. Type-safe params suit the `ui_control` `set_mode` allowlist. *Optional* — for a 5-mode shell, a small switch on Zustand state is also acceptable; pick the router only if deep-linking/back-button is required. |
| **`react-markdown`** | 10.1.0 | Render `document`/`web_fetch` markdown displays | `web_fetch` already returns markdown; this renders it. Elysia + llm-graph-builder both use it. |
| **`lucide-react`** | latest (0.5xx line; pin at adoption) | Icon set | Industrial line icons; matches the dark-cockpit tone. The icon rail + tool chips need a consistent set. |
| **shadcn/ui** | (copy-in, not a dep) | Button/Input/Dialog/Tabs/Table/Popover primitives | shadcn is *copied source* (Radix + Tailwind under the hood), not an npm dependency — fits "own your components". Use it to materialize the `Aura/Button`, `Aura/Chip`, `Aura/Panel`, `Aura/Table` component families from FIGMA_PROJECT.md. Radix primitives (what Elysia uses directly) are the alternative if you prefer not to copy. |

### Go-side serving / embedding

| Component | Approach | Why |
|-----------|----------|-----|
| **`//go:embed`** | New package `internal/webui` (mirrors picobot's `embeds` pattern, `D:/tmp/picobot/embeds/embeds.go`) holding `//go:embed all:dist` → `embed.FS`. | Aura already uses `//go:embed` in 4 packages (`db/migrate.go`, `knowledge/migrate.go`, `skills/builtin.go`, `conversations/tiktoken.go`) — this is an established codebase pattern, not a new dependency. `all:` includes dotfiles Vite may emit. |
| **SPA fallback handler** | `fs.Sub(dist, "dist")` → `http.FileServer(http.FS(sub))`, with a wrapper that serves `index.html` on 404 for non-asset, non-API paths (the PocketBase pattern). | Client-side routing (TanStack Router) needs deep-link refreshes to fall back to `index.html`. Exclude `/agent/`, `/threads/`, `/healthz`, `/readyz`, `/metrics`, `/debug/` from the fallback so API 404s stay real. |
| **Mount point** | Register the static handler on the *same* `agui.Server.Mux()` (or a sibling mux on the same `http.Server` in `serve.go`), behind the same loopback bind. | Reuses the existing `http.Server` in `cmd/aura/serve.go` (`env.httpSrv`). One port, one process, one binary — the single-binary invariant holds. The cockpit becomes "just another route family" on the daemon already running. |

---

## Installation

```bash
# scaffold (in a new web/ dir at repo root)
npm create vite@latest web -- --template react-ts

# Core
npm install react@19 react-dom@19
npm install @ag-ui/client @ag-ui/core

# Chat accelerator (chat lane only)
npm install @assistant-ui/react @assistant-ui/react-ag-ui

# Graph (Frame 06 — Neo4j explorer)
npm install @neo4j-nvl/react @neo4j-nvl/base @neo4j-nvl/layout-workers

# Tree/flow (Frame 02 — decision trace)
npm install @xyflow/react dagre

# Shell + state + UX
npm install zustand @tanstack/react-query cmdk lucide-react react-markdown
npm install @tanstack/react-router   # optional, if deep-linking required

# Styling (Tailwind v4 — vite plugin, no PostCSS)
npm install -D tailwindcss @tailwindcss/vite

# Dev
npm install -D vite @vitejs/plugin-react typescript
```

```go
// internal/webui/embed.go
package webui

import "embed"

//go:embed all:dist
var Assets embed.FS
```

---

## Build / CI / Single-binary pipeline

The single-binary constraint is satisfied by building the JS *before* the Go build and committing the contract that `internal/webui/dist/` exists at compile time.

**Local + CI sequence:**
```
1. cd web && npm ci && npm run build           # Vite → web/dist
2. copy web/dist → internal/webui/dist          # (Makefile target, or build into place)
3. go build ./...                                # //go:embed all:dist bakes it into the binary
```

**Makefile:** add a `web` target (`npm ci && npm run build && rsync dist → internal/webui/dist`) and make `build`/`quality-full` depend on a present `dist`. Gate it so Go-only contributors are not forced to install Node for unrelated changes (ship a checked-in `dist` placeholder or a `web-build` CI artifact).

**Docker (multi-stage):** the current `Dockerfile` has **no Node stage** (verified) — add one:
```dockerfile
FROM node:24-alpine AS web        # Node 24 matches the repo's Node-24 CI/release posture
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build                  # → /web/dist
# ... existing Go builder stage ...
COPY --from=web /web/dist /src/internal/webui/dist
RUN go build ...                   # embeds dist
```
This adds one cached layer; the cold image cost is bounded by `npm ci` (the Go build is unchanged). Honors memory `feedback_preserve_docker_build_cache` — the web layer caches on `package*.json`.

**Staleness risk:** Vite 8 + React 19 + Tailwind 4 are all current majors (2025–2026), so the chosen stack has the longest staleness runway available today. The one watch-item is the `@ag-ui/*` and `@assistant-ui/*` packages (pre-1.0, `0.0.x`/`0.14.x`) — pin exact versions and treat upgrades as deliberate; the *protocol* is stable (Aura's Go SDK speaks it), only the client API surface churns.

---

## Integration points with `internal/agui` (verified against source)

| Cockpit need | Existing backend seam | Notes |
|--------------|----------------------|-------|
| Live run stream | `POST /agent/run` → SSE via `streamSSE` (`server.go`) | `@ag-ui/client` `HttpAgent({ url: "/agent/run" })` consumes it directly. The translator emits the full lifecycle (RUN_STARTED → text/reasoning/tool/state deltas → RUN_FINISHED). |
| Run timeline (reasoning, tool start/args/end/result, state delta) | `translator.go` already maps these to AG-UI `TEXT_MESSAGE_*`, `REASONING_*`, `TOOL_CALL_*`, `STATE_DELTA` | The frontend run-timeline (Frame 01 + BACKEND_CAPABILITY_MAP `run_timeline_event`) renders these event types directly. |
| HITL approval center | Pause → `RUN_FINISHED` with `Interrupt` (`interruptFrom`, `responseSchema`); resume via `Resume[]` → `SubmitAnswers` | The `ask_user` approval queue maps to AG-UI interrupt/resume — assistant-ui's runtime handles this idiom; the schema-constrained options come through `ResponseSchema`. |
| Artifact delivery | `CUSTOM` event `"aura.artifact"` (`ArtifactEventName`) | The display router keys on this custom event name to render a `local_artifact` display. |
| History snapshot | `GET /threads/{id}/messages` → `MESSAGES_SNAPSHOT` JSON | React Query fetch on thread open / rehydrate. |
| Health / readiness | `GET /healthz`, `GET /readyz` (PG + Neo4j probes), `/metrics`, `/debug/vars` | Frame 07 / runtime-status surface (`runtime_status`) reads these. |
| Typed-display payload router | **No backend change for v1** — classify off the existing event stream + tool name (`web_search`→`web_result`, `web_fetch`→`document`, Neo4j MCP→`graph_*`, etc., per ux-spec mapping) | The spec's "richer AG-UI typed-display event protocol" is a *roadmap* item; v1 can classify client-side from tool-call names + StateDelta markers (the translator already stamps `tool_call_id`). A typed `CUSTOM` display event is the clean v2 once the client-side classifier proves the taxonomy. |
| `ui_control` lane | **New** — a namespaced AG-UI `CUSTOM` event (`aura.ui_control`), allowlist-validated client-side | Mirrors the existing `aura.artifact` custom-event pattern. The allowlist (`open_panel`/`highlight_source`/`set_mode`/`show_job`/`set_density`/`theme_preview`) is enforced in the Zustand reducer; events are replayable from the run log per spec. |
| Auth boundary | Gateway is **loopback-bind, auth-deferred** (amendment #35; the bind *is* the control) | See web-auth section — the cockpit milestone is where this graduates beyond loopback. |

---

## Web-auth approaches (stack-level outline)

The gateway today binds loopback with no auth (the bind is the compensating control). A web cockpit that an operator reaches over a network needs a real boundary. Three shapes, in increasing Aura-fit order:

| Approach | Shape | Fit for Aura | Trade-off |
|----------|-------|--------------|-----------|
| **Reverse-proxy-enforced** (Caddy/oauth2-proxy/basic-auth in front) | Aura stays loopback; the proxy terminates TLS + auth | **Recommended starting point.** Aura already documents Caddy on-demand TLS (recent commit `5f70703f`). The daemon keeps its loopback posture *unchanged* (no new code, no new attack surface in Go), and the operator boundary lives in infra. Matches "minimal industrial shape". | Auth lives outside the binary — fine for the DGX-Spark single-operator bundle, weaker for true multi-tenant (which PROJECT.md explicitly defers: "Multi-user con auth/RBAC reale" is out of scope). |
| **Session cookie** (server-set, HttpOnly + Secure + SameSite) | Go middleware on the mux issues/validates a signed session cookie | The right *in-binary* choice if/when the proxy is not desired. `golang-security` skill: HttpOnly + Secure + SameSite, constant-time compare, fail-closed. Pairs naturally with a browser SPA (no token storage in JS). | Adds session store + login surface to the daemon — real scope. Only worth it when the proxy boundary is insufficient. |
| **Bearer token** (header on `fetch`/`HttpAgent`) | A static/operator token validated by Go middleware | Simplest in-binary option; `HttpAgent` supports custom headers. | Token must live in the browser (XSS-exposed); inferior to a cookie for a browser app. Reserve for machine/API callers, not the human cockpit. |

**Recommendation:** ship the cockpit behind the **reverse proxy** boundary first (zero change to the auth-deferred Go posture, aligns with the existing Caddy story and the single-operator bundle), and design a thin Go session-cookie middleware as the documented in-binary upgrade path. Do **not** build OAuth/RBAC/multi-tenant — PROJECT.md puts that out of scope for v1.

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| **Shape A (embedded SPA)** | Shape B (separate SPA) | When the cockpit must scale/deploy independently of the daemon, or be served from a CDN. Same app code — purely a serving/auth-boundary decision; defer until there's a concrete reason to split. |
| **Vite 8** | Next.js 15 (`output: export`) | Only if SSR/SSG/file-routing is wanted. Elysia uses it — but Elysia *also* compiles to a static `out/` it copies into a backend (`export.sh`), i.e. it uses Next purely as a static builder. For a pure SPA embedded in a Go binary, Next's SSR machinery is dead weight; Vite is the leaner fit. |
| **`@neo4j-nvl/react`** | Cytoscape.js 3.34 / `react-cytoscapejs` 2.0 | If a permissive OSI license is a hard procurement requirement (Cytoscape is MIT; NVL is a custom Neo4j license — see below). Cytoscape is canvas-rendered (degrades past ~3–5k nodes) and you build the Neo4j label/degree/path model yourself. |
| **`@neo4j-nvl/react`** | sigma.js 3.0 + graphology 0.26 | If the graph routinely exceeds ~50k nodes and raw WebGL throughput dominates. The spec explicitly says default to 20–80 evidence-path nodes and *avoid hairballs* — so NVL's scale is more than enough and its Neo4j-native model wins. |
| **assistant-ui** | Raw `@ag-ui/client` + hand-built chat | If assistant-ui's thread model fights Aura's shell. Keep it removable; the raw client always works (it is the foundation assistant-ui sits on). |
| **Zustand** | Redux Toolkit / Jotai | Redux only if a large team needs strict conventions; overkill here. Jotai if atom-granular re-render tuning becomes a measured bottleneck. |
| **Tailwind v4** | CSS Modules / vanilla-extract | If the team prefers scoped CSS over utilities. But `tokens.json` → Tailwind `@theme` is the lowest-friction path given the design package. |

---

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **Shape C: templ + htmx + vanilla JS** | Cannot deliver the WebGL Neo4j canvas, stateful dockable windows, typed-display router, and `cmdk` without re-inventing a component runtime in untyped JS. Odysseus proves it's *possible* but at ~40 bespoke modules with no types — an anti-pattern for a surface this rich. | Vite + React SPA (Shape A). |
| **Next.js as the runtime** (server mode) | Pulls a Node server into a Go single-binary product — defeats the deploy model. Even `output: export` adds SSR scaffolding you never run. | Vite SPA → `embed.FS`. |
| **React Flow (`@xyflow/react`) for the Neo4j graph** | It's a layered DAG/flow renderer, not a force-directed network graph. Using it for Frame 06 produces a wrong-shaped, non-scaling graph. | `@neo4j-nvl/react` for Frame 06; keep React Flow for the Frame 02 decision *tree* only. |
| **Three.js / `@react-three/*`** (Elysia's sphere) | Pure decoration; ux-spec Non-Goals explicitly forbid copying Elysia's abstract sphere. Heavy GPU/bundle cost on a mini-PC. | Nothing — drop it. |
| **Socket.IO / raw WebSocket** (Elysia's transport) | Aura's backend is SSE (`text/event-stream`), not WS. Adopting Elysia's transport would mean re-plumbing the gateway. | `@ag-ui/client` `HttpAgent` over the existing SSE. |
| **A second graph lib alongside NVL** | Two graph engines = double the bundle + double the maintenance. | One network-graph lib (NVL) + one flow lib (React Flow) for distinct jobs. |
| **OAuth/RBAC/multi-tenant auth** | PROJECT.md puts real multi-user auth out of scope for v1. | Reverse-proxy boundary now; session-cookie middleware as the documented upgrade. |
| **CSR data-fetching without React Query** | Hand-rolled fetch/loading/error/retry across ~8 governance surfaces = bug surface. | `@tanstack/react-query` for all REST-shaped reads. |

---

## Licensing note (decision-grade)

| Package | License | Implication |
|---------|---------|-------------|
| React, Vite, Tailwind, cmdk, assistant-ui, `@ag-ui/*`, Zustand, TanStack, `@xyflow/react` | MIT (xyflow: MIT) | No constraint. |
| **`@neo4j-nvl/*` (1.2.0)** | **Custom Neo4j license** (`SEE LICENSE IN 'LICENSE.txt'`, verified via npm registry) — *not* OSI/MIT | Free to install from public npm with no auth (verified) and used in Neo4j's own OSS `llm-graph-builder`. But it is a Neo4j-authored license, not MIT/Apache. **Read `LICENSE.txt` before shipping the commercial DGX-Spark bundle** (PROJECT.md's commercial target). If the custom license is a blocker for the bundle, the fallback is **Cytoscape.js (MIT)** with a hand-built Neo4j adapter — more work, fully permissive. Flag this as a roadmap decision gate, not a silent default. |

---

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `@neo4j-nvl/react@1.2.0` | `react@18 \|\| ^19` | Peer dep verified — React 19 OK. |
| `@assistant-ui/react-ag-ui@0.0.40` | `@ag-ui/client@^0.0.5x`, `react@18 \|\| 19` | Devdep tracks react 19; peer allows 18\|19. |
| `@ag-ui/client@0.0.57` (TS) | `ag-ui-protocol/go SDK @ v0.0.0-2026...` (Go, vendored) | Same protocol, two official SDKs — wire-compatible by spec. Both pre-1.0; pin exact and upgrade deliberately. |
| Tailwind `4.3.1` + `@tailwindcss/vite@4.3.1` | Vite 8 | v4 uses the Vite plugin (no PostCSS config). |
| Vite `8.0.x` + `@vitejs/plugin-react@6.0.x` | React 19 | Current pairing. |
| Go `1.26.4` + `//go:embed all:dist` | — | `all:` prefix includes dotfiles; matches existing Aura embed usage. |
| Node `24` (CI/Docker build stage) | Vite 8 / npm | Matches Aura's Node-24 release posture (commit `5f70703f`). |

---

## Sources

- **Design package (truth-source, read in full):** `docs/design/aura-deep-search-figma/{ux-spec.md, README.md, FIGMA_PROJECT.md, BACKEND_CAPABILITY_MAP.md, tokens.json, odysseus-pattern-study.md}` — requirements, component inventory, graph payload contract, `ui_control` allowlist, copy contract, non-goals.
- **Aura backend source (read):** `internal/agui/server.go` (SSE pump, CORS, health/ready, redaction), `internal/agui/translator.go` (AG-UI event mapping, interrupt/resume, `aura.artifact` custom event), `cmd/aura/serve.go` (the live `http.Server` to mount on), `go.mod` (Go 1.26.4; vendored `ag-ui-protocol/go` SDK).
- **D:/tmp sources inspected at source level:**
  - `elysia-frontend/` — Next.js 14 `output: export` → static copy into backend (`export.sh`); React 18 + Tailwind 3 + Radix + `cmdk`; `@xyflow/react`+`dagre` for the *decision tree*; transport is **Socket.IO/WebSocket** (`SocketContext.tsx`), not SSE. Proves the typed-display + static-export-into-backend pattern; proves React Flow is for the tree, not the graph.
  - `llm-graph-builder/frontend/` — **Vite 4 + React 18 + Tailwind 4 + `@neo4j-nvl/react` (`InteractiveNvlWrapper`) + `@neo4j-ndl/react`**; the Neo4j team's own tool over a Cypher backend. Decisive evidence for NVL as the graph engine.
  - `assistant-ui/packages/react-ag-ui/` — `@assistant-ui/react-ag-ui` wraps `@ag-ui/client` `HttpAgent`; README lists Go agents as supported. Decisive evidence the chat lane is near-free over Aura's existing SSE.
  - `odysseus/static/` — pure vanilla JS, ~40 hand-rolled modules, no build/types (`package.json` has only an Anthropic SDK dep). Proves vanilla *can* do the operator-shell mechanics but at high hand-rolled cost → argues against Shape C for a surface this rich.
  - `picobot/embeds/embeds.go` — minimal `//go:embed` pattern reference for the Go-side embed package.
  - `nanobot/webui/` — has a separate JS webui (Shape-B-style); confirms the separate-SPA shape exists but is not single-binary.
- **Version verification (live npm registry / Context7 / WebSearch, 2026-06-15):**
  - Vite 8.0.16 (Rolldown; 2026-03-12) — vite.dev/blog/announcing-vite8 [MEDIUM→HIGH, multi-source]
  - React 19.2 stable (Oct 2025) — react.dev/versions [HIGH]
  - Tailwind 4.3.1 (Dec 2025) — tailwindcss.com/blog [HIGH]
  - `@neo4j-nvl/{base,react}` 1.2.0, custom license, peer react 18\|19 — npm registry (curl) [HIGH]
  - `@ag-ui/client` 0.0.57, `@ag-ui/core` 0.0.57 — npm registry [HIGH]
  - `@assistant-ui/react` 0.14.21 (MIT), `@assistant-ui/react-ag-ui` 0.0.40 — npm registry [HIGH]
  - `cmdk` 1.1.1 (MIT) — jsDelivr [HIGH]
  - `@xyflow/react` 12.11.0, `cytoscape` 3.34.0, `sigma` 3.0.3, `graphology` 0.26.0, `react-force-graph` 1.48.2 — npm registry [HIGH]
  - Zustand 5.0.14, `@tanstack/react-query` 5.101.0, `@tanstack/react-router` 1.170.15, `@vitejs/plugin-react` 6.0.2 — npm registry [HIGH]
  - Graph-lib performance comparison (sigma WebGL > cytoscape canvas ~3–5k node ceiling) — pkgpulse / memgraph [MEDIUM]
  - Go+Vite `embed.FS` SPA-fallback pattern — PocketBase + ofeng.org/matteogassend write-ups [HIGH, well-established]

---
*Stack research for: Aura operator web cockpit (frontend + serving infra)*
*Researched: 2026-06-15*
