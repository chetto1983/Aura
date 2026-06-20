# Roadmap: Aura

## Milestones

- ✅ **v0.0.0 Substrate** — Phases 0–21 (shipped 2026-06-15) — full details in [`milestones/v0.0.0-ROADMAP.md`](milestones/v0.0.0-ROADMAP.md)
- 📋 **v1.0.0 Aura Deep Search Web Cockpit** — Phases 22–30 (planning)

## Phases

<details>
<summary>✅ v0.0.0 Substrate (Phases 0–21) — SHIPPED 2026-06-15</summary>

- [x] Phase 0: PRD Amendments (6/6) — 2026-05-29
- [x] Phase 1: Infra DB + Knowledge — 2026-05-30
- [x] Phase 2: Agent Cornerstone (8/8) — 2026-05-30
- [x] Phase 3: LLM Client + ToolResult (5/5) — 2026-05-30
- [x] Phase 4: HITL + Identity + Conversations (5/5) — 2026-05-30
- [x] Phase 6: KV Cache Builder (5/5) — 2026-06-02
- [x] Phase 7: Web Tools (4/4) — 2026-06-02
- [x] Phase 07.1: Agent-Loop Forced Finalization (INSERTED) — 2026-06-03
- [x] Phase 8: Sandbox via sandbox-agent (replaces bespoke 2a/2b) — 2026-06-03
- [x] Phase 08.1: Tool Search hardening — defer_loading parity (INSERTED) — 2026-06-03
- [x] Phase 08.2: Semantic tool_search + unified semindex (INSERTED) — 2026-06-05
- [x] Phase 9: Swarm (Minimal) (6/6) — 2026-06-04
- [x] Phase 10: Scheduler (6/6) — 2026-06-04
- [x] Phase 11: Skills (10/10) — 2026-06-06
- [x] Phase 12: AG-UI Gateway (6/6) — 2026-06-07
- [x] Phase 13: Channels + Telegram + Multimodal (10/10) — 2026-06-08
- [x] Phase 14: Onboarding + Agent.md (5/5) — 2026-06-14
- [x] Phase 15: Memory Subsystem (5/5) — 2026-06-12
- [x] Phase 16: MCP Sidecar Manager + Third-Party Trust (8/8) — 2026-06-04
- [x] Phase 17: Packaging & Distribution (8/8) — 2026-06-14
- [x] Phase 18: Slice 7e Executable Snippet Reuse — 2026-06-08
- [x] Phase 19: Audit Bug Resolution + E2E Live Test — 2026-06-10
- [x] Phase 20: Scheduler Hardening Full Implementation — 2026-06-11
- [x] Phase 21: Plugins — Hooks (Slice EXT-1) — 2026-06-15

> Phase 5 (bespoke Sandbox 2a) was superseded by the Phase 8 sandbox-agent pivot (D-15) and is not counted.

</details>

### 📋 v1.0.0 Aura Deep Search Web Cockpit (Planning)

Embedded Vite + React + assistant-ui operator cockpit over the AG-UI/SSE gateway, per `docs/design/aura-deep-search-figma/ux-spec.md`. The operator directive (2026-06-15) is to **stand up the industrial frontend foundation FIRST** — research-locked toolchain, linter/formatter CI gate, design-token dark-operator theme, brand, and the embedded build/test pipeline — before any cockpit feature code. Build order: harden the agent perimeter first (Phase 22), then the research-first frontend industrial foundation (Phase 23), then the serve/auth/health web host (Phase 24), then the Core-Value chat+approval loop (Phase 25), then the GAP-1 typed-display spine + router (Phase 26), then the self-contained graph explorer (Phase 27), then read-only governance + web onboarding (Phase 28), and finally the governance WRITE surfaces — MCP configuration + skills install/lifecycle (Phase 29), the highest-risk surfaces landing last after auth + the approval center + the read-only boards are proven. The `ui_control` operator-OS shell (SHELL) and scheduler write surfaces (GOVW-03) are deferred to a follow-up milestone.

- [x] **Phase 22: Agent Perimeter Hardening** — Remediate the `internal/agent` production-readiness audit so the web exposure lands on a hardened base (HARDEN-01..12) — all 5 plans executed + automated-green 2026-06-15 (AG-001..064 ledgered, none dropped); **Gate-3 CLOSED 2026-06-16** (operator Part-B live sign-off: B1 + B2 + B3 9/9 live rows, commit `28c1f7c7` — `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`)
- [x] **Phase 23: Frontend Infrastructure & Industrial Foundation** — Research-first industrial frontend foundation: locked decision record + Vite/React/TS embed scaffold + linter/formatter/type-check CI gate + design-token dark-operator theme + brand + Node-24 build/test pipeline, BEFORE any feature code (FND-01..06) (completed 2026-06-16)
- [x] **Phase 24: Web Foundation — Serve + Auth + Health** — Single-binary SPA host on `aura serve` (SPA-fallback route exclusion) with the GAP-2 web-auth boundary + non-loopback boot guard + runtime health shell (WEB-01..04)
 (completed 2026-06-16) — _the GAP-2 HMAC passphrase cookie is being superseded by embedded **Authula** (flag-gated, default still passphrase) in the post-25 cockpit overhaul — see `docs/cockpit-overhaul/05-authula-auth-SPEC.md`._

- [x] **Phase 25: Chat + Approval Center** — assistant-ui chat lane over SSE + conversation management + cost/cache footer + cross-thread HITL approval queue + conversation branch trees (CHAT-01..05, APRV-01..03) (completed 2026-06-17; 8/8 verified, 6 live UAT carried into the cockpit-overhaul live cutover) — _chat/footer/tool-cards enhanced in place by the post-25 cockpit overhaul — see `docs/cockpit-overhaul/{01,04}-*.md`._
- [x] **Phase 26: Typed-Display Protocol + Router** — GAP-1 `aura.display` event + Go normalizer + frontend display router for web/document/code/table/chart + system-event cards + source explorer + swarm report (DISP-01..05, SWARM-01) (completed 2026-06-18)
- [x] **Phase 27: Neo4j Graph Explorer** — Go graph-normalizer + read-only Cypher guard + WebGL canvas + node inspector + path strip (GRAPH-01..04) (completed 2026-06-19)
- [ ] **Phase 28: Governance Boards + Web Onboarding** — Read-only MCP / skills / scheduler boards + web setup/onboarding wizard over the existing onboarding LoopAgent (GOV-01..03, ONBD-01..02)
- [ ] **Phase 29: Governance Write — MCP Configuration + Skills Install** — Cockpit write surfaces over the existing MCP manager + scoring-gated skill install/approval/audit backend: recipe/custom MCP install with CLI + managed-config preview, redacted env editing, enable/disable/remove, skills install → risk-tiered approval queue → activate, restore/archive, immutable audit (MCPW-01..03, SKW-01..03)
- [x] **Phase 30: Telegram Onboarding on Frontend (Link + QR)** — ✅ **absorbed-into-28** (D-09): Telegram link/QR is delivered as **ONBD-01b** inside Phase 28's onboarding wizard. See `28-SPEC §ONBD-01b`; `30-SPEC.md` is a tombstone. _(Original scope: surface Telegram account linking in the web cockpit — deep-link + scannable QR over the existing Telegram channel + setup-wizard backend.)_

- [ ] **Phase 31: Retrieval & Memory Pipeline Hardening (Rerank + Full-Docs E2E)** — GPU cross-encoder reranking + two-stage retrieval (vector→rerank-seeds→graph-expand) wired into memory recall + document retrieval + full-document ingest E2E across ALL markitdown-supported formats (pdf/docx/pptx/xlsx/html/csv/md/images/…, not PDF-only) + GraphRAG connected-nodes, over the existing Neo4j stack (no migration). Spike-gated by 068/069/070 (GPU Qwen3-Reranker-0.6B Q4_K_M, rerank-seeds pipeline, RRF fallback, self-learning deferred). (RET-01..05)

> **Cockpit Overhaul (post-Phase-25, in progress — not a formal phase).** After Phase 25 closed, a
> premium-bar overhaul reworked the Phase-23/24/25 surfaces in place: a logo-matched **blue** design
> system (operator-accepted 2026-06-18, fonts + WCAG AA gate), a responsive shell (svh grid, drawers,
> edge-swipe, intent-restore, 380px floor), chat/footer enrichment, and **Authula** embedded auth
> (flag-gated, superseding the Phase-24 passphrase cookie). Specs + adversarial validation + per-spec
> implementation ledgers: `docs/cockpit-overhaul/` (`00-VALIDATION.md` = umbrella). A large
> frontend + auth layer is currently **uncommitted** in the working tree; commit + live cutover pending.

## Phase Details

### Phase 22: Agent Perimeter Hardening

**Goal**: The `internal/agent` operational perimeter is production-ready (blended readiness 6.5 → ≥8.0) so the cockpit's web exposure lands on a hardened base — the spec is locked in `.planning/phases/22-bug-fix/22-SPEC.md`.
**Depends on**: Nothing (first phase of v1.0.0; gates web exposure)
**Requirements**: HARDEN-01, HARDEN-02, HARDEN-03, HARDEN-04, HARDEN-05, HARDEN-06, HARDEN-07, HARDEN-08, HARDEN-09, HARDEN-10, HARDEN-11, HARDEN-12
**Success Criteria** (what must be TRUE):

  1. A deliberately-panicking tool / swarm child / `shell_bg` reaper goroutine dispatched through `executeBatch` / `parallel.Run` / `swarm.runWave` does not crash `aura serve`; the panic surfaces as a model-visible per-call error with no goroutine leak (race+goleak proven)
  2. A flapping or hung MCP server degrades gracefully (single-flight off-lock reconnect, backoff + circuit breaker, `AURA_MCP_CALL_TIMEOUT_SEC=0` bounded by the default deadline) and a failing hook completes the turn under `fail_open`
  3. Credentials do not leak: `IsSecretEnvKey("AURA_DB_URL")` is true, a `shell_exec` child cannot read the composed DSN, hook child env carries no secret-named vars, and the reasoning trace does not write verbatim history by default
  4. Production is observable — each terminal turn outcome increments a labeled Prometheus counter, an LLM-latency histogram is scrapeable, and an injected entropy failure does not panic the daemon
  5. A finding-coverage ledger maps every `AG-###` to fixed+test / accepted+rationale / confirmed+routed; `make coverage` ≥85% owned-surface, full CI + `cache_invariant_audit.sh` green, mutation spot-check ≥70% on the touched critical files

**Plans**:

  - `22-01-PLAN.md` (Wave 1) -- Crash firewall + dedup race hardening (HARDEN-01, HARDEN-02, HARDEN-09, HARDEN-12)
  - `22-02-PLAN.md` (Wave 2) -- Secret boundary + observability minimum (HARDEN-01, HARDEN-04, HARDEN-05, HARDEN-12)
  - `22-03-PLAN.md` (Wave 3) -- MCP resilience + reasoning-router bounds + active budget/wallclock (HARDEN-03, HARDEN-06, HARDEN-09, HARDEN-12)
  - `22-04-PLAN.md` (Wave 4) -- Hooks + provenance + tools/workflow hardening (HARDEN-07, HARDEN-08, HARDEN-09, HARDEN-10, HARDEN-12)
  - `22-05-PLAN.md` (Wave 5) -- Skill reconcile + AG ledger + CI/mutation/live sign-off (HARDEN-01..12)

### Phase 23: Frontend Infrastructure & Industrial Foundation

**Goal**: The industrial frontend foundation exists and is documented BEFORE any cockpit feature code — a research-locked decision record fixes the toolchain/theme/build choices, then the foundation is scaffolded: a Vite 8 + React 19 + TypeScript package whose `//go:embed all:dist` output is baked into the single binary and renders a branded, dark-operator-themed placeholder shell from `aura serve`, behind a blocking zero-warning lint/format/type-check CI gate, with the Node-24 multi-stage Docker build producing the embedded asset (no Node in the runtime image). (Operator directive 2026-06-15: foundation first.)
**Depends on**: Phase 22 (hardened perimeter precedes any web exposure)
**Requirements**: FND-01, FND-02, FND-03, FND-04, FND-05, FND-06
**Research-first**: yes — the industrial-infra research pass (FND-01) runs at plan time before any scaffolding code; its `RESEARCH.md` / decision record (linter ruleset, formatter, design-token architecture, package/repo layout, build + release pipeline, frontend test harness) is the Gate-1 artifact the rest of the phase builds against.
**Success Criteria** (what must be TRUE):

  1. The documented industrial-infra foundation decision record exists and is approved — it locks the linter ruleset, formatter, design-token architecture, `web/` package/repo layout, build + release pipeline, and frontend test harness before any feature code begins (FND-01)
  2. `aura serve` serves a branded placeholder shell embedded in the single binary via `//go:embed all:dist`, with the dark-operator theme + density applied before paint (no flash); the same `dist/` builds reproducibly from `web/` (FND-02, FND-04, FND-05)
  3. `npm run lint` + `npm run format --check` + `tsc --noEmit` pass with zero warnings and run as a blocking CI gate — parity with the Go `golangci-lint` discipline (FND-03)
  4. `public/Logo.png` renders in the app-shell header with a matching favicon + PWA/theme-color metadata, and no marketing hero text appears in the primary viewport (per the ux-spec Copy Contract) (FND-05)
  5. The Node-24 multi-stage Docker build produces the embedded `dist` with no Node in the runtime image, and the Vitest + component/E2E harness runs green in CI (FND-06)

**Plans**: 3 plans

  - `23-01-PLAN.md` (Wave 1) — FND-01 decision-record sign-off + supply-chain gate + zero-warning gate configs + Wave-0 RED test stubs + `internal/webui` embed package (FND-01, FND-02, FND-03)
  - `23-02-PLAN.md` (Wave 2) — design-token dark-operator theme + branded placeholder shell + React-Compiler Vite config + PWA + committed reproducible `web/dist` (FND-02, FND-04, FND-05)
  - `23-03-PLAN.md` (Wave 3) — additive `webui.Handler()` mount into `aura serve` + 4 path-filtered web CI jobs + Node-24 `webbuild` Docker stage + dist-freshness gate (FND-02, FND-03, FND-06)

**UI hint**: yes

### Phase 24: Web Foundation — Serve + Auth + Health

**Goal**: The operator can reach the embedded cockpit SPA served by the single `aura serve` binary — the real SPA host over the Phase-23 embed pipeline, with the SPA catch-all excluding API/`/agent`/health routes, behind a minimal GAP-2 web-auth boundary that activates the dormant `capability_grants` scaffolding, fails fast on an unguarded non-loopback bind, and surfaces a read-only runtime health shell — preserving the single-binary deploy invariant.
**Depends on**: Phase 23 (the embed pipeline + theme + build foundation must exist before the real SPA host)
**Requirements**: WEB-01, WEB-02, WEB-03, WEB-04
**Success Criteria** (what must be TRUE):

  1. Operator opens the cockpit URL served by `aura serve` (SPA embedded via the Phase-23 `//go:embed` pipeline) and the app shell loads from the single binary; an API / `/agent` / health path that does not exist returns a real 404, never the SPA `index.html`
  2. `aura serve` refuses to boot bound to a non-loopback address unless web auth is configured (fail-fast guard), and stays bootable on loopback as before
  3. A mutating route is rejected without authentication when exposed beyond loopback — supported via a reverse-proxy boundary with zero Go change, and via an in-binary signed session cookie (HttpOnly + Secure + SameSite=Strict) bound to an identity row
  4. The app shell renders with theme/density applied before boot (no flash) and shows a read-only runtime health panel aggregating `/healthz` + `/readyz` + status

**Plans**: 4 plans

  - `24-01-PLAN.md` (Wave 1) — WEB-02 fail-fast non-loopback boot guard: `AURA_WEB_AUTH_SECRET`/`AURA_WEB_TRUST_PROXY` knobs + widened `AURA_AGUI_BIND` + `config.GuardWebBind` wired in `bootServe` (WEB-02)
  - `24-02-PLAN.md` (Wave 1) — WEB-01 real SPA host: `internal/webui` SPA-fallback (client-route → index.html, excluded API/`/agent`/health/`/api/` prefix → real 404) + single-source exclusion list, no `/api/` mux collision (WEB-01)
  - `24-03-PLAN.md` (Wave 2) — WEB-03 GAP-2 auth boundary: stdlib `internal/agui/auth.go`+`auth_cookie.go` (constant-time secret, HMAC-signed HttpOnly+Secure+SameSite=Strict cookie, `RequireAuth` whole-origin gate, `capability_grants` on `POST /agent/run`) wired into the mux + `bootServe` (WEB-03)
  - `24-04-PLAN.md` (Wave 3) — WEB-04 runtime health shell + login page + router/404 on the locked Phase-23 tokens + committed `dist` rebuild + live `serve_smoke` proof (WEB-04, WEB-01)

**UI hint**: yes

### Phase 25: Chat + Approval Center

**Goal**: The operator can run the Core-Value agent loop end-to-end from the cockpit — send a prompt, watch the streamed answer, manage conversations, see cost/cache, and resolve HITL interrupts from a cross-thread queue — over the existing AG-UI/SSE transport and `conversations.Store` / `askuser.Store`.
**Depends on**: Phase 24 (serve + auth boundary)
**Requirements**: CHAT-01, CHAT-02, CHAT-03, CHAT-04, CHAT-05, APRV-01, APRV-02, APRV-03
**Success Criteria** (what must be TRUE):

  1. Operator types a prompt and watches the assistant response stream token-by-token over `POST /agent/run` (SSE) in an assistant-ui chat lane
  2. Operator browses, FTS-searches, renames, archives, and deletes conversations through thin HTTP adapters over `conversations.Store`, and can open a reasoning drawer + live tool-activity stream governed by an explicit `showReasoning` policy
  3. Operator sees per-turn cost and cache-hit metrics in a footer
  4. Operator sees a cross-thread list of pending `ask_user` / HITL interrupts (question, options, priority, source) and can accept / decline / cancel one to resume the run over the existing `Interrupt` / `Resume[]` protocol
  5. A stale or auto-terminated approval renders its terminal state with no silent loss
  6. Operator can edit/regenerate a message producing a navigable branch tree (D-09 / CHAT-05 — a deliberate operator-chosen scope addition beyond CHAT-01, recorded PRD-first) over path-aware history, with the `messages[0]` KV-cache invariant preserved (cache-invariant gate green)

**Plans**: 7 plans
Plans:
**Wave 1**

- [x] 25-01-PLAN.md — Conversation REST adapter + reasoning-on flip + /api/conversations mount (CHAT-02/03)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 25-02-PLAN.md — Cross-thread pending read + approvals resolve adapter w/ decline bridge (APRV-01/02/03)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 25-03-PLAN.md — assistant-ui chat lane: SSE reducer, runtime, reasoning drawer, raw tool card (CHAT-01/03)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 25-04-PLAN.md — Conversation sidebar/search/delete-confirm + runtime instrument footer + context gauge (CHAT-02/04)

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 25-05-PLAN.md — Approval badge/list + inline approval card verbs + terminal states (APRV-01/02/03)

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 25-06-PLAN.md — D-09 foundation: CHAT-05 amendment + migration 0017 + path-aware history + cache-invariant audit (CHAT-05)

**Wave 7** *(blocked on Wave 6 completion)*

- [x] 25-07-PLAN.md — D-09 completion: branch list/select + re-run + branch picker UI + Playwright E2E (CHAT-05/01)

**UI hint**: yes

### Phase 26: Typed-Display Protocol + Router

**Goal**: The backend emits a namespaced `aura.display` typed-display protocol (GAP-1) produced by a Go normalizer, and the cockpit renders it through a `switch(payload.type)` display router — turning opaque tool output into inspectable, paginated, source-viewable typed evidence (web/document/code/table/chart), safe system-event cards, a source explorer, and a swarm worker-report table.
**Depends on**: Phase 25 (chat lane is where typed displays render); Phase 22 (default-untrusted provenance / swarm envelope hardening underpins SWARM-01)
**Requirements**: DISP-01, DISP-02, DISP-03, DISP-04, DISP-05, SWARM-01
**Success Criteria** (what must be TRUE):

  1. The backend emits a namespaced `aura.display` CUSTOM event carrying a typed `DisplayPayload` from a Go normalizer (`internal/agent/display/`), via an additive `Actions.Display` slot, with the `messages[0]` KV-cache invariant preserved (CI gate green)
  2. The cockpit renders typed displays via a `switch(payload.type)` router for `web_result`, `document`, `code`, `local_artifact`, `table`, and `chart`, and the operator can inspect a display's raw-data / source view, paginate result groups, and see citation bubbles on completed answers
  3. Web-safety backend error classes render as typed `system_event` cards showing only safe reasons (no SSRF internals), driven by the `internal/web/errors.go` enum
  4. The operator can use a Source Explorer with Table / Metadata / Configuration views
  5. A `swarm_spawn` child report renders as a typed `swarm_report` table over `ChildReport` with no inter-agent chat / mailbox theater

**Plans**: 6 plans (Waves 1–4)

  - `26-01-PLAN.md` (Wave 1) — Backend protocol core: `internal/agent/display/` normalizer + DisplayPayload union + Actions.Display slot + aura.display CUSTOM branch + source registry (DISP-01, DISP-04, DISP-05, SWARM-01)
  - `26-02-PLAN.md` (Wave 1) — Frontend D-06 prerequisite: history-rehydration fetch + snapshotToMessages + sseAdapter CUSTOM frame + DisplayRouter shell (default→raw card) + DisplayPagination (DISP-01, DISP-02)
  - `26-03-PLAN.md` (Wave 2) — Backend source-list tail-inject (Budget.Sources, cache-invariant) + SSRF-safe image-proxy (FetchImage + /api/image-proxy) + D-06 re-derive at projectMessages (DISP-01, DISP-03, DISP-05)
  - `26-04-PLAN.md` (Wave 2) — Frontend data/status displays: table, chart (zero-dep SVG), system_event (web-safety safe reasons), swarm_report, local_artifact (DISP-02, DISP-04, SWARM-01)
  - `26-05-PLAN.md` (Wave 3) — Frontend evidence displays + citation pipeline: web_result (image-proxy thumbnails), document, code (lazy Shiki escaped spans), rehypeCitations inline splice + CitationBubble (Radix hovercard) + 2 pinned MIT deps (DISP-02, DISP-03)
  - `26-06-PLAN.md` (Wave 4) — Frontend read-only Source Explorer (Table/Metadata/Configuration) + "Sources (N)" + citation click-through + Playwright replay e2e + Stryker scope + dist rebuild (DISP-03, DISP-05)

**UI hint**: yes

### Phase 27: Neo4j Graph Explorer

**Goal**: The operator can open a dedicated read-only Neo4j evidence-graph workspace — a Go graph-normalizer turns MCP Cypher rows into the `{nodes, edges, paths, schema, query}` contract, served over REST, and rendered as an interactive WebGL canvas with a node inspector and path strip — answering evidence/path questions, never a decorative hairball.
**Depends on**: Phase 26 (graph_chunk inline displays + display router); Phase 24 (REST/auth boundary)
**Requirements**: GRAPH-01, GRAPH-02, GRAPH-03, GRAPH-04
**Success Criteria** (what must be TRUE):

  1. A Go graph-normalizer (`internal/knowledge/graphview.go`) converts Neo4j MCP results to the `{nodes, edges, paths, schema, query}` contract and serves it over REST (`GET /api/graph/schema`, `POST /api/graph/query`), not the chat SSE stream
  2. Operator opens the Graph Explorer (WebGL canvas) and sees evidence paths with label-family color encoding and a readable path strip below the canvas
  3. Operator selects a node and inspects its label / properties / degree / neighbors / citations; on mobile and keyboard, tap/focus opens the inspector (hover is never the only access path)
  4. Graph queries are read-only by default (read-only Cypher guard rejecting `CREATE/MERGE/SET/DELETE/DROP`) with a Cypher preview, and dense graphs default to filtered evidence paths rather than hairballs

**Plans**: 4 plans (Waves 1–3)

  - `27-01-PLAN.md` (Wave 1) — Backend normalizer + contract: `internal/knowledge/graphview.go` (GraphReader read-only seam + flat `{nodes,edges,paths,schema,query}` structs) + structured-intent→parameterized read-Cypher compiler + `assertReadOnly` write-verb guard + row→contract normalizer (labels via `apoc.convert.toJson`) + cap-clamp + the Wave-0 live mcp-serialization/footprint probe (GRAPH-01, GRAPH-04)
  - `27-02-PLAN.md` (Wave 2) — REST + wiring: `internal/agui/graph_api.go` thin handlers for `GET /api/graph/schema` + `POST /api/graph/query` (body-cap + intent validation + `sanitizeErr`) + `SetGraphView` setter + boot-time `knowledge.Client` in serve + the `/api/graph/*` siblings mounted RequireAuth-only under the carve-out (GRAPH-01, GRAPH-04)
  - `27-03-PLAN.md` (Wave 2) — Frontend core + install: legitimacy-gated install of sigma/graphology/@react-sigma/forceatlas2 (MIT, lazy) + pure `graphIntent.ts` (intent reducer + filter predicates + schema-driven label-family color mapper) + `graphApi.ts` typed fetch + `types.ts` + Vitest (no WebGL) (GRAPH-02, GRAPH-04)
  - `27-04-PLAN.md` (Wave 3) — Frontend workspace: Frame-06 three-pane `GraphExplorer` + `SigmaCanvas` (WebGL, ForceAtlas2 position-cache, resize-remount + ErrorBoundary) + `SeedFilterPanel` (read-only Cypher preview) + `NodeInspector` (pin-path/open-source/show-Cypher, no add-note) + `PathStrip` + a11y parallel DOM + lazy AppShell swap + en/it i18n + contrast-ramp + Stryker scope + Playwright graph/graph-a11y e2e + dist rebuild (GRAPH-02, GRAPH-03, GRAPH-04)

**UI hint**: yes

### Phase 28: Governance Boards + Web Onboarding

**Goal**: The operator can view the substrate's governance state read-only — MCP servers, the skills library, and the scheduler board — and a new operator can complete a web onboarding/setup wizard that links identity and seeds the `Agent.md` profile through the existing onboarding LoopAgent. (Governance writes and `ui_control` are deferred to a follow-up milestone.)
**Depends on**: Phase 24 (serve + auth + REST boundary); Phase 25 (approval/HITL surface that the onboarding flow resumes through)
**Requirements**: GOV-01, GOV-02, GOV-03, ONBD-01, ONBD-02
**Success Criteria** (what must be TRUE):

  1. Operator views MCP servers read-only — source, status, env health, doctor result, mounted tool count, allowlist/risk policy (raw secrets never shown)
  2. Operator views the skills library read-only across active / pending / archived / audit tabs, and pending skills are not runnable or prompt-injectable from the board
  3. Operator views the scheduler board read-only — tasks, schedule, next run, status, run history, heartbeat
  4. A new operator completes a web onboarding / setup wizard (beyond the `:9081` loopback setup) that links identity and seeds the `Agent.md` profile, driving the existing onboarding LoopAgent with confirm/edit/skip and without duplicate LLM turns

**Plans**: 5/6 plans executed

Plans:
**Wave 1**

- [x] 28-01-PLAN.md — Wave-0 backend gaps + DI seams (run-history query, capability/audit stores, migration 0021, MCP probe, skills stage reader, agui seams)
- [x] 28-04-PLAN.md — BLOCKING PRD-amendment: relax single-operator (D-07) + absorb Phase 30 (D-09) + OperatorUserID relaxation

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 28-02-PLAN.md — Governance boards backend: 6 read-only /api/governance/* handlers + mounts

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 28-03-PLAN.md — Governance boards frontend: lazy governance workspace + MCP/Skills/Scheduler boards (live probe, lifecycle tabs, run history)
- [x] 28-05-PLAN.md — Onboarding backend: TTL session store + REST step + cross-store provisioning saga + QR + Telegram-status

**Wave 4** *(blocked on Wave 3 completion)*

- [ ] 28-06-PLAN.md — Onboarding frontend: full-screen wizard (credentials → capability picker → Telegram link+QR → interview → review+Create)

**UI hint**: yes

### Phase 29: Governance Write — MCP Configuration + Skills Install

**Goal**: The operator can configure MCP servers (recipe / custom-stdio install, env editing with redaction, enable/disable/remove) and govern the skills lifecycle (install from a source field or catalog → risk-tiered approval queue → activate, restore/archive, immutable audit) from the cockpit — the web WRITE surface over the EXISTING backend (the MCP manager control plane from Phase 16 + the scoring-gated skill install/create/delete + `ask_user` approval + append-only audit from Phase 11), not new core capability. Per the ux-spec Frame 08 Non-Goals, this surface never shows raw saved MCP secrets, never silently mounts destructive MCP tools when an allowlist exists, never lets a model tool call self-activate a skill, never lets pending skills run or inject prompt content, and never presents `--ignore-scripts` install as safe — install remains RISKY supply-chain input.
**Depends on**: Phase 24 (GAP-2 web auth — write surfaces require it), Phase 25 (the approval center — the skill/MCP approval queue reuses the HITL `Interrupt`/`Resume[]` resume protocol), Phase 28 (the governance READ boards — these write surfaces extend them). LAST phase: the highest-risk write surfaces land only after auth + the approval center + the read-only boards are proven.
**Requirements**: MCPW-01, MCPW-02, MCPW-03, SKW-01, SKW-02, SKW-03
**Success Criteria** (what must be TRUE):

  1. Operator installs an MCP server from a recipe (or adds a custom stdio server) from the cockpit, sees the equivalent CLI command + the managed-config destination (`~/.aura/mcp/servers.json` / the `AURA_MCP_SERVERS_JSON` override source) previewed before save, and after trust approval + mount-time risk policy the server appears mounted with its tool count (MCPW-01, MCPW-03)
  2. An MCP env value displays as a redacted chip after save and is never returned raw; required / optional / missing / placeholder states are visually distinct and a still-placeholder required recipe var raises a warning (MCPW-02)
  3. Operator disables a server (reversible) and removes a server (confirmation + an audit row); a destructive/denied MCP tool is shown explicitly and is never silently mounted when an allowlist exists, and fail-soft mount warnings surface (MCPW-02, MCPW-03)
  4. Operator installs a skill from a source field or a catalog item; the install pipeline surfaces source, content hash/preview, risk tier, the validation checklist (`--ignore-scripts`, sanitized env, `SKILL.md` parse, body cap, injection-literal blocklist, sanitized name/path) and destination — and a RISKY/DESTRUCTIVE action enters the approval queue with a resume token, is NOT runnable or prompt-injectable while pending, and activates only on the approval resume (no model-facing approve) (SKW-01, SKW-02)
  5. Operator restores / archives skills across separate active / pending / archived / audit tabs (each row showing capability scope, last used, use count, TTL/archive state), and the skills audit ledger shows the install as an append-only row (SKW-02, SKW-03)

**Plans**: TBD
**UI hint**: yes

### Phase 30: Telegram Onboarding on Frontend with Link and QR Code

**Goal:** ✅ **Absorbed into Phase 28** (D-09, 2026-06-20) — the Telegram link/QR surface is delivered as requirement **ONBD-01b** inside Phase 28's onboarding wizard (deep-link + server-rendered scannable QR, reusing the existing setup-wizard mint/`ConsumeOnboarding` token flow). This phase is a tombstone; do NOT plan/execute it separately. Original requirement traceability is preserved in `30-SPEC.md` → see `28-SPEC §ONBD-01b`.
**Requirements**: ONBD-01b (delivered in Phase 28)
**Depends on:** Phase 29
**Plans:** absorbed into Phase 28 (no separate plans)

Plans:

- [x] Absorbed into Phase 28 (ONBD-01b) — see `28-SPEC §ONBD-01b`

### Phase 31: Retrieval & Memory Pipeline Hardening (Rerank + Full-Docs E2E)

**Goal**: Aura's retrieval is precision-hardened end-to-end — a GPU cross-encoder reranker (Qwen3-Reranker-0.6B Q4_K_M) behind a fail-soft Go client and a two-stage pipeline (vector seed → rerank seeds → graph-expand winners), wired into BOTH memory recall and document retrieval over the existing Neo4j stack (no DB migration), with the full-document ingest path (ANY markitdown-supported format — pdf/docx/pptx/xlsx/html/csv/md/txt/images/… via the existing `markitdown` sidecar, not PDF-only → format-aware chunk with page/sheet/slide/section locator → Granite 384d embed → connected `:Chunk`/`:NEXT_CHUNK` graph) hardened and proven E2E, gated by an eval harness (nDCG@10 / Recall@5 / MRR), a non-monotonic rerank guard, a per-stage p95 budget, and graceful RRF fallback when GPU is absent.
**Depends on**: Phase 15 (Memory Subsystem), Phase 22 (perimeter hardening), Phase 27 (graphview read seam). Independent of Phases 28/29.
**Research-first**: no — spikes 068/069/070 ARE the research. Plan with `--skip-research`. Locked decisions: GPU Qwen3-Reranker-0.6B Q4_K_M (`server-cuda`, `--reranking --pooling rank -ngl 99`, Apache-2.0, <1GB VRAM, 333ms/15-docs; CPU=23s rejected; jina-v3 rejected broken+NC; Q3_K_M rejected slower); rerank-the-seeds-then-expand (267ms fast-path, not expand-then-rerank 1.4s); RRF fallback; Neo4j stays; Leiden = external `leidenalg`/`graspologic` sidecar; self-learning OUT (deferred).
**Requirements**: RET-01, RET-02, RET-03, RET-04, RET-05
**Success Criteria** (what must be TRUE):

  1. A GPU reranker sidecar (`server-cuda` Qwen3-Reranker-0.6B Q4_K_M, `--reranking --pooling rank -ngl 99`) is reachable behind an `internal/rerank` client mirroring `documents.EmbeddingClient`; with the sidecar / GPU absent, retrieval degrades to RRF order and never hard-fails (fail-soft proven) (RET-01)
  2. Two-stage retrieval (vector/BM25 seed → rerank the ~10 seeds → graph-expand winners for context) is wired into memory recall AND document retrieval; the `messages[0]` KV-cache invariant is preserved (cache-invariant gate green); end-to-end retrieval p95 < 500ms on a representative corpus (RET-02)
  3. The full-document ingest pipeline accepts ALL markitdown-supported formats (pdf/docx/pptx/xlsx/xlsm/html/htm/csv/md/markdown/txt/json/xml/epub/images via the existing `markitdown` sidecar — the artificial `isSupportedDocument` 4-format cap is removed and the sidecar `/extract` emits format-aware chunks with a page/sheet/slide/section locator, generic-markdown fallback for the long tail) → Granite 384d embed → Neo4j `:Chunk` + `:NEXT_CHUNK` connected graph; hardened (bounded/streamed, provenance-scoped chunks) and proven E2E on the real G220-class PDF manual PLUS at least one non-PDF format (e.g. PPTX or HTML) (RET-03)
  4. GraphRAG connected-nodes retrieval (vector seed → 1-hop graph expansion → rerank) returns evidence within the documented per-stage p95 budget (vector + graph ~tens of ms; rerank the dominant bounded cost) (RET-04)
  5. An eval harness (nDCG@10 / Recall@5 / MRR, vector vs vector+rerank) shows a measured precision lift with zero queries regressed beyond noise (non-monotonic guard); `make coverage` ≥85% owned-surface; a live retrieval/`rerank_integration` E2E tier runs green in CI; self-learning is explicitly OUT (deferred) (RET-05)

**Plans**: 5 plans
- [ ] 31-01-PLAN.md — Rerank GPU sidecar + internal/rerank fail-soft client + AURA_RERANK_BASE_URL (RET-01) [wave 1]
- [ ] 31-02-PLAN.md — Full-docs ingest: single allowlist (all markitdown formats) + PPTX/HTML/CSV handlers + :NEXT_CHUNK edges (RET-03) [wave 2]
- [ ] 31-03-PLAN.md — Two-stage retrieval (seed → rerank seeds → expand winners) wired into document retrieval + memory recall, RRF fallback, messages[0] preserved (RET-02) [wave 3]
- [ ] 31-04-PLAN.md — GraphRAG connected-nodes retrieval over :NEXT_CHUNK with per-stage p95 budget (RET-04) [wave 4]
- [ ] 31-05-PLAN.md — Eval harness (nDCG@10/Recall@5/MRR) + non-monotonic guard + CI live tiers + coverage ≥85% + self-learning OUT (RET-05) [wave 5]
**UI hint**: no

## Progress

| Phase | Milestone | Plans | Status | Completed |
| ----- | --------- | ----- | ------ | --------- |
| 0–21 (substrate) | v0.0.0 | 144/144 | ✅ Shipped | 2026-06-15 |
| 22. Agent Perimeter Hardening | v1.0.0 | 5/5 | Complete   | 2026-06-15 |
| 23. Frontend Infrastructure & Industrial Foundation | v1.0.0 | 3/3 | Complete    | 2026-06-16 |
| 24. Web Foundation — Serve + Auth + Health | v1.0.0 | 4/4 | Complete    | 2026-06-16 |
| 25. Chat + Approval Center | v1.0.0 | 7/7 | Complete    | 2026-06-17 |
| 26. Typed-Display Protocol + Router | v1.0.0 | 6/6 | Complete    | 2026-06-19 |
| 27. Neo4j Graph Explorer | v1.0.0 | 4/4 | Complete   | 2026-06-19 |
| 28. Governance Boards + Web Onboarding | v1.0.0 | 5/6 | In Progress|  |
| 29. Governance Write — MCP Configuration + Skills Install | v1.0.0 | 0/? | Not started | - |
| 30. Telegram Onboarding on Frontend (Link + QR) | v1.0.0 | — | Absorbed into 28 | 2026-06-20 |
| 31. Retrieval & Memory Pipeline Hardening (Rerank + Full-Docs E2E) | v1.0.0 | 0/? | Planning | - |
