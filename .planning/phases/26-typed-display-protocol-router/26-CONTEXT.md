# Phase 26: Typed-Display Protocol + Router - Context

**Gathered:** 2026-06-18
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver the **typed-display protocol (GAP-1)** end-to-end: the backend emits a
namespaced `aura.display` AG-UI **CUSTOM** event carrying a typed `DisplayPayload`
produced by a NEW Go normalizer (`internal/agent/display/`) via an **additive
`Actions.Display` slot**, and the cockpit renders it through a
`switch(payload.type)` **display router** — turning opaque tool output into
inspectable, paginated, source-viewable typed evidence. Requirements:
**DISP-01..05, SWARM-01.**

Display types in scope: `web_result`, `document`, `code`, `local_artifact`,
`table`, `chart` (DISP-02) + `system_event` (DISP-04, web-safety cards) +
`swarm_report` (SWARM-01). Plus a **Source Explorer** (DISP-05) and **citation
bubbles** (the cockpit-overhaul deliberately deferred citations to land HERE).

This phase is grounded in TWO deep-research passes over the curated `D:/tmp`
sources (**elysia / elysia-frontend** = the primary reference for the router,
citation pipeline, and source explorer; **odysseus** = operator-OS patterns,
all deferred; **assistant-ui** = the runtime) plus 2026 best-practice. The
`switch(payload.type)` router, the `aura.display`-as-twin-of-`aura.artifact`
protocol, and the citation-registry approach are all validated against working
code, not invented.

**Out of bounds (do NOT pull forward):**
- **`graph_chunk` typed display + the Neo4j Graph Explorer** → **Phase 27**. The
  router keeps an extensible default case; Phase 26 ships no graph display.
- **Governance WRITE surfaces** (MCP config, skills install) → **Phase 29**. This
  bounds the Source Explorer to **read-only** (D-03) and defers the
  feedback-rating store (D-11).
- **`ui_control` operator-OS shell** (dock windows, command palette, adaptive
  icon rail, AI-driven UI events; ux-spec Frame 07) → follow-up milestone
  (PROJECT.md §Deferred). **Take NOTHING from odysseus's dock/tile/icon-rail
  machinery.** The only borrow is the *concept* of expand, in elysia's minimal
  temporary-full-view form.
- **assistant-ui `generativeUI` JSON-spec part** and **MCP-Apps `ui://` iframes**
  were evaluated and **rejected** for Phase 26 (the former couples the Go backend
  to a React component vocabulary, violating "backend emits typed *data*"; the
  latter is a Phase-27+ `mcp_message` concern with the wrong trust granularity).

</domain>

<decisions>
## Implementation Decisions

### Protocol & wire (DISP-01)
- **D-PROTO (Claude's discretion, research-locked):** `aura.display` is an
  **additive twin of `aura.artifact`**. Add `DisplayEventName = "aura.display"`
  (mirror `ArtifactEventName`, `translator.go:19`) + a new `Actions.Display` slot
  on the `Actions` struct (mirror the `ArtifactDelta`/`AwaitingInput` omitempty
  pattern, `event.go:68-83`) + one `events.NewCustomEvent("aura.display", …)`
  branch in `Translate` (parallel to the `aura.artifact` branch,
  `translator.go:115-123`). The frontend reducer `sseAdapter.ts` is **extended
  additively** (NOT rewritten) with a CUSTOM/`aura.display` frame that attaches
  the typed payload to the matching tool part by `toolCallId` (today
  CUSTOM/aura.artifact is a no-op there, `sseAdapter.ts:99-103`).
- **D-FALLBACK (mandatory):** any tool WITHOUT a normalizer, and any payload with
  an unrecognized `type`, falls through to the **existing `ToolActivityCard` raw
  escaped `<pre>` card** (D-02 from Phase 25). The router's `default:` returns the
  raw card, never null — output is never lost. **HARDEN-08 posture is preserved**:
  untrusted output renders as escaped text, never markdown/HTML, UNLESS a trusted
  Go normalizer produced the typed payload.
- **`messages[0]` KV-cache invariant preserved** + `cache_invariant_audit.sh` CI
  gate green (DISP-01 success criterion).

### Display router & placement
- **D-01 — Placement = INLINE + click-to-expand.** Typed displays render inline
  in the chat thread, **upgrading the D-02 raw card in place** via the existing
  `tools.Fallback` seam (`ExternalStoreChat.tsx:425`). Each card has a
  click-to-expand to a **temporary full-view** (elysia's "result" view swap, Esc
  returns) — NOT a dock, NOT a persistent side panel, NOT a separate `Displays`
  tab. Rationale: it's the native assistant-ui grain, Phase-25-consistent, keeps
  evidence next to the answer, and doesn't pull forward the deferred Frame-07 shell.
- **D-15 — Loading = progressive swap.** While a tool runs, show the existing
  running raw card; **swap to the typed display when the `aura.display` payload
  arrives on completion**. No mid-stream type guessing. Per-type empty + error
  states follow the locked design system.
- **Collapsed/expanded default (Claude's discretion, research-locked):** reuse the
  `ToolActivityCard` settle-collapse state machine (`ToolActivityCard.tsx:85-95`)
  — the summary card (title + type icon + item count + first-item preview) is the
  visible unit; verbose/raw bodies collapse by default; manual toggle wins.
- **Merged-result tabs (optional stretch):** when one turn emits N displays of the
  same `type` from different sources, group into a tab strip (elysia
  `MergeDisplays.tsx` pattern, ~40 LOC). The single-display path works without it.

### Per-type render fidelity (DISP-02/04, SWARM-01)
- **D-02 — Charts = zero-dep SVG / table-as-bars MVP.** Define the typed `chart`
  payload now (elysia's `{x_labels, y_values, x_axis_label}` shape — swap-ready)
  but render with zero-dependency SVG/`<div>`-width bars + an accessible table
  fallback. **No charting library** in Phase 26 (single-binary bundle is sensitive;
  no Phase-26 tool emits numeric series yet). Escalation path = **uPlot (~14KB gz)**
  if a real series source lands later — **never recharts (~136KB gz)**.
- **D-09 — `web_result` = FULL rich, incl. thumbnails/favicons — rendered SAFELY.**
  Each card: domain chip + snippet + relevance hint (`score`) + `published_at` +
  citation bubble + **thumbnail/favicon**, grouped result sets. **MANDATORY safety
  constraint (operator chose the premium visual; the leak must be engineered out):**
  external images MUST NOT leak the operator's browsing or open a client-side SSRF/
  tracking surface in the whole-origin-private cockpit. Render them via a **backend
  image-proxy that reuses the existing web SSRF allowlist/DNS-pin defense**, OR at
  minimum `referrerpolicy="no-referrer"` + `loading="lazy"` + a CSP `img-src`. The
  SearXNG result metadata already carries `score/engine/category/published_at/
  thumbnail` (`internal/web/searxng.go:39-45`).
- **D-10 — `code`/`document` = lazy syntax highlighting + copy + collapse.** `code`
  (sandbox/shell output) renders mono with a **lazy-loaded highlighter chunk**
  (Shiki recommended — TextMate-grammar tokenization that emits *escaped* `<span>`s,
  never executes) + copy-to-clipboard + collapse-long-body. **This consciously
  reconciles the 01-SPEC `rehypeHighlight` deferral** (whose rationale was bundle
  weight) via code-splitting, and **preserves HARDEN-08** (tokenize-as-text, never
  execute untrusted output). `document` (web_fetch markdown) reuses the sanitized
  `MarkdownText.tsx` + copy.
- **D-14 — `table` = sort + filter + copy/CSV export.** Client-side sortable
  columns + a filter box + copy/CSV export (elysia-style), with in-card pagination
  (below). All client-side — no backend.
- **D-PAGINATION (Claude's discretion, research-locked):** pagination lives INSIDE
  each display card (port elysia `DisplayPagination.tsx`): items-per-page select +
  "X–Y of N" count + prev/next. Default 3 items/page.
- **`local_artifact` (Claude's discretion):** filename + size + download/path chip;
  reuse the existing `aura.artifact`/`ArtifactDelta` plumbing.

### System events (DISP-04)
- **D-07 — `system_event` scope = `WebError` + swarm-status ONLY.** The only
  backend classes with a stable, safe, classified shape TODAY: the web-safety enum
  (`internal/web/errors.go` — `blocked_url`/`unsupported_scheme`/`timeout`/
  `http_error`/`extraction_failed` + safe `reason` strings, no SSRF internals) and
  the swarm failed-child **`Status` enum** (`ok`/`failed`/`needs_user_input` —
  NOT the free-form `Error` text). **Zero new backend classification.**
- **DEFERRED (each needs NEW backend error-classification = scope expansion):**
  sandbox/shell errors (free-form `fmt.Errorf`, no enum), MCP errors (single leaky
  `ErrTransport`+`stderrTail`), rate-limit (`HTTPError{StatusCode,RetryAfterSec}`
  shape exists but is never propagated to the event stream), self-healing/retry
  (string-matching, no enum), and **suggestion-as-prompt** (NO Aura backend emits
  suggestions; elysia fetches them from a dedicated `/util/follow_up_suggestions`
  endpoint). `ask_user`/`needs_user_input` is already a **Phase-25** inline approval
  card — NOT a Phase-26 system_event.

### Citations & Source Explorer (DISP-03, DISP-05)
- **D-04 — Citations = FULL hovercard + click-through.** Numbered chip → hovercard
  preview (type-icon + title + snippet) → click opens the source in the Source
  Explorer. The chosen reference (`01-chat-thread-SPEC.md` §3.3/§14) + "top adopt"
  (`06-candidates-eval-SPEC.md` §5.1). Port the elysia `rehypeCitations` rehype
  plugin onto the EXISTING `MarkdownText.tsx` seams (it already passes
  `rehypePlugins` + `components`). **FIX elysia's two bugs:** (1) inline positional
  `[n]` splice at the supported claim — NOT the end-append `processTextWithCitations`
  fallback; (2) render images — drop elysia's hard-coded `prose-img:hidden`. **Skip
  `rehypeHighlight`** in the citation pipeline (bundle; highlighting is the
  separate lazy chunk per D-10). Hovercard chrome = an assistant-ui hovercard
  primitive (the one net-new frontend dep).
- **D-05 — Citation source-of-truth = HYBRID (code registry + model places `[n]`).**
  Aura has NO per-source id today (`web_search` returns a bare `{title,url,snippet}`
  array, `web_search.go:101`; `ToolResult` has no source channel). The new
  `internal/agent/display/` normalizer assigns **stable URL-keyed source ids**,
  renders a numbered `[n] Title — url` list into the **model-visible tool-result
  preview** (so the model can copy the number), AND populates the `aura.display`
  **source-registry** (powers the hovercard + Source Explorer). The **model places
  `[n]` inline** next to claims; the registry owns `[n]→source` truth (the model
  can't reference a number not in the list). Distinguish **`cited` vs `consulted`**
  sources in the registry (Source Explorer Table shows all consulted; chips show
  cited). **CACHE-CRITICAL:** a static "emit `[n]` next to claims; sources are
  numbered in the provided list" convention line is safe in `messages[0]`; the
  **volatile numbered source list MUST ride the existing tail-inject copy path**
  (`llm_agent.go:269`, like CurrentTime/budget) — NEVER `messages[0]`, or it trips
  the AG-031 KV-drift guard (`llm_agent.go:303`). Anthropic's Citations API is NOT
  usable (DeepSeek-V4 is OpenAI-wire).
- **D-03 — Source Explorer = READ-ONLY this milestone.** Table (sort/search/
  paginate) + read-only Metadata pane + read-only Configuration pane + warning
  banners (unprocessed/incomplete sources), backed by the SAME source-registry the
  citations use (one registry, two consumers). In elysia the Metadata/Configuration
  views are PATCH-writes + destructive jobs (Re-Analyze/Clear) — building that =
  new persistence table + migration + PATCH routes + capability gating = a
  governance-write surface that belongs to **Phase 29**. Re-scope Frame 03's
  "edits"/"controls" wording to "view".
- **D-13 — Source Explorer access = fullscreen sheet from citation + a "Sources
  (N)" button.** Opens two ways (citation click-through AND an answer-level
  "Sources (N)" affordance); renders as an **expand-to-fullscreen sheet** — the
  same pattern as the display expand (D-01), no docked panel/route.

### Persistence (replay across reload)
- **D-06 — Persist displays via RE-DERIVE from the persisted raw tool result.**
  Tool RESULTS are already persisted as `Role:'tool'` turns (`runner_persist.go:118`,
  full bytes spilled to a `.result` sidecar, `result.go:137-151`), and the
  MESSAGES_SNAPSHOT path already replays them. So **re-run the Phase-26 normalizer
  over the persisted tool turn at snapshot time** and emit the `DisplayPayload`
  into the snapshot exactly like the live `aura.display` — **ZERO new storage, one
  normalizer for live + replay.** Only fall back to *storing* a payload for a
  specific display whose data is NOT in the persisted preview+sidecar.
- **⚠ PREREQUISITE (planner must verify + wire):** the cockpit chat lane appears to
  **NOT rehydrate conversation history on reopen today** — `ExternalStoreChat`
  mounts `messages:[]` and nothing calls `GET /threads/{id}/messages` (research
  read the COMMITTED tree; **verify against the uncommitted cockpit-overhaul layer**
  before assuming). Display replay (and text/tool replay generally) depends on
  Phase 26 wiring a MESSAGES_SNAPSHOT→`ThreadMessageLike` rehydration fetch. The
  reducer is already snapshot-aware (`sseAdapter.ts:269-279`); nothing drives it.

### Swarm (SWARM-01)
- **D-08 — `swarm_report` = summary table + in-place row expand.** Render a table
  over `ChildReport` (goal-index / child-id / status dot / summary); click a row to
  expand `summary` + `error` (+ `question`/`options` for a `needs_user_input`
  child) inline. **Every expanded field is already in the `[]ChildReport` payload
  on the SSE wire** (`report.go:32-41`) → ZERO new backend; reuse the
  `ToolActivityCard` child-row + expand machine (`ToolActivityCard.tsx:34,72-160`).
  "No inter-agent chat / mailbox theater." The full per-child `.jsonl` transcript
  drill-down is a **deferred follow-up** (needs a new authenticated file-read
  endpoint over the disk runDir — not web-reachable today; matches Claude Code /
  nanobot / LangGraph: summary inline, full transcript behind a deliberate path).

### Answer chrome
- **D-11 — Action bar: rating group DEFERRED.** Keep the 25-UI-SPEC-locked
  Copy/Edit/Reload + show turn duration (cheap — elapsed already tracked). DEFER
  thumbs up/down rating: it's not a typed-display concern and needs a new feedback
  table + endpoint (no feedback store exists). (01-SPEC §14 lists the rating group
  for Phase 26, but it's NOT in DISP-01..05 — folding it in would need a
  REQUIREMENTS amendment; deferred instead.)
- **D-12 — Mobile = inline single-column + fullscreen-sheet expand.** Displays stay
  inline single-column, collapsed-by-default; the D-01 click-to-expand promotes a
  heavy display to a full-screen sheet — reconciles Frame 05's "displays open as
  drawers" intent WITHOUT a separate drawer system.
- **D-16 — a11y + i18n = held to existing gates.** Displays meet the enforced WCAG
  AA contrast gate; keyboard/tap access for citation popovers + pagination (hover
  is NEVER the only access path, per the ux-spec rule); all type labels +
  system-event reasons + Source Explorer column headers in **en + it**
  (`web/src/i18n/resources.ts`, rebuild `dist` after copy changes).

### Claude's Discretion (research-resolved — no operator input needed)
- The exact `DisplayPayload` Go type union + per-type structs (mirror the
  `ChildReport`/`WebError` flat shapes); the `internal/agent/display/` normalizer
  package layout; which tools wire normalizers first (recommend order:
  `web_search`→`web_result`, `web_fetch`→`document`, `sandbox/shell`→`code`/
  `local_artifact`, `swarm_spawn`→`swarm_report`, `WebError`→`system_event`,
  structured rows→`table`).
- The frontend `DisplayRouter.tsx` component shape (mirror elysia `RenderDisplay.tsx:51`).
- The source-registry wire shape (proposed: `{refId, index, type, title, url?,
  snippet, confidence?, cited, object?}` — validated ~1:1 against Vercel AI SDK
  `InlineCitation`).
- Migration numbering (if any storage is needed — verify the next slot per
  PROJECT.md §Persistence); test plan; empty/loading/error states.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher + planner) MUST read these before planning or
implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 26: Typed-Display Protocol + Router" — goal, the
  5 success criteria, DISP-01..05 + SWARM-01, depends-on Phase 25 (chat lane) +
  Phase 22 (default-untrusted provenance / swarm envelope underpins SWARM-01),
  UI hint: yes.
- `.planning/REQUIREMENTS.md` DISP-01..05 (lines 62-66), SWARM-01 (line 77).
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" (the typed-display bullet,
  single-binary invariant) + §Out of Scope (no-RBAC / read-only-write-deferred
  posture, the `ui_control` shell deferral that bounds this phase).

### UI/UX contract (UI hint: yes — consider /gsd-ui-phase)
- `docs/design/aura-deep-search-figma/ux-spec.md` — §"Elysia Patterns Adopted"
  (lines 43-64: typed payload router, display types, merged result tabs, citation
  bubbles, paginated result groups, source explorer Table/Metadata/Configuration),
  **Frame 01** (Chat + Display Workspace), **Frame 03** (Source Explorer),
  **Frame 04** (System Events — SSRF safe reasons, suggestion→prompt),
  **Frame 05** (Mobile — displays as drawers), §"Backend Capability Patterns"
  (~lines 420-430: tool→display mapping). NOTE: ux-spec lists `graph_chunk` /
  `mcp_message` / dockable windows / `ui_control` — these are Phase 27+/deferred,
  NOT Phase 26.

### Phase-26 boundary + chosen citation reference (LOCKED by the overhaul)
- `docs/cockpit-overhaul/01-chat-thread-SPEC.md` §3.3 (lines ~578-604) + §14 (lines
  ~1082-1097) — citations are DELIBERATELY deferred to Phase 26 with the chosen
  reference: elysia `rehypeCitations`-style inline positional splice + an
  assistant-ui hovercard primitive, **fixing elysia's two bugs** (end-append →
  inline splice; render images). Raw tool card (D-02) is the placeholder this
  replaces. Also defers syntax highlighting for bundle (reconciled by D-10's lazy
  chunk).
- `docs/cockpit-overhaul/06-candidates-eval-SPEC.md` §5.1 — elysia citation
  pipeline (`MarkdownFormat.tsx:39-111`, `CitationBubble.tsx:25-56`) = the "top
  adopt"; the documented bugs to fix.

### Prior phase context (carried forward — do NOT re-decide)
- `.planning/phases/25-chat-approval-center/25-CONTEXT.md` — D-02 (raw tool card =
  the Phase-26 handoff placeholder), the SSE/AG-UI chat data plane, the `RequireAuth`
  whole-origin gate every new `/api/` route inherits, the `messages[0]` KV-cache
  invariant + branch-tree path-aware history.
- `.planning/phases/24-web-foundation-serve-auth-health/24-CONTEXT.md` — SPA host
  + `/api/` carve-out + `RequireAuth` gate (the image-proxy + any Source-Explorer
  read endpoint mount behind it).
- `.planning/phases/23-frontend-infrastructure-industrial-foundation/23-CONTEXT.md`
  — React Router / React Query / the dark-operator (logo-matched blue) design-token
  theme + density + committed `web/dist` + Node-24 rebuild + CI freshness gate.

### Architecture / stack / pitfalls (LOCKED shape)
- `.planning/research/ARCHITECTURE.md` — serve/embed + the four-layer write
  protection model; §5 observability.
- `.planning/research/STACK.md`, `.planning/research/PITFALLS.md` — milestone
  stack + pitfalls (assistant-ui / SSE / embed).

### KV-cache invariant (D-05 risk)
- `prd.md` §Slice 4 (KV cache) + the cross-slice `messages[0]` cache-invariant CI
  gate (`scripts/cache_invariant_audit.sh`). The citation convention line goes in
  `messages[0]` (static, cache-safe); the volatile numbered source list rides the
  tail-inject copy path (`internal/agent/llm_agent.go:269`), guarded by AG-031
  (`llm_agent.go:303`). Memory `reference_aura_cache_poisoning_sites_2026-05-27`
  maps the mutation sites.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/agui/translator.go`** — `ArtifactEventName = "aura.display"`'s twin
  (`:19`) + the `events.NewCustomEvent(artifactEventName, …)` branch (`:115-123`)
  is the EXACT pattern to copy for `aura.display`. `Translate(…, showReasoning)`
  is the pure mapper; the new CUSTOM branch slots beside the artifact branch.
- **`internal/agent/event.go`** — the `Actions` struct (`:68-83`); add a
  `Display *DisplayPayload` slot mirroring `ArtifactDelta`/`AwaitingInput`
  (omitempty pointer, decode(encode)==identity).
- **`internal/agent/display/`** — NEW normalizer package (does not exist). Owns the
  typed `DisplayPayload` union, per-tool normalizers, the URL-keyed source registry,
  and the numbered-source-list rendering for the model-visible preview.
- **`internal/web/errors.go`** — the D-38 stable `WebError` enum + safe `reason`
  constants (`:11-30`); `system_event` (D-07) maps these directly. `sanitize()`
  (`:134`) is the non-leak chokepoint.
- **`internal/web/searxng.go`** — `web.Result{Title,URL,Snippet,Metadata}` +
  `ResultMetadata{engine,score,category,published_at,thumbnail}` (`:39-45`) — the
  source for `web_result` richness (D-09).
- **`internal/swarm/report.go`** — `ChildReport` (`:32-41`) + `Status*` constants
  (`:21-23`); `swarm_report` (D-08) renders this. `dumpTranscript` writes the
  per-child `.jsonl` (disk-only; the deferred drill-down source).
- **`internal/runner/runner_persist.go`** — tool RESULTS persisted as `Role:'tool'`
  turns (`:118-123`); the basis for re-derive replay (D-06).
- **`internal/agui/server.go`** — `handleMessages` / MESSAGES_SNAPSHOT
  (`:261-288`, `projectMessages` `:455-494`); where the re-derived display is
  attached per tool turn (D-06).
- **`web/src/chat/sseAdapter.ts`** — the KEPT-VERBATIM reducer; add a
  CUSTOM/`aura.display` frame to `AguiFrame` + attach the payload to the tool part
  by `toolCallId`. CUSTOM/aura.artifact is a no-op today (`:99-103`); the reducer is
  already snapshot-rehydration-aware (`:269-279`).
- **`web/src/chat/ExternalStoreChat.tsx`** — the single wiring point: `tools.Fallback`
  (`:425`) → branch `part.display ? <DisplayRouter/> : <ToolActivityCard/>`. Also
  where the history-rehydration fetch (D-06 prerequisite) must be added.
- **`web/src/chat/ToolActivityCard.tsx`** — the D-02 raw fallback card; reuse its
  status dots (`:15-31`), settle-collapse state machine (`:85-95`), and child-row
  shape (`:34-39`) for `swarm_report` rows.
- **`web/src/chat/MarkdownText.tsx`** — sanitized markdown + `rehypePlugins` +
  `components` seams; reuse for `document`/`table` and host the citation rehype
  plugin (D-04).

### Established Patterns
- **Namespaced CUSTOM event** (`aura.artifact`) — mirror for `aura.display`;
  additive `Actions` slot, omitempty, decode(encode)==identity.
- **`messages[0]` KV-cache invariant** + `cache_invariant_audit.sh` CI gate; volatile
  per-turn data tail-injected to a copy (CurrentTime/budget precedent).
- **Untrusted-output-as-text (HARDEN-08)** — never markdown/HTML for raw tool output;
  typed displays render only for trusted-normalizer-produced payloads; the lazy
  highlighter must emit escaped spans.
- **Thin HTTP adapters over stores, behind `RequireAuth`** — any new read endpoint
  (image-proxy, Source-Explorer read) mounts under `/api/` behind the whole-origin gate.
- **Web SSRF defense** (`web_fetch` IPv6/DNS-pin) — the image-proxy (D-09) reuses it.
- **Minimal-industrial-shape** ([[feedback_no_atomic_bombs_minimal_industrial_shape]]).
- **Frontend quality gates** ([[feedback_frontend_quality_gates_coverage_mutation]])
  — Vitest ≥85% + Stryker ≥70% killed + blocking CI.
- **i18n** — `t('feature.key')`, en+it bundles (`web/src/i18n/resources.ts`), rebuild `dist`.

### Integration Points
- `Actions.Display` → `translator.go` `aura.display` CUSTOM event → SSE →
  `sseAdapter` reducer → display part → `<DisplayRouter switch(payload.type)>` in
  `ExternalStoreChat`.
- `web/errors.go` WebError → `system_event` card. `swarm/report.go` ChildReport →
  `swarm_report` table.
- Source registry (NEW, on the `aura.display` payload) ← normalizer → CitationBubble
  hovercard → click-through → Source Explorer fullscreen sheet.
- Re-derive replay: `server.go` `projectMessages` (tool turn) → re-run normalizer →
  `DisplayPayload` in MESSAGES_SNAPSHOT → `ExternalStoreChat` history fetch → same router.
- Image-proxy (NEW, behind `RequireAuth`, reuses web SSRF guard) ← `web_result`
  thumbnails/favicons (D-09).

</code_context>

<specifics>
## Specific Ideas

- **elysia is the load-bearing reference** — the `switch(payload.type)` router
  (`RenderDisplay.tsx:51`), the citation pipeline (`MarkdownFormat.tsx` /
  `CitationBubble.tsx`, with its 2 documented bugs to fix), the source explorer
  (`DataExplorer`/`DataConfig`/`DataMetadata` — confirmed editable+destructive, so
  Aura takes the read-only subset), `DisplayPagination.tsx`, and `MergeDisplays.tsx`
  are all in `D:/tmp/elysia-frontend`. Port shapes, don't reinvent.
- **Premium operator cockpit** — the operator again chose the richest options
  (inline+expand, full citations, thumbnails, lazy syntax highlighting) consistent
  with the DGX-Spark bundle product vision ([[project_aura_dgx_spark_bundle_vision]])
  and [[feedback_cockpit_premium_bar_over_minimal]]. Build to that bar — and make
  the premium choices industrial/safe (the D-09 image-proxy, the D-10 escaped lazy
  highlighter) rather than declining them.
- **Perplexity-grade citations** — numbered chip → hover preview → click-to-source,
  with code-owned `[n]→source` truth (the model can't hallucinate a number). This
  is the deep-search core value.

</specifics>

<deferred>
## Deferred Ideas

- **`graph_chunk` typed display + Neo4j Graph Explorer** → **Phase 27** (the router
  keeps an extensible default case only).
- **Governance WRITE surfaces** (MCP config, skills install) → **Phase 29**; this is
  why the Source Explorer is read-only (D-03) and the feedback-rating store is
  deferred (D-11).
- **`ui_control` operator-OS shell** (dockable tools, adaptive icon rail, command
  palette, AI UI-control events; ux-spec Frame 07) → follow-up milestone. No
  odysseus dock/tile machinery this phase.
- **Full swarm-child `.jsonl` transcript drill-down** → separate follow-up plan
  (needs a new authenticated file-read endpoint over the disk runDir, with
  path-traversal confinement).
- **Broader `system_event` classes** (sandbox/shell, MCP, rate-limit, self-healing,
  suggestion-as-prompt) → each needs NEW backend error-classification + event-stream
  plumbing; a future phase, not Phase 26.
- **Answer feedback rating group** (thumbs up/down + persistence) → needs a new
  feedback store + a REQUIREMENTS amendment; deferred (D-11).
- **uPlot / real charting library** → only when a tool actually emits numeric series
  (D-02 ships the SVG-MVP + a swap-ready payload shape).

</deferred>

---

*Phase: 26-typed-display-protocol-router*
*Context gathered: 2026-06-18*
