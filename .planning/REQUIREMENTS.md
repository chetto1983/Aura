# Requirements: Aura — v1.0.0 Aura Deep Search Web Cockpit

**Defined:** 2026-06-15
**Core Value (milestone):** Operator-visible, governable control over the Aura agent substrate through an embedded web cockpit — chat, evidence, and governance rendered as typed surfaces over the existing AG-UI/SSE gateway, on a hardened agent perimeter, preserving the single-binary deploy.

**Design truth-source:** `docs/design/aura-deep-search-figma/ux-spec.md` (8 frames, component inventory, display-type mapping, Non-Goals).
**Research:** `.planning/research/` (STACK, FEATURES, ARCHITECTURE, PITFALLS, SUMMARY — HIGH confidence).

## v1 Requirements

### Hardening (HARDEN) — Phase 22, gates web exposure

Remediation of the 2026-06-15 `internal/agent` production-readiness audit (`docs/audit/`); detail locked in `.planning/phases/22-bug-fix/22-SPEC.md`. Moves blended production-readiness 6.5 → ≥8.0 (multi-tenant-only security slices explicitly deferred there).

- [x] **HARDEN-01**: A panicking tool / swarm child / `shell_bg` reaper goroutine cannot crash `aura serve`; the panic surfaces as a model-visible per-call error (AG-001, P0)
- [x] **HARDEN-02**: The dedup ring is concurrency-safe by construction (mutex), race-clean under parallel dispatch (AG-002)
- [x] **HARDEN-03**: A flapping/hung MCP server degrades gracefully — single-flight reconnect outside the lock, backoff + circuit breaker, sane `=0`/`-1` timeout semantics (AG-005/006)
- [x] **HARDEN-04**: Credentials (DSN-shaped env, secrets) do not leak to shell children, hook subprocesses, or the reasoning trace by default (AG-010/009/003)
- [x] **HARDEN-05**: Production is observable — turn-outcome / LLM-latency / error / token / hook metrics + agent `slog`; telemetry cannot crash the daemon (AG-012/013/033)
- [x] **HARDEN-06**: An embed-sidecar outage adds no per-turn latency cliff (reasoning-router fallback policy, AG-008)
- [x] **HARDEN-07**: A hook fault is contained, not turn-fatal (fail-soft hook policy, AG-004)
- [x] **HARDEN-08**: Unknown-tool and swarm-child output is default-untrusted and cannot launder prompt injection (AG-052)
- [x] **HARDEN-09**: Loop / budget / workflow correctness — bounded and validated (AG-035..043)
- [x] **HARDEN-10**: Tool execution is memory-safe, evictable, and consistent — fs size cap, cycle guard, dedup growth bound, etc. (AG-014..046)
- [x] **HARDEN-11**: Skill self-extension docs match behavior; dead code removed (AG-011/044/051)
- [x] **HARDEN-12**: Every in-scope finding closed to Gate-3 with its named regression test; ≥85% owned-surface coverage holds; nothing silently dropped

### Frontend Foundation (FND) — Phase 23, research-first, before any feature code

Industrial frontend foundation established and documented BEFORE cockpit feature coding (operator directive 2026-06-15). Research-first: a deep industrial-infra pass locks the toolchain/theme/build choices, then the foundation is scaffolded.

- [x] **FND-01**: Deep industrial-infra research pass produces a documented, locked foundation decision record (linter ruleset, formatter, design-token architecture, package/repo layout, build + release pipeline, frontend test harness) — `RESEARCH.md` / `docs/` — before feature code begins
- [x] **FND-02**: Vite 8 + React 19 + TypeScript package scaffold with a `//go:embed all:dist` pipeline producing a binary-embeddable `dist/` consumed by `aura serve` (a branded placeholder shell renders from the single binary)
- [x] **FND-03**: Frontend linter + formatter + type-check (ESLint/Biome + Prettier + `tsc --noEmit`) wired into CI as a blocking, zero-warning gate — parity with the Go `golangci-lint` discipline
- [x] **FND-04**: Design-token theme system — `tokens.json` → Tailwind 4 `@theme` mapping the dark operator-cockpit palette (elysia-informed board) + density modes, applied before app boot (no flash)
- [x] **FND-05**: Brand integration — `public/Logo.png` in the app-shell header + favicon + PWA/theme-color metadata, per the ux-spec Copy Contract (no marketing hero text)
- [x] **FND-06**: Frontend test harness (Vitest + component/E2E runner) + the Node 24 multi-stage Docker build stage producing the embedded asset with no Node in the runtime image

### Web Foundation (WEB) — Slice A (gates everything)

- [x] **WEB-01**: Operator can open the cockpit served by `aura serve` from the single binary (SPA embedded via `//go:embed`), with API / `/agent` / health routes excluded from the SPA catch-all so API 404s stay real errors
- [x] **WEB-02**: `aura serve` refuses to bind a non-loopback address unless web auth is configured (fail-fast boot guard)
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

### MCP Configuration — write/governance (MCPW) — Phase 29 (ux-spec Frame 08)

Operator-directed addition (2026-06-15). Backend write capability already exists (MCP manager, Phase 16); this adds the web config surface over it, gated by GAP-2 auth (Phase 24).

- [ ] **MCPW-01**: Operator can install an MCP server from a recipe or add a custom stdio server via the cockpit, with the equivalent CLI command + managed-config destination (`~/.aura/mcp/servers.json`, or the `AURA_MCP_SERVERS_JSON` override source) shown before save
- [ ] **MCPW-02**: Operator can edit MCP env values (redacted chips after save, never raw; required/optional/missing/placeholder states distinct), enable/disable a server (reversible), and remove a server (confirmation + audit row)
- [ ] **MCPW-03**: MCP config mutations pass trust approval + mount-time risk policy before tools enter the registry; denied/destructive tools are explicit, never silently mounted; fail-soft mount warnings are surfaced

### Skills Install & Lifecycle — write/governance (SKW) — Phase 29 (ux-spec Frame 08)

Operator-directed addition (2026-06-15). Backend write capability already exists (scoring-gated skill install/create/delete + `ask_user` approval + append-only audit, Phase 11); this adds the web install/approval surface, gated by GAP-2 auth + the approval center (Phase 25).

- [ ] **SKW-01**: Operator can install a skill from a source field or a catalog item via the cockpit; the install pipeline surfaces source, content hash/preview, risk tier, and the validation checklist (`--ignore-scripts`, sanitized env, `SKILL.md` parse, body cap, injection-literal blocklist, sanitized name/path) + destination before activation
- [ ] **SKW-02**: RISKY/DESTRUCTIVE skill actions (install/create/update/delete) enter an approval queue with source, content preview, risk tier, and resume token; pending skills cannot run or be prompt-injected; activation is the approval resume (no model-facing approve)
- [ ] **SKW-03**: Operator can restore / archive skills and view the immutable audit ledger; active/pending/archived/audit tabs are separate; actions show capability scope, last used, use count, and TTL/archive state

## v2 Requirements

Deferred to a follow-up milestone (acknowledged, not in this roadmap).

### Governance write — scheduler (GOVW)

- **GOVW-03**: Scheduler task create / cancel / approve / run-now via HTTP — *MCP config (MCPW) and skills install (SKW) were pulled into v1.0.0 per operator directive; only scheduler-write remains deferred.*

### Operator-OS shell (SHELL) — Slice I

- **SHELL-01**: Allowlisted, audited, replayable `ui_control` event family (`open_panel`, `highlight_source`, `set_mode`, `focus_query`, `show_job`, `set_density`, `theme_preview`) + backend validator + Zustand reducer
- **SHELL-02**: Adaptive icon rail, dockable/minimizable tool windows + dock chips, command palette, slash actions, background-job feedback

### Richer observability (OBS)

- **OBS-01**: Built-in observability dashboard render panel (metrics/`/debug/vars` exist; dashboard is v2 polish)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Scheduler write surfaces via HTTP (create/cancel/approve) | Lower urgency; deferred to v2 (GOVW-03). MCP config (MCPW) + skills install (SKW) are now in v1.0.0 (Phase 29). |
| `ui_control` + operator-OS shell | Highest abuse surface; only valuable once typed displays + multiple tool windows exist — v2 (SHELL) |
| Multimodal user input on `/agent/run` | Endpoint rejects it today (`server.go:33`); Telegram remains the multimodal channel |
| Multi-tenant auth / RBAC / OAuth | Minimal single-operator session cookie only this milestone; real multi-tenancy is a future milestone |
| Public marketplace skill discovery | Local-first by definition (substrate constraint) |
| Next.js / SSR | Dead weight in a Go single-binary product (STACK.md rejected) |

## Open Decisions (resolve in discuss-phase)

- **NVL license**: `@neo4j-nvl/*` is a CUSTOM Neo4j license (not MIT) — must be reviewed before the commercial DGX-Spark bundle. MIT fallback = Cytoscape.js 3.34.0 (more work, fully permissive). Decide before Phase 27 (GRAPH / Slice F).
- **`showReasoning` web policy**: CoT is a deliberate Telegram opt-in today (redacted by default at `server.go:214`); the web cockpit needs an explicit policy (CHAT-03, Phase 25).
- **Frontend linter/formatter choice** (FND-03): ESLint+Prettier vs Biome — resolve in the Phase 23 industrial-infra research pass (FND-01) before scaffolding.

## Traceability

Each REQ-ID maps to exactly one phase (numbering continues from Phase 21; v1.0.0 = Phases 22–29). Phase 23 (Frontend Infrastructure & Industrial Foundation) was inserted per the operator directive 2026-06-15 to establish the industrial frontend foundation before any feature code; the former Phases 23–27 shifted to 24–28.

| Requirement | Phase | Status |
|-------------|-------|--------|
| HARDEN-01 | Phase 22 | Complete |
| HARDEN-02 | Phase 22 | Complete |
| HARDEN-03 | Phase 22 | Complete |
| HARDEN-04 | Phase 22 | Complete |
| HARDEN-05 | Phase 22 | Complete |
| HARDEN-06 | Phase 22 | Complete |
| HARDEN-07 | Phase 22 | Complete |
| HARDEN-08 | Phase 22 | Complete |
| HARDEN-09 | Phase 22 | Complete |
| HARDEN-10 | Phase 22 | Complete |
| HARDEN-11 | Phase 22 | Complete |
| HARDEN-12 | Phase 22 | Complete |
| FND-01 | Phase 23 | Complete |
| FND-02 | Phase 23 | Complete |
| FND-03 | Phase 23 | Complete |
| FND-04 | Phase 23 | Complete |
| FND-05 | Phase 23 | Complete |
| FND-06 | Phase 23 | Complete |
| WEB-01 | Phase 24 | Complete |
| WEB-02 | Phase 24 | Complete |
| WEB-03 | Phase 24 | Pending |
| WEB-04 | Phase 24 | Pending |
| CHAT-01 | Phase 25 | Pending |
| CHAT-02 | Phase 25 | Pending |
| CHAT-03 | Phase 25 | Pending |
| CHAT-04 | Phase 25 | Pending |
| APRV-01 | Phase 25 | Pending |
| APRV-02 | Phase 25 | Pending |
| APRV-03 | Phase 25 | Pending |
| DISP-01 | Phase 26 | Pending |
| DISP-02 | Phase 26 | Pending |
| DISP-03 | Phase 26 | Pending |
| DISP-04 | Phase 26 | Pending |
| DISP-05 | Phase 26 | Pending |
| SWARM-01 | Phase 26 | Pending |
| GRAPH-01 | Phase 27 | Pending |
| GRAPH-02 | Phase 27 | Pending |
| GRAPH-03 | Phase 27 | Pending |
| GRAPH-04 | Phase 27 | Pending |
| GOV-01 | Phase 28 | Pending |
| GOV-02 | Phase 28 | Pending |
| GOV-03 | Phase 28 | Pending |
| ONBD-01 | Phase 28 | Pending |
| ONBD-02 | Phase 28 | Pending |
| MCPW-01 | Phase 29 | Pending |
| MCPW-02 | Phase 29 | Pending |
| MCPW-03 | Phase 29 | Pending |
| SKW-01 | Phase 29 | Pending |
| SKW-02 | Phase 29 | Pending |
| SKW-03 | Phase 29 | Pending |

**Coverage:**

- v1 requirements: 50 total (12 HARDEN + 6 FND + 4 WEB + 4 CHAT + 3 APRV + 5 DISP + 4 GRAPH + 1 SWARM + 3 GOV + 2 ONBD + 3 MCPW + 3 SKW)
- Mapped to phases: 50 (one-to-one, Phases 22–29)
- Unmapped: 0 ✓

**Phase distribution:**

- Phase 22 (Agent Perimeter Hardening): HARDEN-01..12 (12)
- Phase 23 (Frontend Infrastructure & Industrial Foundation): FND-01..06 (6)
- Phase 24 (Web Foundation — Serve + Auth + Health): WEB-01..04 (4)
- Phase 25 (Chat + Approval Center): CHAT-01..04, APRV-01..03 (7)
- Phase 26 (Typed-Display Protocol + Router): DISP-01..05, SWARM-01 (6)
- Phase 27 (Neo4j Graph Explorer): GRAPH-01..04 (4)
- Phase 28 (Governance Boards + Web Onboarding): GOV-01..03, ONBD-01..02 (5)
- Phase 29 (Governance Write — MCP Configuration + Skills Install): MCPW-01..03, SKW-01..03 (6)

---
*Requirements defined: 2026-06-15*
*Last updated: 2026-06-15 — roadmap revised; MCP Configuration (MCPW-01..03) + Skills Install & Lifecycle (SKW-01..03) pulled into v1.0.0 per operator directive and mapped to a new LAST Phase 29 (Governance Write — MCP Configuration + Skills Install); all 50 v1 requirements mapped to Phases 22–29 (0 unmapped, 0 duplicates). Category breakdown 12+6+4+4+3+5+4+1+3+2+3+3 = 50; the one-to-one body↔table mapping is authoritative. Earlier headline counts (33, 39, 44) were superseded as scope grew.*
