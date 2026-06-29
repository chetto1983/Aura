# Phase 26: Typed-Display Protocol + Router - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-18
**Phase:** 26-typed-display-protocol-router
**Areas discussed:** Display placement, Chart fidelity, Source Explorer scope, Citation depth, Citation source-of-truth, Display persistence/replay, System-event scope, Swarm report drill-down, web_result card richness, code/document affordances, Answer action bar, Mobile display behavior, Source Explorer access/placement, Table interactions, Display loading states, a11y/i18n

> Two deep-research passes (operator-directed: "deep research on D:/tmp + best 2026 patterns") fed every decision — 3 parallel `gsd-advisor-researcher` agents on the primary areas, then 4 on the secondary areas. Primary sources surveyed: `D:/tmp/elysia` + `elysia-frontend` (router, citation pipeline, source explorer, pagination, merged tabs), `D:/tmp/odysseus` (operator-OS patterns — confirmed all deferred), `D:/tmp/assistant-ui` (runtime grain), plus the live Aura backend (`internal/agui`, `internal/agent`, `internal/web`, `internal/swarm`, `internal/runner`, `internal/conversations`).

---

## Display placement

| Option | Description | Selected |
|--------|-------------|----------|
| Inline + expand | Typed cards inline (upgrade D-02 card in place), per-card click-to-expand temporary full-view; no docking | ✓ |
| Inline only | Typed cards inline, no expand affordance | |
| Separate Displays tab | Dedicated workspace mode (elysia does NOT do this) | |

**User's choice:** Inline + expand (D-01)
**Notes:** assistant-ui's native grain (`tools.Fallback` already wired); elysia renders all displays inline, expand = temporary full-view swap (not a dock).

## Chart fidelity

| Option | Description | Selected |
|--------|-------------|----------|
| SVG bars MVP | Define chart payload now (swap-ready), render zero-dep SVG/table-as-bars | ✓ |
| uPlot (~14KB gz) | Real charts now, tiny bundle | |
| recharts (~136KB gz) | What elysia uses; richest API, heaviest | |

**User's choice:** SVG bars MVP (D-02)
**Notes:** No Phase-26 tool emits numeric series yet; single-binary bundle sensitive. uPlot is the escalation path, never recharts.

## Source Explorer scope

| Option | Description | Selected |
|--------|-------------|----------|
| Read-only now | Table + read-only Metadata + read-only Config + warnings; editing → Phase 29 | ✓ |
| Editable now | Operator edits mappings/render config; needs new persistence + PATCH + capability gating | |

**User's choice:** Read-only now (D-03)
**Notes:** elysia's Metadata/Config are PATCH-writes + destructive jobs (Re-Analyze/Clear) — that's a Phase-29 governance-write surface.

## Citation depth

| Option | Description | Selected |
|--------|-------------|----------|
| Full hovercard + click-through | Chip → hovercard preview → click opens Source Explorer; fix elysia's 2 bugs | ✓ |
| Minimal numbered chip | Styled chip, no preview/click | |

**User's choice:** Full hovercard + click-through (D-04)
**Notes:** The chosen reference (01/06-SPEC). Requires source-registry on the wire + assistant-ui hovercard primitive.

## Citation source-of-truth

| Option | Description | Selected |
|--------|-------------|----------|
| Hybrid: registry + model [n] | Normalizer assigns stable ids + numbered list; model places [n]; registry owns truth; cache-safe | ✓ |
| Auto-number sources only | Number all sources, no per-claim placement (elysia end-append) | |

**User's choice:** Hybrid: registry + model [n] (D-05)
**Notes:** Aura has no per-source id today. Static convention in messages[0]; volatile list tail-injected (never messages[0] — AG-031 guard). Anthropic Citations API not usable (OpenAI-wire DeepSeek).

## Display persistence/replay

| Option | Description | Selected |
|--------|-------------|----------|
| Persist via re-derive | Re-run normalizer over the already-persisted raw tool result at snapshot time; zero new storage | ✓ |
| Store the payload | Serialize DisplayPayload into the turn; new migration | |
| Live-only | Displays vanish on reload | |

**User's choice:** Persist via re-derive (D-06)
**Notes:** Tool results already persisted as Role:'tool' turns (+.result sidecar). PREREQUISITE: cockpit appears to not rehydrate history on reopen today — planner must verify vs uncommitted overhaul + wire it.

## System-event scope

| Option | Description | Selected |
|--------|-------------|----------|
| WebError + swarm-status | Only the stable safe classified classes today; zero new backend | ✓ |
| Expand: classify more now | Add sandbox/MCP/rate-limit; needs new backend classification + amendment | |

**User's choice:** WebError + swarm-status only (D-07)
**Notes:** Sandbox/shell/MCP/rate-limit/self-healing are free-form or unpropagated; suggestions have no backend producer. ask_user is already Phase 25.

## Swarm report drill-down

| Option | Description | Selected |
|--------|-------------|----------|
| Table + in-place expand | swarm_report table; expand row → summary+error+question/options (all in payload); zero new backend | ✓ |
| Full transcript drill-down | Drill into child .jsonl; needs new authenticated file-read endpoint | |

**User's choice:** Table + in-place expand (D-08)
**Notes:** Full transcript = deferred follow-up. Matches Claude Code / nanobot / LangGraph: summary inline, transcript behind a deliberate path.

## web_result card richness

| Option | Description | Selected |
|--------|-------------|----------|
| Rich, no external images | Domain chip + snippet + score + date + citation; no external favicons/thumbnails (privacy) | |
| Full rich incl. thumbnails | Also render thumbnails + favicons (external client fetch) | ✓ |
| Lean | Title + url + snippet + citation only | |

**User's choice:** Full rich incl. thumbnails (D-09)
**Notes:** Operator chose the premium visual over my recommendation. CONTEXT.md records the MANDATORY safety constraint: render external images via a backend image-proxy reusing the web SSRF allowlist/DNS-pin (or no-referrer + lazy + CSP img-src) so the whole-origin-private cockpit never leaks browsing / opens client-side SSRF.

## code/document affordances

| Option | Description | Selected |
|--------|-------------|----------|
| Mono + copy + collapse | Escaped mono <pre>, copy, collapse; no highlighting (01-SPEC deferred) | |
| Add lazy syntax highlighting | Lazy-loaded highlighter chunk for code | ✓ |

**User's choice:** Add lazy syntax highlighting (D-10)
**Notes:** Operator chose premium over my recommendation. CONTEXT.md records the constraint: lazy-loaded chunk (Shiki — escaped-span tokenization, never executes) preserves HARDEN-08 + bundle; consciously reconciles the 01-SPEC rehypeHighlight deferral via code-splitting.

## Answer action bar

| Option | Description | Selected |
|--------|-------------|----------|
| Defer rating group | Keep 25-UI-SPEC Copy/Edit/Reload + duration; defer thumbs rating | ✓ |
| Fold in full feedback bar | Add thumbs + persistence now via amendment + new store | |

**User's choice:** Defer rating group (D-11)
**Notes:** Rating not a typed-display concern; not in DISP-01..05; needs new feedback store.

## Mobile display behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Inline + expand-to-fullscreen | Inline single-column + collapse; expand → full-screen sheet (reuses D-01) | ✓ |
| Separate bottom-drawer | Heavy displays in a dedicated bottom drawer (Frame 05 literal) | |

**User's choice:** Inline + expand-to-fullscreen (D-12)
**Notes:** Reconciles Frame 05's "drawer" intent without a separate drawer system.

## Source Explorer access/placement

| Option | Description | Selected |
|--------|-------------|----------|
| Sheet from citation + Sources button | Opens from citation click + answer-level "Sources (N)"; fullscreen sheet (same as display expand) | ✓ |
| Dedicated route/panel | Persistent panel or /sources route | |

**User's choice:** Sheet from citation + Sources button (D-13)
**Notes:** No new shell/nav surface; consistent with the inline+expand pattern.

## Table interactions

| Option | Description | Selected |
|--------|-------------|----------|
| Sort + filter + copy/CSV | Client-side sortable columns + filter + copy/CSV export + in-card pagination | ✓ |
| Read-only static table | Paginated but no sort/filter/export | |

**User's choice:** Sort + filter + copy/CSV (D-14)
**Notes:** All client-side, no backend.

## Display loading states

| Option | Description | Selected |
|--------|-------------|----------|
| Progressive swap | Running raw card while tool runs; swap to typed display on completion | ✓ |
| Typed skeleton | Render a typed skeleton mid-stream | |

**User's choice:** Progressive swap (D-15)
**Notes:** No mid-stream type guessing; robust. Per-type empty/error states follow the design system.

## a11y / i18n

| Option | Description | Selected |
|--------|-------------|----------|
| Apply existing gates | WCAG AA contrast gate; keyboard/tap access (hover never the only path); en+it labels | ✓ |
| Discuss specifics | A specific requirement to nail down | |

**User's choice:** Apply existing gates (D-16)
**Notes:** No new decision — held to the existing enforced bar.

---

## Claude's Discretion

- `aura.display` = additive twin of `aura.artifact` (no `generativeUI` JSON-spec, no MCP-Apps iframe — both evaluated and rejected); fallback-to-raw-card mandatory.
- The `DisplayPayload` Go type union + per-type structs; `internal/agent/display/` package layout; normalizer wiring order (web_search/web_fetch/sandbox/swarm/WebError/table first).
- `DisplayRouter.tsx` shape (mirror elysia `RenderDisplay.tsx:51`); source-registry wire shape (`{refId,index,type,title,url?,snippet,confidence?,cited,object?}`).
- Merged-result tabs = optional stretch (~40 LOC, elysia `MergeDisplays.tsx`).
- Migration numbering (only if storage proves needed); test plan; empty/loading/error states.

## Deferred Ideas

- `graph_chunk` typed display + Neo4j Graph Explorer → Phase 27.
- Governance WRITE surfaces (MCP config, skills install) → Phase 29 (bounds the read-only Source Explorer + deferred rating store).
- `ui_control` operator-OS shell (dock/tile/icon-rail/AI-UI events; Frame 07) → follow-up milestone. No odysseus dock machinery this phase.
- Full swarm-child `.jsonl` transcript drill-down → separate follow-up plan (new authenticated file-read endpoint).
- Broader `system_event` classes (sandbox/MCP/rate-limit/self-healing/suggestion-as-prompt) → future phase (each needs new backend classification).
- Answer feedback rating group (thumbs + persistence) → needs new store + amendment.
- uPlot / real charting library → only when a tool emits numeric series.
