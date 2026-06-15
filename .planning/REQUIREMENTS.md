# Requirements: Aura — v1.0.0 Aura Deep Search Web Cockpit

**Defined:** 2026-06-15
**Core Value (milestone):** Operator-visible, governable control over the Aura agent substrate through an embedded web cockpit — chat, evidence, and governance rendered as typed surfaces over the existing AG-UI/SSE gateway, on a hardened agent perimeter, preserving the single-binary deploy.

**Design truth-source:** `docs/design/aura-deep-search-figma/ux-spec.md` (8 frames, component inventory, display-type mapping, Non-Goals).
**Research:** `.planning/research/` (STACK, FEATURES, ARCHITECTURE, PITFALLS, SUMMARY — HIGH confidence).

## v1 Requirements

### Hardening (HARDEN) — Phase 22, gates web exposure

Remediation of the 2026-06-15 `internal/agent` production-readiness audit (`docs/audit/`); detail locked in `.planning/phases/22-bug-fix/22-SPEC.md`. Moves blended production-readiness 6.5 → ≥8.0 (multi-tenant-only security slices explicitly deferred there).

- [ ] **HARDEN-01**: A panicking tool / swarm child / `shell_bg` reaper goroutine cannot crash `aura serve`; the panic surfaces as a model-visible per-call error (AG-001, P0)
- [ ] **HARDEN-02**: The dedup ring is concurrency-safe by construction (mutex), race-clean under parallel dispatch (AG-002)
- [ ] **HARDEN-03**: A flapping/hung MCP server degrades gracefully — single-flight reconnect outside the lock, backoff + circuit breaker, sane `=0`/`-1` timeout semantics (AG-005/006)
- [ ] **HARDEN-04**: Credentials (DSN-shaped env, secrets) do not leak to shell children, hook subprocesses, or the reasoning trace by default (AG-010/009/003)
- [ ] **HARDEN-05**: Production is observable — turn-outcome / LLM-latency / error / token / hook metrics + agent `slog`; telemetry cannot crash the daemon (AG-012/013/033)
- [ ] **HARDEN-06**: An embed-sidecar outage adds no per-turn latency cliff (reasoning-router fallback policy, AG-008)
- [ ] **HARDEN-07**: A hook fault is contained, not turn-fatal (fail-soft hook policy, AG-004)
- [ ] **HARDEN-08**: Unknown-tool and swarm-child output is default-untrusted and cannot launder prompt injection (AG-052)
- [ ] **HARDEN-09**: Loop / budget / workflow correctness — bounded and validated (AG-035..043)
- [ ] **HARDEN-10**: Tool execution is memory-safe, evictable, and consistent — fs size cap, cycle guard, dedup growth bound, etc. (AG-014..046)
- [ ] **HARDEN-11**: Skill self-extension docs match behavior; dead code removed (AG-011/044/051)
- [ ] **HARDEN-12**: Every in-scope finding closed to Gate-3 with its named regression test; ≥85% owned-surface coverage holds; nothing silently dropped

### Web Foundation (WEB) — Slice A (gates everything)

- [ ] **WEB-01**: Operator can open the cockpit served by `aura serve` from the single binary (SPA embedded via `//go:embed`), with API / `/agent` / health routes excluded from the SPA catch-all so API 404s stay real errors
- [ ] **WEB-02**: `aura serve` refuses to bind a non-loopback address unless web auth is configured (fail-fast boot guard)
- [ ] **WEB-03**: Mutating routes require authentication when exposed beyond loopback — reverse-proxy boundary supported with zero Go change, plus an in-binary signed session cookie (HttpOnly + Secure + SameSite=Strict) bound to an identity row (activates dormant `capability_grants`)
- [ ] **WEB-04**: App shell renders with theme/density applied before boot (no flash) and a read-only runtime health/readyz panel aggregating `/healthz` + `/readyz` + status

### Chat (CHAT) — Slice B

- [ ] **CHAT-01**: Operator can send a prompt and watch the streamed assistant response over `POST /agent/run` (SSE) in an assistant-ui chat lane
- [ ] **CHAT-02**: Operator can browse, FTS-search, rename, archive, and delete conversations (thin HTTP adapters over `conversations.Store`)
- [ ] **CHAT-03**: Operator can view a reasoning drawer (chain-of-thought) and a live tool-activity stream, governed by an explicit `showReasoning` policy
- [ ] **CHAT-04**: Operator sees per-turn cost and cache-hit metrics in a footer

### Approval Center (APRV) — Slice C

- [ ] **APRV-01**: Operator can see a cross-thread list of pending `ask_user` / HITL interrupts with question, options, priority, and source
- [ ] **APRV-02**: Operator can accept / decline / cancel a pending interrupt and resume the run over the existing `Interrupt`/`Resume[]` protocol
- [ ] **APRV-03**: Stale / auto-terminated approvals render their terminal state (no silent loss)

### Typed Display (DISP) — Slices D + E (GAP-1)

- [ ] **DISP-01**: Backend emits a namespaced `aura.display` CUSTOM event carrying a typed `DisplayPayload`, produced by a Go normalizer (`internal/agent/display/`) from structured tool results — additive `Actions.Display` slot, `messages[0]` cache invariant preserved
- [ ] **DISP-02**: The cockpit renders typed displays via a `switch(payload.type)` router: `web_result`, `document`, `code`, `local_artifact`, `table`, `chart`
- [ ] **DISP-03**: Operator can inspect a display's raw-data / source view, paginate result groups, and see citation bubbles on completed answers
- [ ] **DISP-04**: Web-safety backend error classes render as typed `system_event` cards showing only safe reasons (no SSRF internals) — `internal/web/errors.go` enum
- [ ] **DISP-05**: Operator can use a Source Explorer with Table / Metadata / Configuration views

### Neo4j Graph Explorer (GRAPH) — Slice F

- [ ] **GRAPH-01**: A Go graph-normalizer converts Neo4j MCP results to the `{nodes, edges, paths, schema, query}` contract (REST, not SSE)
- [ ] **GRAPH-02**: Operator can open a Neo4j Graph Explorer (WebGL canvas) showing evidence paths with label-family color encoding + a readable path strip
- [ ] **GRAPH-03**: Operator can select a node and inspect label/properties/degree/neighbors/citations; hover is never the only access path (tap/focus opens the inspector on mobile + keyboard)
- [ ] **GRAPH-04**: Graph queries are read-only by default (read-only Cypher guard) with a Cypher preview; dense graphs default to filtered evidence paths, not hairballs

### Swarm Report (SWARM) — Slice G

- [ ] **SWARM-01**: A `swarm_spawn` child report renders as a typed `swarm_report` table over `ChildReport` (no inter-agent chat / mailbox theater)

### Governance — read-only (GOV) — P2

- [ ] **GOV-01**: Operator can view MCP servers read-only — source, status, env health, doctor result, mounted tool count, allowlist/risk policy
- [ ] **GOV-02**: Operator can view the skills library read-only across active / pending / archived / audit tabs (pending skills not runnable or prompt-injectable)
- [ ] **GOV-03**: Operator can view the scheduler board read-only — tasks, schedule, next run, status, run history, heartbeat

### Web Onboarding (ONBD) — Slice J (pulled into v1.0.0)

- [ ] **ONBD-01**: A new operator can complete a web onboarding / setup wizard (beyond the `:9081` loopback setup) that links identity and seeds the `Agent.md` profile
- [ ] **ONBD-02**: The wizard drives the existing onboarding LoopAgent / `Agent.md` flow with confirm/edit/skip and without duplicate LLM turns

## v2 Requirements

Deferred to a follow-up milestone (acknowledged, not in this roadmap).

### Governance write surfaces (GOVW) — Slice H

- **GOVW-01**: MCP install / remove via HTTP with confirmation + audit
- **GOVW-02**: Skill install / approve / delete via HTTP through the approval queue
- **GOVW-03**: Scheduler task create / cancel / approve via HTTP

### Operator-OS shell (SHELL) — Slice I

- **SHELL-01**: Allowlisted, audited, replayable `ui_control` event family (`open_panel`, `highlight_source`, `set_mode`, `focus_query`, `show_job`, `set_density`, `theme_preview`) + backend validator + Zustand reducer
- **SHELL-02**: Adaptive icon rail, dockable/minimizable tool windows + dock chips, command palette, slash actions, background-job feedback

### Richer observability (OBS)

- **OBS-01**: Built-in observability dashboard render panel (metrics/`/debug/vars` exist; dashboard is v2 polish)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Governance write surfaces (MCP/skill/scheduler mutations via HTTP) | Higher-risk; needs GAP-2 hardened + a settled auth model — v2 (GOVW) |
| `ui_control` + operator-OS shell | Highest abuse surface; only valuable once typed displays + multiple tool windows exist — v2 (SHELL) |
| Multimodal user input on `/agent/run` | Endpoint rejects it today (`server.go:33`); Telegram remains the multimodal channel |
| Multi-tenant auth / RBAC / OAuth | Minimal single-operator session cookie only this milestone; real multi-tenancy is a future milestone |
| Public marketplace skill discovery | Local-first by definition (substrate constraint) |
| Next.js / SSR | Dead weight in a Go single-binary product (STACK.md rejected) |

## Open Decisions (resolve in discuss-phase)

- **NVL license**: `@neo4j-nvl/*` is a CUSTOM Neo4j license (not MIT) — must be reviewed before the commercial DGX-Spark bundle. MIT fallback = Cytoscape.js 3.34.0 (more work, fully permissive). Decide before Slice F (GRAPH).
- **`showReasoning` web policy**: CoT is a deliberate Telegram opt-in today (redacted by default at `server.go:214`); the web cockpit needs an explicit policy (CHAT-03).

## Traceability

Populated by the roadmapper during ROADMAP.md creation. Each REQ-ID maps to exactly one phase (continuing numbering from Phase 22).

| Requirement | Phase | Status |
|-------------|-------|--------|
| (filled by roadmapper) | — | Pending |

**Coverage:**
- v1 requirements: 33 total (12 HARDEN + 4 WEB + 4 CHAT + 3 APRV + 5 DISP + 4 GRAPH + 1 SWARM + 3 GOV + 2 ONBD)
- Mapped to phases: 0 (pending roadmapper)
- Unmapped: 33 ⚠️

---
*Requirements defined: 2026-06-15*
*Last updated: 2026-06-15 after milestone v1.0.0 definition*
