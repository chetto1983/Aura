# Phase 25: Chat + Approval Center - Context

**Gathered:** 2026-06-17
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver the **Core-Value agent loop end-to-end from the cockpit**: the operator
types a prompt, watches the streamed answer, manages conversations, sees
cost/cache/context-budget, and resolves HITL interrupts from a cross-thread
queue — over the EXISTING AG-UI/SSE transport (`POST /agent/run`),
`conversations.Store`, and `askuser.Store`. Requirements: **CHAT-01..04,
APRV-01..03**.

This is the milestone's Core-Value moment — the first phase where the cockpit
*does the thing Aura is for*. It builds the chat lane (assistant-ui), thin HTTP
adapters over the already-complete conversation store, a per-turn runtime
instrument footer, and the Claude-Code-style approval/HITL experience.

**Scope addition decided in discussion (flag for plan-time amendment):** the
operator chose **full assistant-ui branch trees** (D-09), which is *beyond* the
literal CHAT-01 ("send a prompt and watch the streamed response"). It requires
a conversation-tree backend that does not exist today. This is a deliberate,
recorded scope expansion — see D-09 and the `## Deferred Ideas` note. Per the
PRD-first principle, the planner should record a ROADMAP/REQUIREMENTS amendment
(a CHAT-05-style branch-tree requirement) before implementing it.

**Out of bounds (later phases — do NOT pull forward):**
- **Typed-display protocol + `switch(payload.type)` router** → Phase 26 (DISP-).
  Phase 25 renders tool output as name + collapsed/expandable raw result only.
- **Neo4j Graph Explorer** → Phase 27.
- **Read-only governance boards + web onboarding** → Phase 28.
- **Governance WRITE surfaces (MCP config, skills install/approval)** → Phase 29.
  The skills/MCP *approval queue* reuses the SAME `Interrupt`/`Resume[]` protocol
  built here, but its UI lands in Phase 29.
- **`ui_control` operator-OS shell** (dock windows, adaptive icon rail, command
  palette, AI-driven UI events; ux-spec Frame 07) → follow-up milestone
  (PROJECT.md §Deferred). No dockable tools / command palette this phase.
- **Multi-user auth / RBAC / OAuth** → out of scope for the whole milestone.

</domain>

<decisions>
## Implementation Decisions

### Reasoning + tool-activity exposure (CHAT-03)
- **D-01 — Reasoning drawer ON by default.** Flip the cockpit SSE path to
  `Translate(…, true)` so REASONING_* events stream into a collapsible reasoning
  drawer every turn. This is a **deliberate operator override** of the
  conservative redacted web default (`internal/agui/server.go:212`,
  `Translate(…, false)`; live CoT is a Telegram-only opt-in today). Justified by
  the **whole-origin-private, single-operator** cockpit (Phase 24 D-03): the
  only viewer is the authenticated operator. The drawer is collapsible with a
  persisted show/hide preference (builder default). NOTE the Phase-22 posture —
  the reasoning *trace* still does not persist verbatim history by default
  (HARDEN-05); surfacing CoT *live in the drawer* is the cockpit-stream concern,
  separate from trace persistence.
- **D-02 — Tool activity = name + collapsed raw result.** The live
  tool-activity stream shows each tool call's name + status, with an **expandable
  raw text/JSON result blob** now. Rich typed rendering (web_result / document /
  code / table / chart) is **Phase 26's** display router — Phase 25 deliberately
  ships the lightweight raw view and hands off the typed upgrade to DISP-01..05.

### Approval Center (APRV-01..03) — "perfectly like Claude Code"
- **D-03 — Resolution is inline, in-thread (the Claude Code interrupt model).**
  An `ask_user`/HITL interrupt renders as a card **in that conversation's
  message stream** — question + option buttons (+ free-text where the pause
  allows) — and the operator answers *in place*; the run resumes immediately over
  the existing `Resume[]` protocol. No heavyweight separate "approvals
  dashboard" as the primary surface.
- **D-04 — Cross-thread awareness via a header badge + lightweight list.**
  Because an `ask_user` can fire in a **background or scheduled** thread the
  operator is not viewing (Aura is multi-thread; Claude Code is single-session),
  a persistent **pending-count badge** in the app shell + a lightweight list lets
  the operator **jump straight to that thread's inline card**. This is the
  minimal aggregation APRV-01's "cross-thread list" requires — resolution still
  happens inline (D-03). Backend gap: `askuser.Store` has **no cross-thread
  pending query** today (`ListPending` is per-conversation; `ListRecent(limit)`
  returns recent records) — the planner adds a `status='pending'` aggregate read.
- **D-05 — Verb semantics map to Claude Code's allow/deny/interrupt.**
  - **Answer** (pick option / type) → `MarkResumed` with that answer; run resumes.
  - **Deny** → resume with an explicit **"declined" answer** so the agent
    continues *informed of the refusal* (Claude Code's "deny" does NOT kill the
    session — the model is told no and proceeds).
  - **Cancel run** (esc) → abort the whole run + auto-resolve the pending
    (`AutoResolveForConversation` path).
- **D-06 — Stale / auto-terminated render explicit terminal state (APRV-03).**
  An expired interrupt or auto-terminated run shows its terminal state ("expired
  / auto-resolved") inline AND in the badge list — never silently disappears.

### Conversation management (CHAT-02)
- **D-07 — Archive-first; hard-delete behind a confirm dialog.** Primary list
  action **archives** (reversible, `Store.UpdateStatus`); a separate explicit
  "Delete permanently" with a confirm dialog calls `Store.Delete`. Exercises
  both store methods; safe default (Claude.ai-style).
- **D-08 — FTS search = snippet result list → open thread at match.** Present
  `SearchConversationTurns` hits as a panel of matching turns with highlighted
  snippet + conversation title; clicking opens that thread (scrolled to the
  match). Uses the `SearchResult` projection as-is.
- **Builder defaults (no operator input needed):** inline rename (click title →
  `Store.Rename`), auto-title from the first turn (`Store.SetTitleIfNull`,
  editable), recent-first sidebar, `Store.List(includeArchived)` drives the list.

### Chat interaction scope + footer (CHAT-01, CHAT-04)
- **D-09 — FULL assistant-ui branch trees, built IN Phase 25 as an explicit
  sub-slice.** The operator chose the richest option: message
  branching/versioning + edit/regenerate, not just linear chat. This is a
  **deliberate scope addition beyond CHAT-01** and requires backend that does
  not exist today:
  - a **schema migration** adding parent/branch pointers to
    `aura.conversation_turns` (the next migration slot — verify numbering per
    PROJECT.md §Persistence),
  - **path-aware history loading** — `conversations.LoadManagedHistory` walks the
    *selected* branch path, not a flat sequence,
  - **edit / re-run-from-a-point** semantics in the agent-run wiring,
  - **KV-cache stable-prefix care** (CAP-04 `messages[0]` invariant) so branching
    does not poison the cache — the selected-path reconstruction must stay
    deterministic; this interacts with the cross-slice cache-invariant CI gate.
  Baseline streaming + a **stop/interrupt button** (ctx-cancel the active turn)
  are included as the Claude-Code core loop. The planner should treat the
  conversation-tree backend as a distinct, sequenced work item and record the
  ROADMAP/REQUIREMENTS amendment first.
- **D-10 — Runtime footer = per-turn + session totals.** Footer shows the latest
  turn's **tokens + cache-hit % + estimated $**, plus a **running session
  cumulative**. Estimated $ is feasible without a bespoke pricing table —
  **OpenRouter returns a native `cost` field** in usage. Cache metrics are
  already persisted (`Store.AppendAssistantTurnWithCacheMetric`); the SSE stream
  carries no cost/cache fields today, so the data source (additive turn-complete
  signal vs thin REST read of the persisted `cache_metrics`) is a **planner
  decision** — keep it OFF the Phase-26 `aura.display` namespace.

### Context-budget visibility (operator-added area — read-only)
- **D-11 — Indicator = fill gauge + microcompact event markers.** Show a compact
  gauge of tokens-in-context vs the model window (e.g. "42k / 128k · 33%") plus a
  marker when the microcompact ladder fires ("compacted N older turns"). Surfaces
  both fullness and *why* older context dropped. Read-only projection of the
  existing `LoadManagedHistory` + microcompact-ladder state (Phase 1.8b).
- **D-12 — Lives in the footer alongside cost + cache.** One **runtime
  instrument cluster** (cost + cache-hit + context-budget) — the operator's
  at-a-glance gauges. Cohesive with CHAT-04's footer.

### Claude's Discretion (research/planner-resolved — no operator input needed)
- assistant-ui **runtime adapter** choice (`@assistant-ui/react-ag-ui` runtime
  vs `useExternalStoreRuntime`/`useLocalRuntime` mapping the SSE) — pick whatever
  cleanly carries streaming + branching + stop over `POST /agent/run`. PROJECT.md
  names `@assistant-ui/react-ag-ui`; confirm at research time it supports the
  branch-tree + stop affordances or wire an external-store runtime.
- The **cross-thread pending-query** shape on `askuser.Store` (new
  `status='pending'` aggregate read) and its thin HTTP adapter.
- The **conversation HTTP adapter** surface (`GET/POST /api/conversations…`) —
  thin REST over `conversations.Store`, under the Phase-24 `/api/` carve-out and
  behind the `RequireAuth` whole-origin gate. Reasoning drawer + tool activity +
  footer reads/streams ride the existing `/agent/run` SSE + `/threads/{id}/
  messages` + new `/api/` reads.
- **Footer data source** (turn-complete SSE signal vs REST read of persisted
  `cache_metrics`) — D-10.
- **Empty/error/loading states**, mobile/responsive behavior (ux-spec Frame 05),
  and conversation-list navigation chrome — standard, follow Phase 23 tokens.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher + planner) MUST read these before planning or
implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 25: Chat + Approval Center" — goal, the 5
  success criteria, CHAT-01..04 + APRV-01..03, depends-on Phase 24, UI hint: yes.
- `.planning/REQUIREMENTS.md` CHAT-01..04 (lines 48-51), APRV-01..03
  (lines 55-57); **line 136** (`showReasoning` web policy is THIS phase — D-01).
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" + §Out of Scope — the
  single-binary invariant, minimum-boundary / no-RBAC mandate, and the
  `ui_control` shell deferral that bounds Phase 25.

### UI/UX contract
- `docs/design/aura-deep-search-figma/ux-spec.md` — **Frame 01** (Chat + Display
  Workspace: composer, running status, chat message, citation bubbles, feedback),
  **Frame 02** (Tree View + Reasoning Drawer — drives the D-01 reasoning drawer),
  **Frame 04** (System Events — `ask_user` pause → `system_event` w/
  `needs_user_input`), **Frame 05** (Mobile). §"Aura Backend Capability Patterns"
  lines 121-130 (run-timeline mapping, **`ask_user` is not just a chat message —
  approval-center component** w/ clarification/choice/approval/priority/resume
  token/accept-decline-cancel/stale states → APRV). §"Implementation Model"
  `approval_item` (line 510) + `run_timeline_event` (506) + `runtime_status`
  cache-hit (505). §"Important Non-Goals" (no swarm/mailbox chat; typed displays
  are Phase 26's point — Phase 25 ships the lightweight raw view).

### Prior phase context (carried forward — do NOT re-decide)
- `.planning/phases/24-web-foundation-serve-auth-health/24-CONTEXT.md` — the SPA
  host + `/api/` carve-out (D-04/exclusion list), the **whole-origin-private
  `RequireAuth` gate** (D-03) that protects every Phase-25 route, the
  `capability_grants` principal seam on `POST /agent/run` (D-04), theme/density
  before paint (D-08).
- `.planning/phases/23-frontend-infrastructure-industrial-foundation/23-CONTEXT.md`
  — React Router (D-14), React Query, the dark-operator design-token theme +
  density (D-07/D-08), committed `web/dist` + Node-24 rebuild + CI freshness gate.

### Architecture / stack / pitfalls (LOCKED shape)
- `.planning/research/ARCHITECTURE.md` — serve/embed + the four-layer write
  protection model (proxy → principal → `capability_grants` → risk/approval gate)
  the approval center sits within; §5 observability (`runtime_status` — what
  exists for the footer).
- `.planning/research/STACK.md`, `.planning/research/PITFALLS.md` — milestone
  stack + pitfalls (assistant-ui / SSE / embed context).

### KV-cache invariant (D-09 risk)
- `prd.md` §Slice 4 (KV cache) + the cross-slice `messages[0]` cache-invariant CI
  gate (`scripts/cache_invariant_audit.sh`) — branch-tree history reconstruction
  (D-09) must not break the stable prefix. Memory
  `reference_aura_cache_poisoning_sites_2026-05-27` maps the mutation sites.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/agui/server.go`** — the AG-UI gateway. Only `POST /agent/run`
  (SSE, `handleRun` → `streamSSE(… Translate(threadID, runID, idgen, turn,
  false))`) and `GET /threads/{id}/messages` (`handleMessages`,
  MESSAGES_SNAPSHOT) exist today. D-01 flips the `false` → reasoning-on for the
  cockpit stream; `maxRunBodyBytes` caps the run body; `redactEvent` /
  `sanitizeErr` are the WR-03 redaction belt-and-suspenders.
- **`internal/conversations/store.go`** — COMPLETE for CHAT-02: `Create`, `Get`,
  `List(includeArchived)`, `UpdateStatus` (archive), `Rename`, `SetTitleIfNull`
  (auto-title), `CountTurns`, `LoadHistory`, `SearchConversationTurns(query,
  limit)` (FTS, returns `SearchResult`), `Delete` (hard). `context.go`
  `LoadManagedHistory(ctx, id, ContextConfig)` (microcompact ladder — D-11
  source). `store_append.go` `AppendTurn` + `AppendAssistantTurnWithCacheMetric`
  (D-10 cache-metric source). **Branching (D-09) extends this surface.**
- **`internal/askuser/store.go`** — HITL store: `Insert`, `GetByToken`,
  `ListPending(conversationID)` (per-conversation — NOT cross-thread),
  `ListRecent(limit)`, `MarkResumed(token, ResumeAnswer)`, `MarkResumedBatch`,
  `AutoResolveForConversation` (D-05 cancel path), `CleanupResumedOlderThan`
  (D-06 stale). `ResumeAnswer` is the resume payload type. **D-04 adds a
  cross-thread `status='pending'` aggregate read.**
- **`internal/agui/auth.go` + `auth_cookie.go`** (Phase 24) — `RequireAuth`
  whole-origin gate + the `capability_grants` check on `POST /agent/run`; new
  Phase-25 `/api/` reads mount behind the same gate.
- **`internal/webui`** (`embed.go`, `Handler()`, `dist/`) — leaf embed package
  (no `internal/*` imports — `scripts/agui_boundary_check.sh` enforces). The
  React app (chat lane, approval badge, conversation list, footer) builds here.
- **`web/`** — Vite + React 19 + TS + React Router + React Query + i18next.
  **assistant-ui + ag-ui packages are NOT yet in `package.json`** — Phase 25
  introduces them. i18n: add keys to BOTH `en`+`it` in `web/src/i18n/resources.ts`
  (memory `reference_cockpit_i18n_react_i18next`).

### Established Patterns
- **Single-binary deploy invariant** — one Go binary, embedded frontend, one
  loopback `http.Server`; mount additively, no new listener/port.
- **Thin HTTP adapters over stores** — CHAT-02 conversation routes are thin REST
  over `conversations.Store`; no business logic in the handler.
- **Existing SSE/AG-UI transport is the chat data plane** — do NOT invent a new
  chat protocol; assistant-ui runtime consumes `POST /agent/run` SSE +
  `/threads/{id}/messages` snapshots.
- **Minimal-industrial-shape** ([[feedback_no_atomic_bombs_minimal_industrial_shape]])
  — drove D-02 (raw view now, typed displays Phase 26), D-04 (badge+list, not a
  dashboard). The ONE deliberate exception is D-09 (full branching) — an explicit
  operator-chosen scope addition, recorded as such.
- **Reasoning redaction posture** (Phase 22 HARDEN-05) — trace doesn't persist
  verbatim by default; D-01 is the *live cockpit stream*, distinct from trace
  persistence.
- **Frontend quality gates** (memory `feedback_frontend_quality_gates_coverage_mutation`)
  — `web/` must match the Go floors: Vitest coverage ≥85% + Stryker ≥70% killed +
  blocking CI.
- **i18n discipline** — `t('feature.key')`, en+it bundles, rebuild `dist` after
  copy changes.

### Integration Points
- assistant-ui runtime ↔ `POST /agent/run` (SSE stream: text + REASONING_* +
  tool activity) + `GET /threads/{id}/messages` (history snapshot) + new
  `GET/POST /api/conversations…` (list/search/rename/archive/delete) — all behind
  `RequireAuth` (Phase 24).
- Approval badge/list ↔ new cross-thread pending read on `askuser.Store` ↔ inline
  card resume (`MarkResumed`/`AutoResolveForConversation`) ↔ the `Interrupt`/
  `Resume[]` run protocol.
- Footer ↔ per-turn cost/cache (OpenRouter `cost` + persisted `cache_metrics`) +
  context-budget (`LoadManagedHistory` / microcompact state).
- Branch-tree backend (D-09) ↔ new migration on `aura.conversation_turns` ↔
  `LoadManagedHistory` path-walk ↔ agent-run re-run wiring ↔ KV-cache stable
  prefix (CAP-04 CI gate).

</code_context>

<specifics>
## Specific Ideas

- **"Perfectly like Claude Code"** — the operator's explicit reference model for
  the approval/HITL experience: inline, in-thread, contextual permission/clarify
  cards answered in place (D-03), allow/deny/interrupt verbs (D-05). Aura's only
  addition is cross-thread discovery (D-04) because Aura runs multiple/background
  threads where Claude Code has one session.
- **Premium operator cockpit** — the operator consistently chose the richest
  options (reasoning-on, full branching, $ cost, fill-gauge + compaction). This
  is consistent with the DGX-Spark bundle product vision
  (memory `project_aura_dgx_spark_bundle_vision`) — a polished, transparent
  instrument, not a minimal demo. Build to that bar where the backend allows it.
- **Runtime instrument cluster** — cost + cache-hit + context-budget live
  together in ONE footer (D-10/D-12), the operator's at-a-glance gauges.

</specifics>

<deferred>
## Deferred Ideas

- **Typed-display protocol + `switch(payload.type)` router** (web_result /
  document / code / table / chart / system_event / swarm_report) → **Phase 26**.
  Phase 25's tool-activity raw view (D-02) is the deliberate placeholder.
- **Neo4j Graph Explorer** → **Phase 27**.
- **Read-only governance boards (MCP / skills / scheduler) + web onboarding
  wizard** → **Phase 28**.
- **Governance WRITE surfaces (MCP config, skills install/approval queue UI)** →
  **Phase 29** (reuses the SAME `Interrupt`/`Resume[]` protocol built here).
- **`ui_control` operator-OS shell** (dockable tools, adaptive icon rail, command
  palette, slash actions, AI UI-control events; ux-spec Frame 07) + scheduler
  write surfaces → follow-up milestone (PROJECT.md §Deferred).
- **Estimated-$ pricing beyond OpenRouter's native `cost`** — if a non-OpenRouter
  provider is ever primary, a pricing table would be needed; not now.

### Scope note (NOT deferred — folded into Phase 25 by operator decision)
- **Full conversation branch trees (D-09)** are an explicit scope addition the
  operator chose to build IN Phase 25, not defer. Recorded here so the expansion
  is intentional: it needs a conversation-tree migration + path-aware history +
  re-run semantics + KV-cache-invariant care. The planner MUST record a
  ROADMAP/REQUIREMENTS amendment (e.g. a CHAT-05 branch-tree requirement) before
  implementing — PRD-first discipline.

</deferred>

---

*Phase: 25-chat-approval-center*
*Context gathered: 2026-06-17*
