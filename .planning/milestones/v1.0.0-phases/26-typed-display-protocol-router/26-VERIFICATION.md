---
phase: 26-typed-display-protocol-router
verified: 2026-06-18T23:25:00Z
status: passed
closed: 2026-06-19T06:40:00Z
closure: "The 4 human_needed live items were cleared via a Playwright E2E matrix (desktop Chrome + Pixel 5 + iPhone 13; 51 tests, 4 consecutive green runs) instead of manual sign-off — citation hovercard+click-through (displays.spec), Source Explorer read-only posture (asserts no write controls), swarm no-mailbox (asserts no composer/textbox in card), and the D-06 Playwright replay (replayText===liveText, live). The mobile pass also surfaced + fixed a CRITICAL iOS/iPadOS-Safari render crash (CSS asset-hash desync, commit 26ce045a) that the original desktop-only verification could not catch."
score: 6/6 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Visually inspect citation hovercard on a live web_search turn"
    expected: "Clicking/focusing a [n] chip opens a hovercard showing type-icon + title + snippet preview; clicking the chip body navigates into the read-only Source Explorer sheet focused to that source"
    why_human: "Visual/UX fidelity of the Radix hovercard and the Source Explorer focus-to-refId cannot be asserted by automated DOM tests under jsdom; requires an aura serve session with an actual web_search result"
  - test: "Confirm Source Explorer read-only posture (no PATCH/destructive controls)"
    expected: "Opening Source Explorer via 'Sources (N)' button and via citation click-through shows Table/Metadata/Configuration views with NO Re-Analyze, Clear, Save, Edit, Apply controls — read-only this milestone"
    why_human: "The NEGATIVE assertion (absence of future governance-write controls) is asserted in automated tests, but the live UX judgment that the operator accepts the read-only posture (D-03 deliberate deferral to Phase 29) needs sign-off"
  - test: "Confirm swarm_report table has no inter-agent chat / mailbox theater"
    expected: "Running a swarm_spawn shows a compact ChildReport summary table with row-expand (goal / worker / status / summary); NO mailbox, NO inter-agent chat UI, NO per-child feed"
    why_human: "Negative visual assertion — absence of disallowed UI patterns requires a live swarm_spawn run, not a mocked unit test"
  - test: "D-06 replay e2e live sign-off"
    expected: "Playwright replay.spec.ts run against a live aura serve returns PASS for all 8 tests including the replayText === liveText D-06 parity assertion"
    why_human: "The SUMMARY documents a successful live Playwright run against 127.0.0.1:9091; the verifier cannot independently re-run it without a running stack. The spec is present and structurally correct (anti-skip-as-green guard is code-verified); live pass requires human re-run on the provisioned stack"
---

# Phase 26: Typed-Display Protocol + Router Verification Report

**Phase Goal:** Deliver the typed-display protocol end-to-end: backend emits a namespaced `aura.display` AG-UI CUSTOM event carrying a typed `DisplayPayload` produced by a Go normalizer (`internal/agent/display/`) via an additive `Actions.Display` slot, and the cockpit renders it through a `switch(payload.type)` display router — turning opaque tool output into inspectable, paginated, source-viewable typed evidence.

**Requirements:** DISP-01, DISP-02, DISP-03, DISP-04, DISP-05, SWARM-01

**Verified:** 2026-06-18T23:25:00Z
**Status:** HUMAN_NEEDED (all automated checks pass; 4 live UX/behavioral items need human sign-off)
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Backend emits `aura.display` CUSTOM event with typed `DisplayPayload` from a Go normalizer, preserving `messages[0]` KV-cache invariant | VERIFIED | `internal/agent/display/` package: 9 source files, 9 test files; `Actions.Display *display.Payload` slot on `event.go:80`; `DisplayEventName` + branch in `translator.go:26,138-146`; golden fixture `CUSTOM_DISPLAY` in `testdata/golden-events.json`; `TestNormalize` passes; `TestTranslate` passes; `TestActionsDisplayRoundTrip` passes |
| 2 | Cockpit renders all 8 typed display types via `switch(payload.type)` router with raw-card default (never null) | VERIFIED | `web/src/chat/displays/DisplayRouter.tsx`: all 8 `case` branches (`table`, `chart`, `system_event`, `swarm_report`, `local_artifact`, `document`, `web_result`, `code`) present above a `default:` that returns escaped `ToolActivityCard`; 13/13 DisplayRouter tests pass |
| 3 | Operator can inspect displays, paginate result groups, and see citation bubbles on completed answers | VERIFIED | `DisplayPagination.tsx` (X-Y of N, prev/next, default 3/page); `rehypeCitations.ts` (inline-splice at `[n]` position, unknown `[n]` stays literal); `CitationBubble.tsx` (Radix hovercard, focus+tap accessible, `onOpenSource` callback); all cited tests pass |
| 4 | Web-safety backend errors render as typed `system_event` cards showing only safe reasons (no SSRF internals) | VERIFIED | `SystemEventCard.tsx` maps all 8 `web/errors.go` codes to safe i18n labels; test at `SystemEventCard.test.tsx:53` asserts `169.254.169.254` absent from DOM; `NEVER renders system.message free text`; 14/14 tests pass |
| 5 | Operator can use a read-only Source Explorer (Table/Metadata/Configuration, no PATCH/destructive control) | VERIFIED | `SourceExplorerSheet.tsx` (496 LOC), `SourcesButton.tsx`, `SourceExplorerContext.tsx`; negative test at `SourceExplorerSheet.test.tsx:173` asserts absence of Re-Analyze/Clear/Save/Edit/Apply; focus trap + Esc + aria-modal; 40/40 sheet+button tests pass |
| 6 | A `swarm_spawn` child report renders as a typed `swarm_report` table over `ChildReport` (no inter-agent chat / mailbox theater) | VERIFIED | `SwarmReportTable.tsx` renders a `<table>` over `payload.swarm` (ChildReport); status labels come from enum only; negative test asserts NO mailbox/chat affordance; row expand shows summary + error + question/options; 22/22 swarm tests pass |

**Score:** 6/6 truths verified (all automated gates green)

---

### Per-Requirement Coverage

| Requirement | Description | Status | Evidence |
|-------------|-------------|--------|----------|
| DISP-01 | Backend emits `aura.display` CUSTOM event; `Actions.Display` omitempty slot; normalizer maps tool results → typed `Payload`; `messages[0]` cache invariant preserved | COVERED | `display/payload.go`, `display/normalize.go`, `event.go:80`, `translator.go:138-146`; `go test ./internal/agent/display/ ./internal/agent/ ./internal/agui/` all pass; cache-invariant turn-08 fixture (web_search→cite) present at `scripts/fixtures/cache_invariant/turn-08.json`; static citation convention in `prompt.go:106` (messages[0]); volatile source list in `Budget.Sources` via tail-inject (`llm_agent.go:229`, `builder.go:71`) |
| DISP-02 | Cockpit renders typed displays via `switch(payload.type)` router | COVERED | `DisplayRouter.tsx` all 8 cases + default raw-card; `sseAdapter.ts:203-211` CUSTOM/`aura.display` attach-by-toolCallId; `ExternalStoreChat.tsx` `ToolFallback` reads `.display` and branches to `DisplayRouter`; Vitest 502/502 pass |
| DISP-03 | Inspect display / source view; paginate result groups; citation bubbles | COVERED | `DisplayPagination.tsx`; `rehypeCitations.ts` inline-splice; `CitationBubble.tsx` Radix hovercard; `DocumentDisplay.tsx` + `WebResultDisplay.tsx` forward `onOpenSource`; all 502 tests pass |
| DISP-04 | Web-safety errors → typed `system_event` cards (safe reasons, no SSRF internals) | COVERED | `SystemEventCard.tsx` maps all 8 `web/errors.go` codes; leaky-message test asserts no `169.254.169.254` in DOM; `go test ./internal/agent/display/ -run TestNormalizeSystemEvent` passes |
| DISP-05 | Source Explorer with Table/Metadata/Configuration views | COVERED | `SourceExplorerSheet.tsx`, `SourcesButton.tsx`, `SourceExplorerContext.tsx`, `sourceExplorerData.ts`, `answerSources.ts`; read-only registry backed by the same URL-keyed `display.Registry` the citations use; two openers (button + citation chip) over ONE sheet via context |
| SWARM-01 | `swarm_spawn` child report → typed `swarm_report` table (no mailbox theater) | COVERED | `SwarmReportTable.tsx`; `display/swarm.go` normalizer; `display.ChildReport` local mirror (wire-identical to `swarm.ChildReport`, cycle-safe); status-from-enum-only enforced in test |

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agent/display/` (9 source files) | Go normalizer package | VERIFIED | All 9 files exist: `doc.go`, `payload.go`, `normalize.go`, `web.go`, `code.go`, `swarm.go`, `systemevent.go`, `sources.go`, `preview.go`; 98.2% statement coverage |
| `internal/agent/event.go` | `Actions.Display *display.Payload` slot | VERIFIED | Line 80; omitempty pointer, decode(encode)==identity tested |
| `internal/agui/translator.go` | `DisplayEventName` + `aura.display` CUSTOM branch | VERIFIED | Lines 26, 138-146; beside `aura.artifact` branch |
| `internal/web/fetcher_image.go` | SSRF-safe `FetchImage` | VERIFIED | Reuses `hardenedTransport` + `validateAndPin`; SVG excluded; no redirect follow; `TestFetchImage` passes |
| `internal/agui/image_proxy.go` | `GET /api/image-proxy` behind `RequireAuth` | VERIFIED | `handleImageProxy`; mounted on agui Server.Mux; wired via `SetImageProxy(web.NewClient)` at boot (`cmd/aura/serve_webui.go`) |
| `internal/agui/server_display.go` | `projectDisplaySnapshot` re-derive at `projectMessages` | VERIFIED | `rederiveDisplays` uses `display.NormalizeToolPreview`; `TestProjectMessagesDisplay` passes |
| `internal/agent/display/preview.go` | Shared `NormalizeToolPreview` (live+replay parity) | VERIFIED | Single decode+normalize seam used by both `llm_agent_display.go` (live) and `server_display.go` (replay) |
| `web/src/chat/displays/types.ts` | TypeScript wire mirror of Go `display.Payload` | VERIFIED | Field-for-field mirror; `isDisplayPayload()` guard; 128 LOC |
| `web/src/chat/displays/DisplayRouter.tsx` | `switch(payload.type)` with all 8 cases + default | VERIFIED | All 8 cases verified by `grep`; default returns escaped `ToolActivityCard` (never null) |
| `web/src/chat/displays/DisplayPagination.tsx` | In-card pagination X-Y of N, prev/next, default 3/page | VERIFIED | Ported elysia logic; aria-labels; 44px targets; polite live region |
| `web/src/chat/displays/snapshotToMessages.ts` | Display-aware `MESSAGES_SNAPSHOT` → `ThreadMessageLike` | VERIFIED | D-06 replay home; delegates to `snapshotToThreadMessages` |
| `web/src/chat/displays/TableDisplay.tsx` | Sort/filter/Copy-TSV/Export-CSV + row pagination | VERIFIED | Native `<table>`, `aria-sort`, RFC-4180 CSV; `tableData.ts` helper 92.6% mutation |
| `web/src/chat/displays/ChartDisplay.tsx` | Zero-dep CSS bars + accessible `<table>` fallback | VERIFIED | No charting library (test asserts absence of recharts/uplot/chart.js/d3 imports) |
| `web/src/chat/displays/SystemEventCard.tsx` | All 8 web/errors.go codes → safe labels | VERIFIED | Full 8-code map; unknown code → generic safe fallback; no `system.message` free text |
| `web/src/chat/displays/SwarmReportTable.tsx` | ChildReport table + row expand (status-from-enum-only) | VERIFIED | `swarmRow.ts` helper 100% mutation; no mailbox theater asserted |
| `web/src/chat/displays/LocalArtifactDisplay.tsx` | Filename + byte size + mono path chip | VERIFIED | Render-only (no download endpoint) — planned scope, not a stub |
| `web/src/chat/displays/rehypeCitations.ts` | Inline-splice citation plugin; unknown `[n]` stays literal | VERIFIED | 85.0% mutation; splices at claim position (not end-appended); registry-backed only |
| `web/src/chat/displays/CitationBubble.tsx` | Radix hovercard; focus+tap accessible; `onOpenSource` | VERIFIED | Controlled HoverCard; click ALSO fires `onOpenSource`; hover is never the only path |
| `web/src/chat/displays/DocumentDisplay.tsx` | Sanitized markdown + inline citations + images + Copy | VERIFIED | Uses shared `markdownConfig.tsx` pipeline; rehype-sanitize always last |
| `web/src/chat/displays/WebResultDisplay.tsx` | Rich web_result card with image-proxy thumbnails | VERIFIED | All thumbnails via `/api/image-proxy?url=…`; no raw external `<img src="http…">` (asserted); referrerpolicy=no-referrer + lazy |
| `web/src/chat/displays/CodeDisplay.tsx` + `shiki.ts` | Lazy escaped-span Shiki highlighter (code-split) | VERIFIED | `<script>` body renders as escaped text (asserted); Shiki chunk `shiki-BM9IzjFr.js` present in `internal/webui/dist/assets/`; 915 kB separate code-split chunk |
| `web/src/chat/displays/SourceExplorerSheet.tsx` | Read-only Table/Metadata/Configuration; focus trap + Esc | VERIFIED | 496 LOC; aria-modal; role="dialog"; negative test asserts no write controls |
| `web/src/chat/displays/SourcesButton.tsx` | "Sources (N)" button; hidden when N=0 | VERIFIED | Accent color; aria-label; 100% mutation |
| `web/src/chat/displays/SourceExplorerContext.tsx` | Shared sheet context (one registry, two openers) | VERIFIED | `SourceExplorerProvider` + `useSourceExplorer`; avoids prop-drilling across assistant-ui boundary |
| `web/e2e/replay.spec.ts` | Playwright D-06 replay spec | VERIFIED (structurally) | Present at `web/e2e/` (correct testDir); anti-skip-as-green guard throws under CI when neither live serve nor golden fixture present; golden fixture `CUSTOM_DISPLAY` present in `testdata/golden-events.json`; LIVE PASS requires human re-run |
| `internal/webui/dist` | Rebuilt embedded bundle with shiki code-split chunk | VERIFIED | `ls internal/webui/dist/assets/` shows `shiki-BM9IzjFr.js`; 11 asset files including the Shiki chunk separate from `index-*.js` |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| Tool result event | `Actions.Display` | `display.NormalizeWithRegistry` in `llm_agent_display.go` | VERIFIED | `deriveDisplay` sets `Actions.Display` on the live tool-result event |
| `Actions.Display` | `aura.display` CUSTOM event on SSE | `Translate()` in `translator.go:138-146` | VERIFIED | Beside `aura.artifact` branch; nil Display falls through |
| SSE `CUSTOM/aura.display` | `ToolPart.display` in store | `sseAdapter.ts:203-211` | VERIFIED | Attaches by `toolCallId`; unknown CUSTOM name is no-op |
| `ToolPart.display` | `<DisplayRouter>` render | `ExternalStoreChat.tsx` `ToolFallback` | VERIFIED | `part.display ? <DisplayRouter/> : <ToolActivityCard/>` via `useAuiState(s => s.part)` |
| Thumbnail URL | `/api/image-proxy` | `WebResultDisplay.tsx:19,23` `proxiedSrc()` | VERIFIED | Every thumbnail routed through proxy; no raw external `<img src>` (test asserts this) |
| `/api/image-proxy` | `FetchImage` SSRF guard | `image_proxy.go:47` + `cmd/aura/serve_webui.go` SetImageProxy | VERIFIED | Route mounted on agui Server.Mux; `SetImageProxy(web.NewClient)` wired at boot |
| Persisted tool turn | `DisplayPayload` on replay | `server_display.go` `rederiveDisplays` via `display.NormalizeToolPreview` | VERIFIED | Same normalizer as live; one registry per thread; `TestProjectMessagesDisplay` passes |
| Citation `[n]` marker | `CitationBubble` chip | `rehypeCitations.ts` inline-splice + `markdownConfig.tsx` `span` renderer | VERIFIED | Inline positional splice; unknown `[n]` stays literal |
| `CitationBubble` click | `SourceExplorerSheet` open | `onOpenSource` callback threaded `DisplayRouter` → `ToolFallback` → context | VERIFIED | `SourceExplorerProvider` + `useSourceExplorer`; one sheet, two openers |
| Volatile source list | `prompt.Budget.Sources` tail-inject | `llm_agent.go:229` + `builder.go:71` | VERIFIED | `RenderSourceList()` threaded to `Budget.Sources`; NOT `messages[0]`; static citation convention in system prompt |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `SystemEventCard.tsx` | `payload.system` | `display/systemevent.go:normalizeWebError` | Yes — maps `*web.WebError` from real SSRF guard | FLOWING |
| `SwarmReportTable.tsx` | `payload.swarm` | `display/swarm.go:normalizeSwarm` from `[]display.ChildReport` | Yes — mirrors `swarm.ChildReport` wire-identical | FLOWING |
| `WebResultDisplay.tsx` | `payload.web_results` | `display/web.go:normalizeWebSearch` from `[]web.Result` | Yes — from SearXNG real results | FLOWING |
| `DocumentDisplay.tsx` | `payload.document` | `display/web.go:normalizeWebFetch` from `web.Page` | Yes — from real web_fetch result | FLOWING |
| `CodeDisplay.tsx` | `payload.code` | `display/code.go:normalizeCode` from `CodeInput` | Yes — from real shell/sandbox output | FLOWING |
| `SourceExplorerSheet` | `sources` | `display.Registry` URL-keyed | Yes — populated by normalizer from real web results | FLOWING |
| `DisplayRouter.tsx` (on replay) | `part.display` | `snapshotToMessages.ts` → `projectDisplaySnapshot` → `NormalizeToolPreview` | Yes — re-derived from persisted `ResultPreview` | FLOWING |

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Go build clean | `go build ./...` | Exit 0, no output | PASS |
| Go vet clean on phase packages | `go vet ./internal/agent/display/ ./internal/agui/ ./internal/web/` | Exit 0, no output | PASS |
| Display normalizer tests | `go test -count=1 ./internal/agent/display/` | ok 0.214s, 98.2% coverage | PASS |
| Agent event round-trip test | `go test -count=1 -run TestActionsDisplayRoundTrip ./internal/agent/` | ok | PASS |
| AG-UI translator test | `go test -count=1 -run TestTranslate ./internal/agui/` | ok | PASS |
| Budget sources tail-inject test | `go test -count=1 -run TestBudgetSources ./internal/agent/prompt/` | ok | PASS |
| FetchImage SSRF test | `go test -count=1 -run TestFetchImage ./internal/web/` | ok | PASS |
| Image proxy test | `go test -count=1 -run TestImageProxy ./internal/agui/` | ok | PASS |
| Replay re-derive test | `go test -count=1 -run TestProjectMessagesDisplay ./internal/agui/` | ok | PASS |
| Race clean (phase packages) | `go test -count=1 -race ./internal/agent/display/ ./internal/agui/ ./internal/web/ ./internal/agent/prompt/` | ok all | PASS |
| TypeScript typecheck | `npm run typecheck` | Exit 0, no output | PASS |
| ESLint zero-warning | `npm run lint` | Exit 0, no output | PASS |
| Vitest full suite | `npx vitest run` | 502/502 pass (53 test files) | PASS |
| Vitest coverage | `npx vitest run --coverage` | 94.36% stmts / 86.36% branch / 95.73% funcs / 96.22% lines (all ≥ 85%) | PASS |
| Contrast gate | `npm run contrast` | 15/15 AA pairs pass | PASS |
| DisplayRouter all 8 cases | `grep "case '(table\|chart\|..."` | 8 cases confirmed + preserved `default:` raw card | PASS |
| Shiki code-split chunk in dist | `ls internal/webui/dist/assets/ \| grep shiki` | `shiki-BM9IzjFr.js` present | PASS |
| No raw external `<img>` in WebResultDisplay | `WebResultDisplay.test.tsx:56` | Test asserts absence; 10/10 pass | PASS |
| `<script>` body escaped in CodeDisplay | `CodeDisplay.test.tsx:45` | `&#x3C;script...` not executed; 9/9 pass | PASS |
| No SSRF internals in SystemEventCard | `SystemEventCard.test.tsx:53` | `169.254.169.254` absent from DOM; 14/14 pass | PASS |
| No write controls in SourceExplorer | `SourceExplorerSheet.test.tsx:173` | Re-Analyze/Clear/Save/Edit/Apply absent; 40/40 pass | PASS |
| Stryker scope covers displays/ | `stryker.config.json` | `src/chat/displays/**/*.{ts,tsx}` excl. `__tests__`, `types.ts`; break threshold 70 | PASS (config verified; last run score reported as 71.74% in SUMMARY — not re-run by verifier due to runtime cost) |

---

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/cache_invariant_audit.sh` | Requires `aura` binary + running stack (`aura cache-audit`) | NOT RUN by verifier (needs stack up + WSL) | SKIP — requires WSL + running DB stack; fixture turn-08 (web_search→cite) is present and structurally correct; reported green in 26-03 SUMMARY |
| `web/e2e/replay.spec.ts` | `npx playwright test replay` (needs `aura serve` + provisioned stack) | NOT RUN by verifier (needs live stack) | SKIP — requires live `aura serve`; spec is structurally correct, anti-skip-as-green guard is code-verified; reported green in 26-06 SUMMARY against 127.0.0.1:9091 |

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| DISP-01 | 26-01, 26-03 | Backend typed-display event + normalizer + cache invariant | SATISFIED | `display/` package; `Actions.Display`; translator branch; Budget.Sources tail-inject; golden fixture + TestActionsDisplayRoundTrip + TestTranslate all green |
| DISP-02 | 26-02, 26-04, 26-05 | Cockpit DisplayRouter with all 8 typed display types | SATISFIED | `DisplayRouter.tsx` all 8 cases; `sseAdapter` CUSTOM attach; ExternalStoreChat ToolFallback branch; 502/502 Vitest |
| DISP-03 | 26-02, 26-04, 26-05 | Inspect/paginate/cite displays | SATISFIED | `DisplayPagination`, `rehypeCitations`, `CitationBubble`; evidence cards (WebResultDisplay, DocumentDisplay); all tests green |
| DISP-04 | 26-01, 26-04 | Web-safety errors → safe `system_event` cards | SATISFIED | `SystemEventCard` maps all 8 codes; leaky-message absence asserted in tests |
| DISP-05 | 26-01, 26-03, 26-06 | Source Explorer (Table/Metadata/Configuration, read-only) | SATISFIED | `SourceExplorerSheet`, `SourcesButton`, `SourceExplorerContext`; `display.Registry` URL-keyed; no write controls asserted |
| SWARM-01 | 26-01, 26-04 | `swarm_report` typed table over ChildReport (no mailbox theater) | SATISFIED | `SwarmReportTable`; `display/swarm.go`; status-from-enum-only; no mailbox asserted |

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `web/src/__tests__/AppShell.conversation.test.tsx` | 143 | Test flakes under parallel coverage CPU load (times out at 10s when coverage-instrumented) | WARNING | Passes in isolation (5/5); fails 1/5 under `npx vitest run --coverage` parallel run; de-flake commit `dbdef3a8` raised `asyncUtilTimeout` + per-test timeout, but coverage load still triggers it on some runs. Does NOT indicate a production bug. |

No `TBD`, `FIXME`, or `XXX` markers found in any Phase 26 source file. No `return null` display paths (the `default:` returns escaped raw card). No `dangerouslySetInnerHTML`. No raw external `<img src="http…">` in display components. No charting library imports (asserted by test).

---

### Locked-Decision Compliance (D-01 through D-16)

| Decision | Claim | Status | Evidence |
|----------|-------|--------|----------|
| D-PROTO | `aura.display` additive twin of `aura.artifact`; `DisplayEventName`; `Actions.Display` omitempty slot | VERIFIED | `translator.go:26,138-146`; `event.go:80` |
| D-FALLBACK | Unknown type → raw `ToolActivityCard` (never null); HARDEN-08 preserved | VERIFIED | `DisplayRouter.tsx` default case; XSS test in DisplayRouter.test.tsx |
| D-01 | Inline placement + click-to-expand temporary full-view | VERIFIED | `ExternalStoreChat.tsx` ToolFallback replaces card in-place; `SourceExplorerSheet` fullscreen sheet (expand pattern); no docked panel |
| D-02 | Chart = zero-dep SVG/CSS bars + accessible table fallback; no charting library | VERIFIED | `ChartDisplay.tsx`; test asserts no recharts/uplot/chart.js/d3 imports |
| D-03 | Source Explorer = READ-ONLY this milestone | VERIFIED | Negative test asserts absence of write controls; governance-write deferred to Phase 29 |
| D-04 | Citations = hovercard (focus+tap) + click-through to Source Explorer; inline-splice (not end-append) | VERIFIED | `rehypeCitations.ts` splices inline; `CitationBubble` controlled HoverCard; tap fires `onOpenSource` |
| D-05 | Citation source-of-truth = URL-keyed registry + model places `[n]`; volatile list NOT in messages[0] | VERIFIED | `display.Registry`; `Budget.Sources` tail-inject; static convention in `prompt.go:106` (messages[0]) |
| D-06 | Replay = re-derive from persisted tool result via same normalizer (ZERO new storage) | VERIFIED | `display.NormalizeToolPreview` shared seam; `rederiveDisplays` in `server_display.go`; `TestProjectMessagesDisplay` |
| D-07 | `system_event` scope = `WebError` enum + swarm Status ONLY; zero new backend classification | VERIFIED | `normalizeWebError` + `normalizeSwarmStatus`; 8 mapped codes |
| D-08 | `swarm_report` = summary table + row expand; ZERO new backend (reuses `[]ChildReport`) | VERIFIED | `SwarmReportTable.tsx`; `display.ChildReport` local mirror |
| D-09 | `web_result` thumbnails via backend image-proxy (SSRF allowlist reused); no raw external `<img>` | VERIFIED | `FetchImage` reuses `validateAndPin`; SVG excluded; WebResultDisplay `proxiedSrc()`; test asserts no raw external img |
| D-10 | Code = lazy Shiki (escaped spans, never executes) + code-split chunk | VERIFIED | `shiki.ts` dynamic import; `shiki-BM9IzjFr.js` in dist; `<script>` escape asserted in test |
| D-13 | Source Explorer access = citation click-through + "Sources (N)" button; one shared sheet | VERIFIED | `SourceExplorerProvider` context; `onOpenSource` threaded through DisplayRouter → ToolFallback → context |
| D-14 | Table = sort + filter + copy/CSV; client-side only | VERIFIED | `TableDisplay.tsx`; `tableData.ts` pure helpers; no backend call |
| D-15 | Loading = progressive swap (raw card first, typed display on payload arrival) | VERIFIED | `ToolFallback` reads `part.display` post-CUSTOM event; no type-guessing during stream |
| D-16 | a11y + i18n: WCAG AA; keyboard/tap for citation popovers; en+it bundles | VERIFIED | 15/15 contrast pairs; `CitationBubble` focus+tap; `resources.display.ts` 368 LOC with en+it bundles |

---

### Human Verification Required

#### 1. Citation Hovercard Visual Fidelity (DISP-03, D-04)

**Test:** Run `aura serve`, ask a `web_search` question, observe the rendered answer. Hover and focus each `[n]` citation chip. Click the chip.
**Expected:** A hovercard opens showing the type-icon + title + snippet preview for that source. Clicking the chip body opens the Source Explorer sheet focused to that source's Metadata pane.
**Why human:** Visual fidelity of the Radix hovercard + the `focusRefId` behavior in the Source Explorer cannot be asserted by jsdom DOM tests. The automated test verifies `onOpenSource` is called with the correct refId; only a live session confirms the premium UX meets the operator bar.

#### 2. Source Explorer Read-Only Posture Live Acceptance (DISP-05, D-03)

**Test:** Open Source Explorer via "Sources (N)" button. Navigate Table / Metadata / Configuration tabs.
**Expected:** All three panes are view-only. No Re-Analyze, Clear, Save, Edit, or Apply control is present. The Table allows sort/search/paginate; Metadata shows ref/type/url/confidence/snippet fields as text; Configuration shows cited/consulted counts as text. The incomplete-source warning banner appears when any source is incomplete.
**Why human:** The NEGATIVE assertion (absence of future governance-write controls) is machine-verified in unit tests. The operator's sign-off that the read-only posture is acceptable this milestone (with Phase 29 as the write surface) is a UX judgment.

#### 3. Swarm Report No-Mailbox Assertion (SWARM-01, D-08)

**Test:** Invoke a `swarm_spawn` tool. Observe the rendered display.
**Expected:** A compact table shows one row per child (goal-index / worker-id / status dot / summary). Clicking a row expands inline to show summary + error (+ question/options for `needs_user_input`). No mailbox UI, no inter-agent chat feed, no per-child SSE stream shown.
**Why human:** The negative behavioral assertion (absence of mailbox theater) requires a live swarm run; the automated unit test mocks the payload.

#### 4. D-06 Replay E2E Live Pass (DISP-01, D-06)

**Test:** Provision the stack (`make db-up + migrate`), build the `aura` binary with the committed dist, run `AURA_E2E_ORIGIN=http://127.0.0.1:9091 npx playwright test replay` from `web/`.
**Expected:** All 8 replay spec tests pass, including the `replayText === liveText` D-06 parity assertion proving a reopened thread re-renders the typed display identically to live.
**Why human:** The Playwright spec requires a live `aura serve` + Postgres stack. The verifier confirmed the spec is structurally correct (present at `web/e2e/replay.spec.ts`, anti-skip-as-green guard throws if neither live nor golden fixture is available, golden fixture `CUSTOM_DISPLAY` is present). The 26-06 SUMMARY documents a successful live run against 127.0.0.1:9091; the verifier cannot independently re-execute without the stack.

---

### Gaps Summary

No blocking gaps found. All 6 requirements (DISP-01 through DISP-05, SWARM-01) are implemented with substantive, wired, data-flowing artifacts. All automated gates are green (Go build + vet + test -race; Vitest 502/502; TypeScript typecheck; ESLint zero-warning; coverage ≥85% every metric; contrast 15/15 AA). Security threat mitigations (T-26-12 SystemEventCard no-SSRF-internals, T-26-15 WebResultDisplay no-raw-external-img, T-26-16 CodeDisplay escaped-spans, T-26-17 MarkdownText sanitize-last, T-26-18 rehypeCitations registry-backed-only, T-26-19 SourceExplorerSheet read-only) are all proven by targeted automated tests.

The one pre-existing flaky test (`AppShell.conversation` — times out under coverage CPU load) is a known cosmetic issue; it passes in isolation and is unrelated to Phase 26 functionality.

Status is `human_needed` (not `passed`) because 4 live UX/behavioral items require operator sign-off: citation hovercard visual fidelity, Source Explorer read-only posture acceptance, swarm report no-mailbox assertion, and the D-06 Playwright replay e2e live run. All 4 are standard "needs live stack + eyes" checks, not evidence of defects.

---

_Verified: 2026-06-18T23:25:00Z_
_Verifier: Claude (gsd-verifier)_
_Method: Goal-backward, evidence-based; code read at file:line; Go and Vitest tests re-run independently_
