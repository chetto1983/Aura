# Phase 26: Typed-Display Protocol + Router - Research

**Researched:** 2026-06-18
**Domain:** Full-stack typed-display protocol — Go normalizer (`internal/agent/display/`) + AG-UI CUSTOM event + assistant-ui `switch(payload.type)` display router, citations, source explorer, swarm report.
**Confidence:** HIGH (every CONTEXT.md anchor verified against the live working tree; deltas resolved against installed deps + curated elysia reference)

## Summary

Phase 26 is unusually well-specified: `26-CONTEXT.md` carries 16 locked decisions (D-01..D-16 + D-PROTO/D-FALLBACK/D-PAGINATION) with file:line anchors, and `26-UI-SPEC.md` is a checker-reviewed visual contract. This research is the **DELTA** the planner still needs: (1) verification that every anchor still resolves against the CURRENT tree, (2) a mandatory Validation Architecture, (3) concrete resolutions of the "Claude's Discretion (research-resolved)" items, (4) the real net-new frontend dependency surface, and (5) the image-proxy reuse seam.

**The working tree is clean** (only `.planning/STATE.md` modified). CONTEXT.md repeatedly warned that its earlier research read only the COMMITTED tree and that an "uncommitted cockpit-overhaul layer" might change things — **that layer does not exist as uncommitted changes; the committed tree IS the current truth.** Every anchor was verified against `HEAD`.

**The single highest-value finding (D-06 prerequisite, CONFIRMED REAL):** the cockpit chat lane does NOT rehydrate conversation history on reopen. `ExternalStoreChat.tsx:79` mounts `useState<ThreadMessageLike[]>([])` and **nothing** in the component fetches `GET /threads/{id}/messages` — there is no mount `useEffect` that reads the snapshot. The BACKEND endpoint already exists and works (`server.go:122` registers `GET /threads/{id}/messages` → `handleMessages` → `LoadHistory` → `projectMessages` → `MESSAGES_SNAPSHOT`). The gap is purely frontend: the snapshot is never fetched and never folded. Phase 26 must wire this fetch (it is the foundation for display replay AND general text/tool replay). **Drift note:** CONTEXT.md says `sseAdapter.ts:269-279` is "snapshot-rehydration-aware" — this is inaccurate. Lines 269-279 are the `TOOL_CALL_RESULT` case, which tolerates a result arriving without a prior START (useful when folding a snapshot's tool turns), but the reducer has **no MESSAGES_SNAPSHOT frame and no CUSTOM frame at all**; CUSTOM/`aura.artifact` and `aura.display` are silently dropped because they are not in the `AguiFrame` union (`sseAdapter.ts:104-118`).

**Primary recommendation:** Build the Go `DisplayPayload` union + `internal/agent/display/` normalizer as a pure data layer (no storage, no migration — next free slot is 0020 but D-06 needs zero), emit it via an additive `Actions.Display` slot through the existing `aura.artifact`-twin pattern, extend `sseAdapter.ts` with a CUSTOM/`aura.display` frame, and render through a `switch(payload.type)` router that defaults to the raw `ToolActivityCard` (never null). Wire the missing history-rehydration fetch as a Wave-0 prerequisite. Use **`@radix-ui/react-hover-card`** (already transitively present, MIT) — NOT a non-existent "assistant-ui hovercard primitive" — for citation hovercards, and **Shiki 4.2.0 fine-grained core** as the lazy code-split highlighter.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Typed-display normalization (`DisplayPayload`) | API / Backend (`internal/agent/display/`) | — | "Backend emits typed *data*"; trust boundary — only a Go normalizer produces a rich-renderable payload (HARDEN-08) |
| `aura.display` CUSTOM event emission | API / Backend (`internal/agui/translator.go`) | — | The translator is the pure agent→AG-UI mapper; the event rides the same SSE stream |
| Source-id registry assignment | API / Backend (`internal/agent/display/`) | — | Code owns `[n]→source` truth so the model cannot hallucinate a citation number (D-05) |
| Numbered source-list tail-inject | API / Backend (`prompt.Builder.Build`) | — | Volatile per-turn data must ride the append-to-copy path, never `messages[0]` (KV-cache invariant) |
| Image-proxy (thumbnail/favicon fetch) | API / Backend (`/api/` behind `RequireAuth`, reuses `internal/web` SSRF guard) | — | SSRF/DNS-pin defense lives server-side; the cockpit must not client-fetch arbitrary hosts |
| Display routing (`switch(payload.type)`) | Browser / Client (`DisplayRouter.tsx`) | — | Pure presentation over the trusted payload; the native assistant-ui grain |
| SSE reducer extension (attach payload to tool part) | Browser / Client (`sseAdapter.ts`) | — | The reducer folds frames; CUSTOM/`aura.display` attaches by `toolCallId` |
| History rehydration on reopen | Browser / Client (fetch) → Backend (existing endpoint) | — | Endpoint exists (`server.go:122`); only the client fetch is missing |
| Citation render / hovercard / source explorer | Browser / Client | — | Pure UI over the backend source-registry |
| Table sort/filter/CSV, pagination, chart SVG | Browser / Client | — | D-14/D-PAGINATION/D-02 explicitly "all client-side, no backend" |

## User Constraints (from CONTEXT.md)

> Verbatim from `26-CONTEXT.md`. The planner MUST honor these; research does not explore alternatives to locked decisions.

### Locked Decisions

- **D-PROTO:** `aura.display` is an additive twin of `aura.artifact`. Add `DisplayEventName = "aura.display"`, a `Actions.Display` slot, one `events.NewCustomEvent("aura.display", …)` branch in `Translate`. Extend `sseAdapter.ts` additively (CUSTOM/`aura.display` frame attached to the tool part by `toolCallId`).
- **D-FALLBACK (mandatory):** any tool without a normalizer / any unrecognized `type` falls through to the existing `ToolActivityCard` raw escaped `<pre>` card. Router `default:` returns the raw card, NEVER null. HARDEN-08 preserved.
- **`messages[0]` KV-cache invariant preserved** + `cache_invariant_audit.sh` CI gate green.
- **D-01:** Placement = INLINE + click-to-expand (temporary full-view, Esc returns). NOT a dock/side panel/separate tab.
- **D-15:** Loading = progressive swap (running raw card stays; swap to typed display on completion). No mid-stream type guessing.
- **D-02:** Charts = zero-dep SVG / table-as-bars MVP with `{x_labels, y_values, x_axis_label}` shape. NO charting library. Escalation = uPlot, never recharts.
- **D-09:** `web_result` = FULL rich incl. thumbnails/favicons rendered SAFELY via a backend image-proxy reusing the web SSRF allowlist/DNS-pin defense (or at minimum `referrerpolicy="no-referrer"` + `loading="lazy"` + CSP `img-src`).
- **D-10:** `code`/`document` = lazy syntax highlighting (Shiki — emits escaped spans, never executes) + copy + collapse. `document` reuses sanitized `MarkdownText.tsx`.
- **D-14:** `table` = sort + filter + copy/CSV export, in-card pagination, all client-side.
- **D-PAGINATION:** pagination INSIDE each display card (items-per-page select + "X–Y of N" + prev/next). Default 3 items/page.
- **D-07:** `system_event` scope = `WebError` + swarm-status ONLY. Zero new backend classification.
- **D-04:** Citations = FULL hovercard + click-through. Port elysia `rehypeCitations` onto existing `MarkdownText.tsx`, fixing elysia's 2 bugs (inline splice not end-append; render images). Skip `rehypeHighlight`. Hovercard chrome = an assistant-ui hovercard primitive (the one net-new frontend dep).
- **D-05:** Citation source-of-truth = HYBRID (code registry assigns stable URL-keyed ids + numbered list in model-visible preview; model places `[n]` inline). Distinguish `cited` vs `consulted`. CACHE-CRITICAL: convention line in `messages[0]` (static), volatile numbered list rides the tail-inject copy path (`llm_agent.go:269`), never `messages[0]` (AG-031 guard `llm_agent.go:303`).
- **D-03:** Source Explorer = READ-ONLY this milestone (Table + read-only Metadata + read-only Configuration + warning banners). No PATCH/write.
- **D-13:** Source Explorer access = fullscreen sheet from citation click-through AND an answer-level "Sources (N)" button. No docked panel/route.
- **D-06:** Persist displays via RE-DERIVE from the persisted raw tool result. ZERO new storage, one normalizer for live + replay. ⚠ PREREQUISITE: wire the missing history-rehydration fetch.
- **D-08:** `swarm_report` = summary table over `ChildReport` + in-place row expand. Zero new backend. No inter-agent chat/mailbox theater. Full `.jsonl` drill-down DEFERRED.
- **D-11:** Action bar rating group DEFERRED. Keep Copy/Edit/Reload + turn duration.
- **D-12:** Mobile = inline single-column + fullscreen-sheet expand.
- **D-16:** a11y + i18n held to existing gates (WCAG AA, keyboard/tap access, en+it).

### Claude's Discretion (research-resolved — see "Resolved Discretion Items" below)

- The exact `DisplayPayload` Go type union + per-type structs; the `internal/agent/display/` package layout; tool→normalizer wiring order; `DisplayRouter.tsx` shape; source-registry wire shape; migration numbering; test plan; empty/loading/error states.

### Deferred Ideas (OUT OF SCOPE)

- `graph_chunk` typed display + Neo4j Graph Explorer → Phase 27 (router keeps an extensible `default:` only).
- Governance WRITE surfaces (MCP config, skills install) → Phase 29 (this bounds Source Explorer read-only + defers the feedback-rating store).
- `ui_control` operator-OS shell (dock windows, command palette, icon rail, AI UI events) → follow-up milestone. Take NOTHING from odysseus's dock/tile/icon-rail machinery.
- assistant-ui `generativeUI` JSON-spec part + MCP-Apps `ui://` iframes → evaluated and REJECTED.
- Full swarm-child `.jsonl` transcript drill-down → deferred follow-up (needs a new authenticated file-read endpoint over the disk runDir with path-traversal confinement).
- Broader `system_event` classes (sandbox/shell, MCP, rate-limit, self-healing, suggestion-as-prompt) → future phase.
- Answer feedback rating group (thumbs up/down) → needs feedback store + REQUIREMENTS amendment.
- Real charting library (uPlot/recharts) → only when a tool emits numeric series.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DISP-01 | Backend emits namespaced `aura.display` CUSTOM event carrying typed `DisplayPayload` from a Go normalizer; additive `Actions.Display` slot; `messages[0]` cache invariant preserved | `translator.go:19/115-123` (artifact twin pattern verified); `event.go:68-83` (`Actions` struct verified); `prompt/builder.go:91-104` (tail-inject append-to-copy verified); `cache_invariant_audit.sh` (gate verified, drives `aura cache-audit`) |
| DISP-02 | Cockpit renders typed displays via `switch(payload.type)` router: `web_result`/`document`/`code`/`local_artifact`/`table`/`chart` | `sseAdapter.ts:104-118` (CUSTOM frame absent — additive extension point verified); `ExternalStoreChat.tsx:426-435` (`tools.Fallback` seam verified); elysia `RenderDisplay.tsx:51` (router shape; default returns null — Aura OVERRIDES to raw card) |
| DISP-03 | Operator can inspect raw-data/source view, paginate result groups, see citation bubbles on completed answers | `ToolActivityCard.tsx:85-95` (settle-collapse reuse verified); elysia `DisplayPagination.tsx:24-68` (pagination logic — chrome rewrite needed, deps absent); `MarkdownText.tsx:9-15` (rehype seam verified, prop type currently blocks extra plugins — see drift) |
| DISP-04 | Web-safety backend error classes render as typed `system_event` cards showing only safe reasons — `internal/web/errors.go` enum | `web/errors.go:11-30` verified (8 codes + 5 reasons; `sanitize()` chokepoint `:134`); UI-SPEC maps 5 of 8 — planner must map all relevant codes |
| DISP-05 | Source Explorer with Table / Metadata / Configuration views (read-only) | elysia `DataExplorer.tsx` (400 LOC), `DataConfig.tsx` (334), `DataMetadata.tsx` (133) — take the READ subset; backed by the same source-registry as citations |
| SWARM-01 | `swarm_spawn` child report renders as typed `swarm_report` table over `ChildReport` (no inter-agent chat/mailbox theater) | `swarm/report.go:32-41` (`ChildReport` verified) + `:21-23` (`Status*` verified); `ToolActivityCard.tsx:34-39,72-160` (child-row + expand machine reuse verified) |
</phase_requirements>

## Code Anchor Verification (drift audit)

> Every file:line anchor in CONTEXT.md `<code_context>` checked against `HEAD` (working tree clean except `.planning/STATE.md`).

| Anchor (CONTEXT.md) | Status | Notes / Drift |
|---------------------|--------|---------------|
| `translator.go:19` `ArtifactEventName` | ✅ EXACT | `const ArtifactEventName = "aura.display"` is a CONTEXT.md *wording* slip — the constant value is `"aura.artifact"` (`:19`). The line is correct; the prose ("`aura.display`'s twin") describes it as the twin to copy. Add `DisplayEventName = "aura.display"` beside it. |
| `translator.go:115-123` (artifact CUSTOM branch) | ✅ EXACT | `if len(ev.Actions.ArtifactDelta) > 0 { … events.NewCustomEvent(artifactEventName, …) }` at `:115-123`. Copy this block for `aura.display`. |
| `event.go:68-83` (`Actions` struct) | ✅ EXACT | Struct spans `:68-83`. Add `Display *DisplayPayload \`json:"display,omitempty"\``. Mirror the `AwaitingInput *AwaitingInput` omitempty-pointer pattern (`:72`). |
| `web/errors.go:11-30` | ⚠️ DRIFT (more codes than listed) | Anchor resolves. CONTEXT.md D-07 lists 5 codes; the file has **8** (`web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`, `:11-20`) + **5 Reason constants** (`:24-30`). `sanitize()` at `:134`. Planner must decide the full code→label map (UI-SPEC covers only 5). |
| `web/searxng.go:39-45` (`ResultMetadata`) | ✅ EXACT | `ResultMetadata{Engine,Score,Category,PublishedAt *string,Thumbnail}` at `:39-45`. `Result{Title,URL,Snippet,Metadata}` at `:30-35`. |
| `swarm/report.go:32-41` (`ChildReport`) | ✅ EXACT | `ChildReport{GoalIndex,ChildID,Status,Summary,Error,Question,Options,ToolCallID}` at `:32-41`. `Status*` at `:21-23`. |
| `runner_persist.go:118-123` (tool turn persist) | ⚠️ DRIFT (preview, not full bytes) | `AppendTurn{Role:RoleTool, Content:ti.ResultPreview, ToolCallID:…}` at `:118-123`. CONTEXT.md says "full bytes spilled to a `.result` sidecar" — the **conversation** turn carries `ResultPreview` (the preview); the tool's full output sidecar (`tools/result.go`) is a separate artifact not loaded by `LoadHistory`. **D-06 fidelity hinges on this** — see "Resolved: D-06". |
| `agui/server.go:261-288` (`handleMessages`/snapshot) | ✅ EXACT | `handleMessages` at `:261-288`; route `GET /threads/{id}/messages` registered at **`:122`**. The rehydration endpoint EXISTS and works. |
| `agui/server.go:455-494` (`projectMessages`) | ✅ EXACT | `projectMessages` `:455-473`, `projectToolCalls` `:475-494`. Projects `Content: m.Content` (= the persisted `ResultPreview` for tool turns) + `ToolCalls`. |
| `sseAdapter.ts:99-103` (CUSTOM no-op) | ⚠️ DRIFT (stronger than "no-op") | `:98-103` is the `AguiFrame` doc comment listing CUSTOM/`aura.artifact` as "not modelled". CUSTOM is **not in the union and has no `reduceFrame` case** — it is silently dropped, not a no-op handler. Additive extension = add a `CustomFrame` to the union + a `case 'CUSTOM'` in `reduceFrame`. |
| `sseAdapter.ts:269-279` ("snapshot-aware") | ❌ INACCURATE | `:269-279` is the `TOOL_CALL_RESULT` case (it tolerates a result without a prior START, useful for folding snapshot tool turns). There is **NO MESSAGES_SNAPSHOT handling** in the reducer. The reducer folds a *live SSE turn*, not a snapshot. Snapshot→`ThreadMessageLike` rehydration is unbuilt (see D-06 prerequisite). |
| `ExternalStoreChat.tsx:425` (`tools.Fallback`) | ✅ NEAR (off by ~1) | The `tools:` key is at `:426`, `Fallback:` at `:427` (block `:426-435`). The wiring point is correct: branch `part.display ? <DisplayRouter/> : <ToolActivityCard/>`. **`messages:[]` mount is at `:79`** — the rehydration gap. |
| `ToolActivityCard.tsx:85-95` (settle-collapse) | ✅ EXACT | `useEffect` settle-edge auto-collapse at `:91-95`; `useState(status==='running' && hasRaw)` at `:85`. Status maps `:15-31`. Child-row interface `:34-39`; `ToolActivityRow` (reusable row+expand) `:72-160`. |
| `MarkdownText.tsx` (rehype seam) | ⚠️ DRIFT (prop type blocks injection) | Passes `rehypePlugins={[[rehypeSanitize, schema]]}` + `components` (`:14-16`), BUT the public prop type is `Omit<…, 'remarkPlugins'\|'rehypePlugins'>` (`:10`) — callers **cannot** inject extra rehype plugins via props today. To host `rehypeCitations`, the planner must modify `MarkdownText` to accept additional rehype plugins (or add a `CitedMarkdownText` variant). |
| `llm_agent.go:269` (tail-inject copy) | ✅ EXACT | Comment "live volatile hints are tail-injected to a COPY (messages[0] untouched, D-04)" at `:268-274`; the `prompt.Budget{…CurrentTime…}` build at `:275-282` → `a.builder.Build(a.history,…,budget)` at `:287`. |
| `llm_agent.go:303` (AG-031 guard) | ✅ EXACT | `// AG-031: snapshot messages[0] before the hook to detect cache drift` `:303`; `prefixBefore := prefixSnapshot(req.Messages)` `:304`. |

## Standard Stack

### Core (existing — reuse, do not re-add)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `@assistant-ui/react` | 0.14.22 | Runtime + primitives (the chat lane, `MessagePrimitive.Parts`, `tools.Fallback`) | The shipped Phase-25 runtime; displays render inside its part model |
| `@assistant-ui/react-markdown` | 0.14.4 | `MarkdownTextPrimitive` (sanitized markdown host) | The `document`/citation render host (`MarkdownText.tsx`) |
| `rehype-sanitize` | ^6.0.0 | HTML sanitization in the markdown pipeline | Already the HARDEN-08 guard on `MarkdownText` |
| `remark-gfm` | ^4.0.1 | GFM tables/strikethrough in markdown | Already present; `document`/`table` render |
| `@tanstack/react-query` | ^5.101.0 | Server-state cache | The history-rehydration fetch (D-06) should use this (consistent with `useConversations`) |

### Supporting (NET-NEW this phase)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `@radix-ui/react-hover-card` | 1.1.17 (already transitively installed) | Citation hovercard chrome (D-04) | **The actual net-new dep** (NOT a non-existent "assistant-ui hovercard primitive" — see Drift). MIT. Already in `node_modules` via the `radix-ui` meta-package; add as a DIRECT pinned `package.json` dependency rather than relying on a transitive resolution. |
| `shiki` | 4.2.0 | Lazy code highlighter for `code` displays (D-10) | Import the **fine-grained** `shiki/core` + `shiki/engine/javascript` + selective `@shikijs/langs/*` + `@shikijs/themes/*`, loaded via `import()` in a separate Vite chunk so it stays off the critical-path bundle. Emits escaped `<span>`s (tokenize-as-text, never executes — HARDEN-08). |
| `unist-util-visit` | latest (verify) | The `rehypeCitations` plugin walks the hast tree | Already a transitive dep of the rehype ecosystem; verify it resolves before adding direct. Used by the citation rehype plugin (elysia `MarkdownFormat.tsx:11`). |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `@radix-ui/react-hover-card` | Hand-rolled popover on `@floating-ui` (also present) | Radix gives focus-trap/keyboard/`onOpenChange` for free; matches D-16 (hover-is-never-only-path needs focus + tap). Hand-rolling re-implements a11y. Prefer Radix. |
| Shiki fine-grained | `rehype-highlight` (highlight.js) | `rehype-highlight` was the 01-SPEC-deferred option (bundle weight). Shiki fine-grained + code-split reconciles this (D-10) with better grammar fidelity and TextMate tokenization. |
| Zero-dep SVG chart | uPlot (~14KB gz) / recharts (~136KB gz) | D-02 locks zero-dep SVG MVP now; uPlot is the future swap when a numeric-series source lands; recharts is forbidden (bundle). |
| elysia `DisplayPagination` chrome (framer-motion + react-icons + shadcn Select) | Native `<button>`/`<select>` + committed `motion.css` tokens + inline SVG | Those 3 deps are absent and UI-SPEC-forbidden; port the **logic** (`React.Children.toArray` + slice + page state), rewrite the chrome. |

**Installation (frontend):**
```bash
cd web
npm install @radix-ui/react-hover-card@1.1.17 shiki@4.2.0
# verify unist-util-visit resolves transitively before adding direct
```
No backend `go get` is needed — `DisplayPayload`, the normalizer, the image-proxy, and the source-list injection are all standard-library + existing-package Go.

**Version verification:** `@radix-ui/react-hover-card@1.1.17` and `@radix-ui/react-tooltip@1.2.10` confirmed present in `web/node_modules` (pulled by `radix-ui`, an assistant-ui dependency). `shiki` latest is **4.2.0** [CITED: npmjs.com/package/shiki]. No charting lib, no react-icons, no framer-motion installed (confirmed via node_modules survey).

## Package Legitimacy Audit

> slopcheck was not run in this session (offline tooling). Both net-new packages are well-known, MIT, and one (`@radix-ui/react-hover-card`) is already physically present in `node_modules`. Treat as `[ASSUMED]`-clean; the planner should gate the install behind the existing Phase-23 frontend supply-chain CI gate (the `web-lint` job runs `npm ci`; `allowScripts` is an explicit allow-list in `package.json`).

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `@radix-ui/react-hover-card` | npm | mature (Radix UI) | very high | github.com/radix-ui/primitives | not run | Approved — already transitively installed at 1.1.17; pin direct |
| `shiki` | npm | mature (4.2.0) | very high | github.com/shikijs/shiki | not run | Approved — pin exact; load fine-grained + code-split |
| `unist-util-visit` | npm | mature (unifiedjs) | very high | github.com/syntax-tree/unist-util-visit | not run | Approved if direct import needed (likely transitive) |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none
**Postinstall check:** no postinstall scripts expected from these MIT packages; the existing `allowScripts` allow-list (`package.json:72`) governs.

## Resolved Discretion Items (concrete recommendations for the planner to lock)

### R1. `DisplayPayload` Go type union + per-type structs

Mirror the flat `ChildReport`/`WebError` shapes. A single tagged-union struct (NOT an interface) keeps decode(encode)==identity and JSON-omitempty clean, matching the `Actions` field idiom:

```go
// internal/agent/display/payload.go  [ASSUMED — proposed shape]
type Kind string
const (
    KindWebResult     Kind = "web_result"
    KindDocument      Kind = "document"
    KindCode          Kind = "code"
    KindLocalArtifact Kind = "local_artifact"
    KindTable         Kind = "table"
    KindChart         Kind = "chart"
    KindSystemEvent   Kind = "system_event"
    KindSwarmReport   Kind = "swarm_report"
)

type Payload struct {
    Type       Kind      `json:"type"`
    ToolCallID string    `json:"tool_call_id"`        // correlation key for sseAdapter attach
    Title      string    `json:"title,omitempty"`
    WebResults []WebItem `json:"web_results,omitempty"`
    Document   *Document `json:"document,omitempty"`
    Code       *Code     `json:"code,omitempty"`
    Artifact   *Artifact `json:"artifact,omitempty"`
    Table      *Table    `json:"table,omitempty"`
    Chart      *Chart    `json:"chart,omitempty"`
    System     *System   `json:"system,omitempty"`
    Swarm      []swarm.ChildReport `json:"swarm,omitempty"` // reuse the existing type directly
    Sources    []Source  `json:"sources,omitempty"`   // the registry (D-05) — powers hovercard + explorer
}
```

Per-type structs (flat, mirror SearXNG/WebError/ChildReport):
- `WebItem{Title, URL, Snippet, Domain, Score float64, PublishedAt *string, Thumbnail string, RefID string}` — populated from `web.Result` + `ResultMetadata` (searxng.go:30-45).
- `Document{ContentMD string, URL, Title string}` — from `web.Page` (fetcher.go:22-28).
- `Code{Body, Lang string}` — sandbox/shell output.
- `Artifact{Filename string, SizeBytes int64, Path string}` — reuse `ArtifactDelta` plumbing.
- `Table{Columns []string, Rows [][]string}` — structured rows.
- `Chart{XLabels []string, YValues []float64, XAxisLabel string}` — elysia swap-ready shape (D-02).
- `System{Class, Reason, Message string, Severity string}` — from `web.WebError` (errors.go:36-41) + swarm `Status`.
- `Source{RefID, Index int, Type Kind, Title, URL, Snippet string, Confidence float64, Cited bool}` — the registry wire shape (validated ~1:1 against Vercel AI SDK `InlineCitation`, per CONTEXT.md).

### R2. `internal/agent/display/` package layout (≤600 LOC/file — NO GOD CLASS rule)

```
internal/agent/display/
├── doc.go            # package doc + the trust-boundary contract
├── payload.go        # the Payload union + per-type structs (≤200 LOC)
├── normalize.go      # the dispatch: ToolResult/name → Payload (≤150 LOC)
├── web.go            # web_search → web_result, web_fetch → document (≤150 LOC)
├── code.go           # sandbox/shell → code/local_artifact (≤120 LOC)
├── swarm.go          # swarm_spawn ChildReport → swarm_report (≤80 LOC)
├── systemevent.go    # WebError + swarm-status → system_event (≤100 LOC)
├── sources.go        # the URL-keyed source registry + numbered-list renderer (≤200 LOC)
└── *_test.go         # table + golden tests per file
```
Each normalizer is a small function `func(toolResult) (Payload, bool)` (the bool = "I recognized this; the false path is D-FALLBACK). This matches `golang-structs-interfaces` "small interfaces, accept-the-input-shape-return-a-struct".

### R3. Tool→normalizer wiring order (recommended)

`web_search`→`web_result` (the deep-search core; exercises the source registry + citations) → `WebError`→`system_event` (zero new backend, validates the safe-reason path) → `swarm_spawn`→`swarm_report` (zero new backend, reuses ChildReport) → `web_fetch`→`document` → `sandbox/shell`→`code`/`local_artifact` → structured rows→`table` → `chart` (define payload, SVG MVP). Frontend router lands incrementally with each.

### R4. `DisplayRouter.tsx` shape

Mirror elysia `RenderDisplay.tsx:51` `switch(payload.type)` — but the `default:` returns `<ToolActivityCard …/>` (D-FALLBACK), **never `null`** (elysia returns null at `:125` — do NOT copy). One file, one switch, each `case` delegates to a per-type display component.

### R5. Source-registry wire shape

`{refId, index, type, title, url?, snippet, confidence?, cited, object?}` (per CONTEXT.md) — already folded into `Source` above. One registry, two consumers (citation hovercard shows `cited`; Source Explorer Table shows all `consulted`).

### R6. Migration numbering — NONE NEEDED

**Drift correction:** PROJECT.md §Persistence says the latest migration is 0011. The live tree shows migrations through **0019** (`0019_authula_schema.up.sql`). The next free slot is **0020**. But D-06 = re-derive = **ZERO new storage**, so **Phase 26 needs no migration at all**. The only fallback (per D-06) is storing a payload whose data is not in the persisted preview — and the per-type shapes above are all derivable from `ResultPreview` + the tool name, so even that fallback is unnecessary for the in-scope types.

### R7. D-05 source-list tail-inject mechanics (the one architecturally non-trivial wiring)

The volatile numbered source list must reach `prompt.Builder.Build` via the same append-to-copy path the budget uses (`builder.go:91-104`: `buildBase` appends a `RoleUser` message with `budget.block()` to a COPY of history, leaving `messages[0]` byte-identical). The cleanest additive shape: extend the `prompt.Budget` struct (`builder.go:33-39`) with a `Sources string` field and add a line in `Budget.block()` (`:49-64`). The normalizer's source registry (accumulated across `web_search` calls in the turn) is rendered to a numbered `[n] Title — url` block and threaded into the `Budget` constructed at `llm_agent.go:275-282`. **The static convention sentence** ("emit `[n]` next to claims; sources are numbered in the provided list") is safe to add to the **system prompt (`messages[0]`)** because it never varies; the **volatile numbered list** rides the tail-inject. The `cache_invariant_audit.sh` gate (drives `aura cache-audit`, 22-fixture replay hashing `messages[0]`) will catch any mistake that puts the volatile list in `messages[0]`.

### R8. Image-proxy reuse seam (D-09)

`internal/web` already owns the SSRF defense: `guard.validateAndPin` (ssrf.go:85 — hostname blocklist → DNS resolve → classify-every → pin), `hardenedTransport` (transport.go:44 — dial-only-the-pinned-IP + Control re-check + no-auto-redirect), composed in `Client` (client.go:14-37). `Client.Fetch` (fetcher.go:53) is HTML-only (`allowedContentTypes` = text/html, xhtml — fetcher.go:37-40), so it CANNOT serve images. **Reuse seam:** add a new exported method `func (c *Client) FetchImage(ctx, convID, rawURL string) ([]byte, contentType string, error)` that reuses `c.transport.client` + `withConvID` (transport.go:28) but with an **image content-type allowlist** (`image/png`, `image/jpeg`, `image/webp`, `image/gif`, `image/svg+xml`? — exclude SVG to avoid script-in-SVG) + a size cap. The new HTTP endpoint mounts under `/api/` behind `RequireAuth` (Phase 24 gate, inherited by all `/api/` routes per `24-CONTEXT.md`), takes a URL query param, calls `FetchImage`, and streams the bytes back with `Cache-Control` + a strict `Content-Type`. The frontend `<img src="/api/image-proxy?url=…">` carries `referrerpolicy="no-referrer"` + `loading="lazy"`; add a CSP `img-src 'self'`.

### R9. The D-06 history-rehydration fetch (the prerequisite)

On `ExternalStoreChat` mount/threadId-change (when `threadId.length>0`), fetch `GET /threads/{id}/messages` (returns a `MESSAGES_SNAPSHOT` event whose `messages[]` is `projectMessages` output), map each `events.Message` → `ThreadMessageLike` (a new pure mapper, parallel to `sseAdapter`'s live folder), and `setMessages(...)`. Tool turns in the snapshot carry `Content = ResultPreview` + `ToolCalls`; run the SAME `internal/agent/display` normalizer **server-side** at snapshot projection time (`server.go:projectMessages`) so the snapshot already carries the re-derived `DisplayPayload` per tool turn (D-06 "one normalizer for live + replay"). The frontend mapper attaches `part.display` exactly like the live CUSTOM frame does, so the router renders identically on replay.

## Architecture Patterns

### System Architecture Diagram

```
TOOL EXECUTION (web_search / web_fetch / sandbox / swarm_spawn / WebError)
        │  structured result + tool name
        ▼
┌─────────────────────────────────────────────────────────────┐
│  internal/agent/display  (NEW — the trust boundary)          │
│  normalize(toolName, result) → (Payload, recognized bool)    │
│    • assigns URL-keyed source RefIDs (D-05)                  │
│    • builds the source registry (cited vs consulted)         │
└───────────────┬───────────────────────────┬─────────────────┘
   recognized   │                            │  numbered source list
                ▼                            ▼  (volatile)
   Actions.Display = &Payload     prompt.Budget.Sources (tail-inject, copy)
                │                            │  → Builder.Build (messages[0] UNTOUCHED)
                ▼                            ▼
   ┌──────────────────────────┐     [AG-031 KV-drift guard, llm_agent.go:303]
   │ translator.Translate     │
   │  if Actions.Display != nil│
   │   → NewCustomEvent(       │
   │      "aura.display", …)   │     ──────── SSE ────────►  sseAdapter.reduceFrame
   └──────────────────────────┘                              case 'CUSTOM' aura.display
                                                              → attach payload to tool part
                                                                 by toolCallId
                                                                      │
                                                                      ▼
                                              ExternalStoreChat tools.Fallback (:426)
                                                part.display ? <DisplayRouter/> : <ToolActivityCard/>
                                                                      │
                                              ┌───────────────────────┴───────────────┐
                                              │  switch(payload.type)  (DisplayRouter) │
                                              │  web_result │ document │ code │ table  │
                                              │  chart │ local_artifact │ system_event │
                                              │  swarm_report │ default → ToolActivityCard│
                                              └───────────────────────┬───────────────┘
                                                                      │
                                  citation [n] (rehypeCitations) ◄── source registry ──► Source Explorer sheet
                                  (Radix HoverCard, click-through)                       (Table/Metadata/Config, read-only)

REPLAY (reopen thread):
  GET /threads/{id}/messages → handleMessages → LoadHistory → projectMessages
     (re-run the SAME display normalizer per tool turn) → MESSAGES_SNAPSHOT
        → [NEW] frontend snapshot→ThreadMessageLike mapper → setMessages → same DisplayRouter
  ⚠ The frontend fetch is MISSING today — wire it (Wave 0 prerequisite).

  Image-proxy: <img src="/api/image-proxy?url=…"> → RequireAuth → web.Client.FetchImage
     (reuses guard.validateAndPin + hardenedTransport, image content-type allowlist + size cap)
```

### Recommended Project Structure

```
internal/agent/display/      # NEW Go package (R2 layout)
internal/agui/translator.go  # +DisplayEventName, +aura.display CUSTOM branch
internal/agent/event.go      # +Actions.Display *display.Payload (omitempty)
internal/agui/server.go      # projectMessages: re-run normalizer per tool turn (D-06)
internal/web/                # +FetchImage method (image-proxy seam, D-09)
internal/agent/prompt/builder.go  # +Budget.Sources field + block() line (D-05)
web/src/chat/
├── sseAdapter.ts            # +CustomFrame + case 'CUSTOM' aura.display
├── ExternalStoreChat.tsx    # +history-rehydration fetch; tools.Fallback branch
├── displays/                # NEW dir
│   ├── DisplayRouter.tsx     # switch(payload.type), default → ToolActivityCard
│   ├── WebResultDisplay.tsx
│   ├── DocumentDisplay.tsx   # CitedMarkdownText variant
│   ├── CodeDisplay.tsx       # lazy Shiki chunk
│   ├── TableDisplay.tsx
│   ├── ChartDisplay.tsx      # zero-dep SVG + <table> fallback
│   ├── LocalArtifactDisplay.tsx
│   ├── SystemEventCard.tsx
│   ├── SwarmReportTable.tsx
│   ├── CitationBubble.tsx    # Radix HoverCard
│   ├── rehypeCitations.ts    # inline-splice plugin (fixes elysia bug 1)
│   ├── DisplayPagination.tsx # ported logic, rewritten chrome
│   ├── SourceExplorerSheet.tsx
│   └── snapshotToMessages.ts # MESSAGES_SNAPSHOT → ThreadMessageLike (D-06)
```

### Pattern 1: Namespaced CUSTOM event (additive twin)
**What:** A new `Actions.Display` slot + a `Translate` branch + a `sseAdapter` CUSTOM frame, all mirroring the proven `aura.artifact` path.
**When to use:** Emitting the typed payload on the SSE stream without touching the text/tool lifecycle.
**Example:** Copy `translator.go:115-123` (the `if len(ev.Actions.ArtifactDelta) > 0` block) verbatim, swapping `ArtifactDelta`→`Display` and `artifactEventName`→`displayEventName`.

### Pattern 2: Tail-inject volatile data to a copy (KV-cache safe)
**What:** The numbered source list rides `prompt.Budget` → `buildBase`'s append-to-copy (`builder.go:94`), never `messages[0]`.
**When to use:** Any per-turn volatile data the model needs but that must not poison the cached prefix.
**Source:** `internal/agent/prompt/builder.go:91-104` (verified) + `internal/agent/llm_agent.go:269-287` (verified).

### Anti-Patterns to Avoid
- **Router `default: return null`** (elysia `RenderDisplay.tsx:125`) — Aura MUST return the raw `ToolActivityCard` (D-FALLBACK). Output is never lost.
- **End-appending citations** (elysia `processTextWithCitations`, `MarkdownFormat.tsx:114-141`) — the model places `[n]` inline (D-05); feed the model's text directly to `rehypeCitations`. Drop `processTextWithCitations` entirely.
- **`prose-img:hidden`** (elysia `MarkdownFormat.tsx:171`) — drop it; render images (D-04 fix 2).
- **`rehypeHighlight` in the citation pipeline** (elysia `:207`) — skip it; highlighting is the separate lazy Shiki chunk (D-10).
- **Putting the volatile source list in `messages[0]`** — trips the AG-031 guard + the `cache_invariant_audit.sh` CI gate.
- **Client-fetching arbitrary `<img src>`** — leaks browsing/SSRF; route through the image-proxy (D-09).
- **Copying elysia `DisplayPagination` chrome** (framer-motion + react-icons + shadcn Select) — those deps are absent/forbidden; port the logic, rewrite the chrome.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SSRF-safe image fetch | A new `http.Get` in the proxy handler | `web.Client` hardened transport (`FetchImage` reusing `guard.validateAndPin` + `hardenedTransport`) | DNS-rebind/TOCTOU/metadata-IP defenses are subtle and already battle-tested (ssrf.go/transport.go); a fresh fetch re-opens the exact SSRF hole D-09 forbids |
| Citation hovercard a11y (focus/keyboard/tap) | A hand-rolled `div` popover | `@radix-ui/react-hover-card` | D-16 requires hover-is-never-the-only-path (focus + tap); Radix gives focus management + `onOpenChange` + keyboard for free |
| Syntax highlighting | A regex tokenizer | Shiki fine-grained (escaped spans) | Regex tokenizers are fragile and risk XSS; Shiki emits escaped spans (HARDEN-08) and lazy-loads grammars |
| Markdown sanitization for `document` | A new sanitizer | The existing `MarkdownText` + `rehypeSanitize` + `markdownSanitizeSchema` | Already the HARDEN-08 chokepoint; reuse, just add `rehypeCitations` |
| Tool result preview/sidecar plumbing | New persistence for displays | Re-derive over `projectMessages` (D-06) | The preview is already persisted + projected; ZERO new storage |
| Swarm child rows + expand | A new table widget | `ToolActivityRow` (`ToolActivityCard.tsx:72-160`) | The child-row + settle-collapse machine already exists and matches the design system |
| WebError safe-reason mapping | New error classification | The `web/errors.go` enum + `sanitize()` (`:134`) | D-07: zero new backend classification; the chokepoint guarantees no SSRF internals leak |

**Key insight:** Phase 26's hardest engineering is NOT the React (elysia gives the shapes) — it is keeping the new typed-data path inside the established trust + cache boundaries. Every "hand-roll" risk here is a place where rebuilding re-opens a closed security or cache hole.

## Runtime State Inventory

> N/A — Phase 26 is a code + protocol addition (greenfield package + additive event + frontend components). It renames nothing, migrates no stored data, and registers no OS/service state. **Verified:** no rename/refactor of existing keys, collections, env vars, or registered tasks; the working tree is clean (only `.planning/STATE.md`), and D-06 explicitly adds ZERO new storage.

## Common Pitfalls

### Pitfall 1: Assuming the chat lane already rehydrates history
**What goes wrong:** Plans treat display replay as "just emit the payload again" and skip the fetch — then reopened threads render blank (no text, no tools, no displays).
**Why it happens:** The backend endpoint exists (`server.go:122`) so it *looks* wired; CONTEXT.md even (incorrectly) calls the reducer "snapshot-aware".
**How to avoid:** Wire the `GET /threads/{id}/messages` fetch + a snapshot→`ThreadMessageLike` mapper as a Wave-0 task BEFORE any display work depends on replay.
**Warning signs:** `ExternalStoreChat.tsx:79` still `useState([])` with no mount `useEffect`; reopening a thread shows an empty viewport.

### Pitfall 2: Poisoning `messages[0]` with the volatile source list
**What goes wrong:** The numbered source list is added to the system prompt; the KV-cache prefix changes every turn; cache hit-rate collapses and `cache_invariant_audit.sh` fails CI.
**Why it happens:** It's tempting to put "the sources" near the citation convention sentence in `messages[0]`.
**How to avoid:** Static convention sentence → `messages[0]`; volatile numbered list → `prompt.Budget.Sources` tail-inject (append-to-copy, `builder.go:94`).
**Warning signs:** `aura cache-audit` prints "messages[0] mutated at request N".

### Pitfall 3: Rendering untrusted output as markdown/HTML
**What goes wrong:** A tool without a normalizer (or a malformed payload) gets routed to a rich renderer; prompt-injection or XSS leaks (HARDEN-08 violation).
**Why it happens:** The router's `default:` is set to `null` or to a markdown renderer instead of the escaped raw card.
**How to avoid:** Router `default:` → `<ToolActivityCard/>` (escaped `<pre>`); typed rich rendering ONLY for a trusted-normalizer payload.
**Warning signs:** A display renders markdown for an unrecognized type; `ToolActivityCard.test.tsx`-style XSS assertion absent for the new components.

### Pitfall 4: D-06 re-derive fidelity mismatch (preview vs full bytes)
**What goes wrong:** The live normalizer reads the full tool result; the replay normalizer reads only the persisted `ResultPreview` → different `DisplayPayload`s → replay looks different from live.
**Why it happens:** `runner_persist.go:121` persists `ResultPreview`, not the full bytes; the full tool sidecar is a separate artifact not loaded by `LoadHistory`.
**How to avoid:** Design the normalizer to consume the SAME value in both paths — the tool's result preview shape (the value that becomes both `ResultPreview` and the SSE `TOOL_CALL_RESULT`). Then live and replay are byte-identical by construction.
**Warning signs:** A golden test that feeds the full result to the normalizer in a unit test but the preview at replay.

### Pitfall 5: `MarkdownText` cannot host the citation plugin via props
**What goes wrong:** The plan assumes `MarkdownText` accepts `rehypePlugins` from the caller; it does not (`Omit<…,'rehypePlugins'>`, `:10`).
**How to avoid:** Modify `MarkdownText` to merge an internal extra-plugins array, or add a `CitedMarkdownText` variant that composes `MarkdownTextPrimitive` with `[rehypeSanitize, rehypeCitations]`.
**Warning signs:** TypeScript error on passing `rehypePlugins`; citations silently not rendering.

## Code Examples

### Additive `Actions.Display` slot
```go
// internal/agent/event.go (add inside Actions struct, mirroring AwaitingInput :72)
// Source: verified against event.go:68-83
Display *display.Payload `json:"display,omitempty"` // typed display (Phase 26); nil → omitted
```

### `aura.display` CUSTOM branch in Translate
```go
// internal/agui/translator.go (add beside the artifact branch :115-123)
// Source: verified pattern from translator.go:109-123
const DisplayEventName = "aura.display"
// ...inside the for-range over the agent stream:
if ev.Actions.Display != nil {
    if !closeRuns() { return }
    if !yield(events.NewCustomEvent(DisplayEventName, events.WithValue(ev.Actions.Display)), nil) {
        return
    }
    continue
}
```

### Reducer CUSTOM frame (sseAdapter.ts)
```ts
// Source: extension of sseAdapter.ts:104-118 (add to AguiFrame union + reduceFrame)
interface CustomFrame { readonly type: 'CUSTOM'; readonly name: string; readonly value: unknown; }
// in reduceFrame switch:
case 'CUSTOM': {
  if (frame.name === 'aura.display') {
    const p = frame.value as { tool_call_id?: string };
    if (typeof p.tool_call_id === 'string') {
      const part = ensureTool(state, p.tool_call_id, state.tools.get(p.tool_call_id)?.toolName ?? '');
      state.tools.set(p.tool_call_id, { ...part, display: frame.value as DisplayPayload });
    }
  }
  return state;
}
```

### rehypeCitations inline splice (fix elysia bug 1)
```ts
// Source: ported from elysia MarkdownFormat.tsx:39-111 — KEEP the inline-splice walker,
// DROP processTextWithCitations (:114-141). The model's text already carries inline [n].
// visit(tree,'text',...) → /\[(\d+)\]/g → splice <span data-ref-id> at the match position.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Opaque tool output as escaped `<pre>` (D-02, Phase 25) | Typed `DisplayPayload` + `switch(payload.type)` router | Phase 26 | Inspectable, paginated, source-viewable evidence |
| `rehype-highlight` (deferred for bundle, 01-SPEC) | Shiki 4.2.0 fine-grained, code-split lazy chunk | Phase 26 (D-10) | Reconciles the deferral; better grammar + tokenize-as-text |
| elysia end-appended citations | Hybrid code-registry + model-placed inline `[n]` | Phase 26 (D-05) | Perplexity-grade citations; model can't hallucinate a number |
| shadcn `HoverCard` (elysia) | `@radix-ui/react-hover-card` (Aura grain) | Phase 26 | No shadcn in Aura; Radix already present |

**Deprecated/outdated:**
- CONTEXT.md "assistant-ui hovercard primitive": no such export exists in `@assistant-ui/react@0.14.22` — use `@radix-ui/react-hover-card`.
- PROJECT.md §Persistence "migrations 0001-0011": stale; live tree is at 0019. (No migration needed regardless.)

## Validation Architecture

> nyquist_validation = true in `.planning/config.json` → this section is REQUIRED. Frontend gates: Vitest ≥85% coverage + Stryker ≥70% mutation (blocking CI per project policy). Backend gates: ≥85% owned-surface coverage + the `cache_invariant_audit.sh` CI gate.

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + table/golden tests; `-race`; mutation via `go-mutesting` (WSL) |
| Go quick run | `go test ./internal/agent/display/ ./internal/agui/ ./internal/web/` |
| Go full | `make quality-full` (vet+build+lint+race+vuln+coverage; stack up) |
| Frontend framework | Vitest 4.1.9 + @testing-library/react 16; Stryker 9.6.1 mutation; Playwright e2e |
| Frontend config | `web/vitest` (via `package.json` scripts), `web/stryker` |
| Frontend quick run | `cd web && npm run test` (vitest run --coverage) |
| Frontend full | `npm run test && npm run mutation && npm run lint && npm run typecheck` |
| Cache invariant gate | `bash scripts/cache_invariant_audit.sh` (drives `aura cache-audit`, 22-fixture `messages[0]` hash) |
| Contrast gate | `cd web && npm run contrast` (`scripts/contrast-check.mjs`, 15/15 AA pairs) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DISP-01 | Normalizer maps each tool result → correct `Payload` shape | unit/golden (Go) | `go test ./internal/agent/display/ -run TestNormalize` | ❌ Wave 0 |
| DISP-01 | `Actions.Display` decode(encode)==identity | unit (Go) | `go test ./internal/agent/ -run TestActionsDisplayRoundTrip` | ❌ Wave 0 |
| DISP-01 | `Translate` emits `aura.display` CUSTOM beside artifact (golden) | golden (Go) | `go test ./internal/agui/ -run TestTranslate` (extend golden-events.json) | ✅ extend |
| DISP-01 | `messages[0]` byte-stable with source-list tail-inject | CI gate | `bash scripts/cache_invariant_audit.sh` | ✅ extend fixtures |
| DISP-01/05 | Volatile source list NOT in `messages[0]`; rides Budget copy | unit (Go) | `go test ./internal/agent/prompt/ -run TestBudgetSources` | ❌ Wave 0 |
| DISP-02 | `DisplayRouter` renders each type; `default:`→raw card (never null) | unit (Vitest) | `cd web && npm run test -- DisplayRouter` | ❌ Wave 0 |
| DISP-02 | Reducer attaches `aura.display` payload to tool part by `toolCallId` | unit (Vitest) | `npm run test -- sseAdapter` | ✅ extend |
| DISP-02/HARDEN-08 | Unknown-type/malformed payload renders escaped, never markdown | unit (Vitest, XSS assertion) | `npm run test -- DisplayRouter.xss` | ❌ Wave 0 |
| DISP-03 | Pagination "X–Y of N" + prev/next; default 3/page | unit (Vitest) | `npm run test -- DisplayPagination` | ❌ Wave 0 |
| DISP-03 | Citation chip → hovercard (focus + tap, not hover-only) | unit (Vitest) + a11y | `npm run test -- CitationBubble`; `npm run lint` (jsx-a11y) | ❌ Wave 0 |
| DISP-03 | `rehypeCitations` splices inline at the `[n]` claim position | unit (Vitest) | `npm run test -- rehypeCitations` | ❌ Wave 0 |
| DISP-04 | Each `web/errors.go` code → safe label; no SSRF internals | unit (Vitest) + Go enum coverage | `npm run test -- SystemEventCard`; `go test ./internal/web/ -run TestSanitize` | ❌ Wave 0 / ✅ Go exists |
| DISP-05 | Source Explorer Table sort/search/paginate; Metadata/Config read-only | unit (Vitest) | `npm run test -- SourceExplorer` | ❌ Wave 0 |
| DISP-05/D-09 | Image-proxy reuses SSRF guard; blocks private/metadata targets | integration (Go, `web_integration`) | `go test -tags web_integration ./internal/web/ -run TestFetchImage` | ❌ Wave 0 |
| SWARM-01 | `swarm_report` table over `[]ChildReport`; row expand shows summary/error/question | unit (Vitest) | `npm run test -- SwarmReportTable` | ❌ Wave 0 |
| D-06 | Reopened thread re-derives + renders displays identically to live | integration (Go snapshot) + Vitest | `go test ./internal/agui/ -run TestProjectMessagesDisplay`; `npm run test -- snapshotToMessages` | ❌ Wave 0 |
| D-06 | `GET /threads/{id}/messages` fetched on reopen | e2e (Playwright) | `npm run test:e2e -- replay` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** the package's quick run (`go test ./internal/agent/display/` or `npm run test -- <component>`) + `go vet`/`go build` (Go) or `npm run lint`/`typecheck` (frontend).
- **Per wave merge:** full Go `make quality` + `cd web && npm run test && npm run mutation`; `bash scripts/cache_invariant_audit.sh`; `npm run contrast`.
- **Phase gate:** `make quality-full` green (≥85% owned-surface) + Vitest ≥85% + Stryker ≥70% killed + cache-invariant gate green + Playwright replay e2e green, before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/agent/display/*_test.go` — normalizer table/golden tests (DISP-01/02/04, SWARM-01)
- [ ] `internal/agent/prompt/budget_sources_test.go` — source-list tail-inject + messages[0] stability (DISP-01/05)
- [ ] `internal/agui/golden-events.json` — extend with an `aura.display` fixture (DISP-01)
- [ ] `scripts/cache_invariant_audit.sh` fixtures — add a turn that emits a source list (DISP-01)
- [ ] `internal/web/fetcher_image_test.go` — `FetchImage` SSRF integration (D-09)
- [ ] `web/src/chat/displays/__tests__/*` — DisplayRouter, CitationBubble, rehypeCitations, DisplayPagination, SystemEventCard, SwarmReportTable, SourceExplorer, snapshotToMessages (DISP-02..05, SWARM-01, D-06)
- [ ] `web/src/chat/__tests__/sseAdapter.test.ts` — extend with CUSTOM/aura.display frame
- [ ] Playwright `replay` spec — reopen-thread rehydration (D-06)
- [ ] Stryker config — ensure the new `web/src/chat/displays/` dir is in the mutation scope

## Security Domain

> `security_enforcement` not set to false → included. Phase 26 introduces NO destructive actions (Source Explorer read-only, D-03).

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture / trust boundary | yes | Only a trusted Go normalizer produces a rich-renderable payload (HARDEN-08); router `default:` → escaped raw card |
| V5 Input Validation / output encoding | yes | `rehype-sanitize` for `document`; React-escaped `<pre>` for raw; Shiki escaped spans for `code`; image content-type allowlist + size cap |
| V12 Files / SSRF | yes | Image-proxy reuses `web` SSRF allowlist + DNS-pin + dial-only-pinned-IP (ssrf.go/transport.go); no client-side arbitrary `<img src>` |
| V7 Error handling / info leakage | yes | `system_event` renders only the `web/errors.go` `sanitize()`-classified reason; no SSRF internals, no raw internal URLs |
| V4 Access Control | yes (inherited) | Every new `/api/` route (image-proxy, any read endpoint) behind the Phase-24 `RequireAuth` whole-origin gate |
| V6 Cryptography | no | No new crypto |

### Known Threat Patterns for {Go backend + React cockpit}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Prompt-injection laundered via rich render of untrusted output | Tampering/Elevation | HARDEN-08: typed render only for trusted-normalizer payload; default → escaped text |
| Client-side SSRF / tracking via external `<img>` | Information Disclosure | Backend image-proxy reusing `guard.validateAndPin`; `referrerpolicy="no-referrer"`; CSP `img-src 'self'` |
| XSS via code-display HTML | Tampering | Shiki tokenizes-as-text (escaped spans), never executes |
| SSRF internals leak in error cards | Information Disclosure | `web/errors.go` `sanitize()` chokepoint (`:134`); render `reason` enum, not free-form text |
| KV-cache prefix poisoning via citation data | DoS (cost) | Volatile source list tail-injected to a copy; AG-031 guard + `cache_invariant_audit.sh` |
| Model citing a hallucinated source number | Spoofing | Code-owned `[n]→source` registry; model can only reference numbers in the provided list (D-05) |
| Image-proxy as an open SSRF relay | Elevation | `RequireAuth` gate + SSRF guard + image-only content-type allowlist + size cap + no redirect auto-follow |

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Node.js | Frontend build/test | ✓ (CI Node 24) | >=24.16.0 <25 | — |
| `@radix-ui/react-hover-card` | Citation hovercard | ✓ (transitive) | 1.1.17 | Hand-roll on `@floating-ui` (present) — worse a11y |
| `shiki` | Code highlighting | ✗ (to install) | 4.2.0 | Plain mono `<pre>` (no highlight) — degrades gracefully |
| `web.Client` SSRF transport | Image-proxy | ✓ (in-repo) | — | `referrerpolicy=no-referrer` + lazy + CSP (D-09 minimum) |
| `GET /threads/{id}/messages` | History replay | ✓ (in-repo, `server.go:122`) | — | — (endpoint exists; only the frontend fetch is missing) |
| `aura cache-audit` subcommand | Cache invariant gate | ✓ (in-repo) | — | — |
| `go-mutesting` (WSL) | Mutation spot-check | ✓ (WSL toolchain) | — | Stryker covers frontend; Go mutation per CLAUDE.md |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** `shiki` (fallback = plain mono, no highlighting — acceptable but degrades D-10).

## Project Constraints (from CLAUDE.md)

- **NO GOD CLASS >600 LOC** — the `internal/agent/display/` layout (R2) and the `web/src/chat/displays/` split keep every file well under 600.
- **Deferred-tool pattern** — not directly relevant (no new tools); the normalizer is invoked at the runner/translator seam, not as an LLM-visible tool.
- **HARDEN-08 untrusted-output-as-text** — router `default:` → escaped raw card; rich render only for trusted-normalizer payloads.
- **`messages[0]` KV-cache invariant** — source list tail-injected to a copy; `cache_invariant_audit.sh` gate must stay green.
- **Coverage floor 85%** (owned-surface, full matrix) — applies to `internal/agent/display/` and the new web components (Vitest ≥85%).
- **Mutation ≥70% killed** — Go (`go-mutesting`, critical normalizer files) + frontend (Stryker).
- **English-only prompts; output IT via directive** — the citation-convention system-prompt line is English; user-facing copy is en+it via `t()`.
- **Minimal-industrial-shape** — find the MINIMAL form (zero-dep SVG charts, re-derive not new storage, reuse `ToolActivityRow`/`MarkdownText`/`web.Client`); flag over-engineering. The `MergeDisplays` tab strip is an optional stretch — the single-display path works without it.
- **No comments unless WHY is non-obvious; refactor-on-touch; one slice = one commit; never git push unless asked.**

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The accurate net-new hovercard dep is `@radix-ui/react-hover-card` (1.1.17, already transitive), NOT an "assistant-ui hovercard primitive" | Standard Stack | LOW — verified no hovercard export in `@assistant-ui/react@0.14.22`; Radix package physically present. Planner should still confirm the import surface (meta-package `radix-ui` HoverCard namespace vs the standalone package). |
| A2 | Shiki 4.2.0 fine-grained core + code-split keeps the embedded `web/dist` critical-path bundle from bloating | Standard Stack / D-10 | MEDIUM — Shiki grammars are large; if the fine-grained import isn't done carefully the chunk could still be heavy. Mitigation: lazy `import()`, restrict langs/themes, verify chunk size in the dist-freshness check. |
| A3 | The proposed `DisplayPayload` Go union (tagged struct, not interface) is the right shape | Resolved R1 | LOW — mirrors `ChildReport`/`WebError`/`Actions` idiom; planner may refine field names. |
| A4 | D-06 re-derive at `projectMessages` consuming `ResultPreview` yields live/replay parity | Resolved R9 / Pitfall 4 | MEDIUM — depends on the normalizer being designed to consume the preview shape in BOTH paths. If a normalizer needs full bytes (e.g. a huge table), that specific type falls to the D-06 store-fallback. Planner should confirm per-type preview sufficiency. |
| A5 | The image content-type allowlist should EXCLUDE `image/svg+xml` (script-in-SVG risk) | Resolved R8 | LOW — defensive default; planner confirms whether SearXNG thumbnails are ever SVG (rarely). |
| A6 | `unist-util-visit` resolves transitively (no direct add needed) | Standard Stack | LOW — it's a core unified dep; verify at install time. |
| A7 | The static citation-convention sentence is safe in `messages[0]` (truly invariant) | Resolved R7 | LOW — only if the sentence never templates per-turn data. Keep it a fixed string. |

## Open Questions (RESOLVED)

1. **`@assistant-ui/react` hovercard import surface**
   - What we know: assistant-ui exports no hovercard primitive; `@radix-ui/react-hover-card@1.1.17` and the `radix-ui` meta-package are installed.
   - What's unclear: whether to import from `@radix-ui/react-hover-card` (pin direct) or `radix-ui`'s `HoverCard` namespace.
   - Recommendation: add `@radix-ui/react-hover-card` as a direct pinned dep; do not rely on the transitive resolution.

2. **Per-type preview sufficiency for D-06 re-derive**
   - What we know: the persisted tool turn carries `ResultPreview`, not full bytes.
   - What's unclear: whether every in-scope display type (esp. large `table`) is fully re-derivable from the preview.
   - Recommendation: the planner audits each tool's `ResultPreview` content; for any type whose preview is lossy, use the D-06 store-fallback for that type only.

3. **`MarkdownText` plugin-injection refactor vs variant**
   - What we know: the public prop type blocks caller-supplied rehype plugins (`:10`).
   - What's unclear: refactor `MarkdownText` to merge extra plugins vs add a `CitedMarkdownText`.
   - Recommendation: prefer the small refactor (one internal merge) to avoid two markdown components drifting; covered by the existing markdown tests.

## Sources

### Primary (HIGH confidence — verified against the live tree at HEAD)
- `internal/agui/translator.go` (artifact CUSTOM twin :19/:115-123; Translate :47)
- `internal/agent/event.go` (Actions struct :68-83)
- `internal/agent/prompt/builder.go` (Budget :33-39, buildBase append-to-copy :91-104)
- `internal/agent/llm_agent.go` (tail-inject :268-287, AG-031 guard :303-304)
- `internal/web/errors.go` (enum :11-30, sanitize :134), `searxng.go` (ResultMetadata :39-45), `ssrf.go` (guard :85), `transport.go` (hardenedTransport :44), `client.go` (Client :14-37), `fetcher.go` (Fetch HTML-only :53)
- `internal/swarm/report.go` (ChildReport :32-41, Status :21-23)
- `internal/runner/runner_persist.go` (tool turn :118-123), `internal/conversations/store_helpers.go` (sidecar rehydrate :57-110)
- `internal/agui/server.go` (handleMessages :261-288, route :122, projectMessages :455-494)
- `web/src/chat/sseAdapter.ts` (AguiFrame :104-118, reduceFrame :245-311, TOOL_CALL_RESULT :269-279)
- `web/src/chat/ExternalStoreChat.tsx` (messages:[] :79, tools.Fallback :426-435)
- `web/src/chat/ToolActivityCard.tsx` (status :15-31, settle-collapse :85-95, child-row :34-39/72-160), `MarkdownText.tsx` (rehype seam :9-16, Omit prop :10)
- `web/package.json` (deps), `node_modules` survey (radix-hover-card 1.1.17, no shiki/react-icons/framer)
- `scripts/cache_invariant_audit.sh` (the DISP-01 CI gate), `.github/workflows/ci.yml` (web gates :823-877), `.planning/config.json` (nyquist_validation:true)
- `internal/db/migrations/` (latest 0019 — next free 0020; none needed)
- `D:/tmp/elysia-frontend`: `RenderDisplay.tsx:51` (router, default null — override), `CitationBubble.tsx:25-56` (shadcn HoverCard), `MarkdownFormat.tsx:39-141` (rehypeCitations + the 2 bugs), `DisplayPagination.tsx:24-68` (logic), `DataExplorer/DataConfig/DataMetadata` (read subset)

### Secondary (MEDIUM confidence)
- Shiki latest version + fine-grained/lazy guidance [CITED: shiki.style/guide/best-performance, npmjs.com/package/shiki]

### Tertiary (LOW confidence)
- None — every load-bearing claim is grounded in the live tree or the curated reference.

## Metadata

**Confidence breakdown:**
- Code anchor verification: HIGH — every anchor read against HEAD; drifts documented with exact line numbers.
- Standard stack: HIGH — deps confirmed in node_modules; Shiki version cited.
- Architecture / resolutions: HIGH — grounded in verified seams; the union/layout are proposals (A3) but follow established idioms.
- D-06 fidelity: MEDIUM — depends on per-type preview sufficiency (A4, OQ2).
- Pitfalls / Validation: HIGH — derived directly from verified gates + the trust boundary.

**Research date:** 2026-06-18
**Valid until:** 2026-07-18 (stable in-repo surface; re-verify the web `node_modules` deps if `package.json` changes before planning)
