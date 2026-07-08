---
phase: 37A-web-artifact-delivery-lane
plan: 04
subsystem: web
tags: [react, sse, reducer, download, xss, info-disclosure, i18n, accessibility, webui-dist, vite]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery-lane
    plan: 02
    provides: "the enriched aura.artifact SSE descriptor (asset_id/tool_call_id/filename/size_bytes/mime_type), emitting tool_call_id UNCONDITIONALLY on ingest-success AND on the D-02/D-05 degrade so the reducer can attach a card in both cases"
  - phase: 37A-web-artifact-delivery-lane
    plan: 03
    provides: "GET /api/assets/{id}/download — the authenticated, forced-attachment, owner-scoped route the download button targets"
provides:
  - "web/src/chat/sseAdapter.ts — the aura.artifact CUSTOM-frame reducer branch + isArtifactDescriptor guard: synthesizes a fresh local_artifact DisplayPayload correlated by tool_call_id and NEVER copies the descriptor's raw path (either branch); was previously dropped as a no-op"
  - "web/src/chat/displays/LocalArtifactDisplay.tsx — an authenticated same-origin download button (<a href=/api/assets/{asset_id}/download download={filename}>) when asset_id is present; a render-only filename + size + 'delivery unavailable' note when absent; no path chip in either branch"
  - "internal/webui/dist — the embedded web bundle rebuilt from the src change (web-dist-freshness clean)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Synthesize-don't-thread (Landmine 7): send_file never flows through normalizeCode's aura.display local_artifact — a fresh local_artifact payload is built in the sseAdapter aura.artifact branch and attached by tool_call_id, NOT threaded into aura.display"
    - "No-path-to-browser (D-13): the reducer omits path from the synthesized payload via conditional spreads (only filename/size_bytes/asset_id/mime_type), and the card renders no artifact.path in either branch — the browser never receives a raw host/container path for any authenticated session"
    - "Degrade-reachable card: the reducer attaches a card whenever the descriptor carries tool_call_id; the asset_id-absent 'delivery unavailable' branch is the realistic authenticated-but-ingest-failed (D-02/D-05) case"

key-files:
  created: []
  modified:
    - web/src/chat/displays/types.ts
    - web/src/chat/sseAdapter.ts
    - web/src/chat/sseAdapter_frames.ts
    - web/src/chat/__tests__/sseAdapter.test.ts
    - web/src/chat/displays/LocalArtifactDisplay.tsx
    - web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx
    - web/src/i18n/resources.display.ts
    - internal/webui/dist

key-decisions:
  - "Synthesize a fresh local_artifact payload in the aura.artifact branch (Landmine 7); do NOT thread asset_id into aura.display, which only fires for code-producing tools via normalizeCode (display/code.go) — send_file never reaches it"
  - "The synthesized display payload NEVER carries path (D-01/D-13): conditional spreads add only size_bytes/asset_id/mime_type when present; path is a backend/Telegram-only descriptor field the reducer deliberately never reads"
  - "The asset_id-absent branch is a render-only 'delivery unavailable' card (D-13 tightened per operator decision), NOT the CLI/no-identity host-path carve-out — every SSE frame is post-RequireAuth, so the degrade is always authenticated-but-ingest-failed (D-02 Put/asset-service error or D-05 empty thread)"
  - "Reuse the existing local_artifact display type + DisplayRouter route (D-13); no new display type (Elysia unknown-type-null footgun) and no backend descriptor/event/translator change (Telegram parity D-01 unchanged)"

patterns-established:
  - "aura.artifact reducer branch: guard by isArtifactDescriptor (string tool_call_id + string filename) → ensureTool(tool_call_id) → writeTool with a path-free synthesized local_artifact display"
  - "Rewrite-don't-delete for an intentionally-changed contract (Landmine 5): the :383 'aura.artifact is a no-op' test encoded the OLD drop behavior — rewritten to the attach + no-path contract (CLAUDE.md exception, committed with justification); the aura.display non-payload no-op test kept intact"

requirements-completed: [WEBART-04]

coverage:
  - id: A1
    description: "an aura.artifact frame carrying tool_call_id+filename+asset_id attaches a local_artifact display to that tool part; the synthesized artifact has asset_id and NO path"
    requirement: "WEBART-04"
    verification:
      - kind: unit
        ref: "web/src/chat/__tests__/sseAdapter.test.ts::aura.artifact (asset_id present) attaches a local_artifact card by tool_call_id, no path"
        status: pass
    human_judgment: false
  - id: A2
    description: "a degraded aura.artifact frame (no asset_id, descriptor carries path) still attaches a card whose artifact has filename+size_bytes but NO path and NO asset_id (reducer drops the raw path)"
    requirement: "WEBART-04"
    verification:
      - kind: unit
        ref: "web/src/chat/__tests__/sseAdapter.test.ts::aura.artifact degrade (no asset_id, descriptor has path) → render-only card, no path, no asset_id"
        status: pass
    human_judgment: false
  - id: A3
    description: "LocalArtifactDisplay renders <a href=/api/assets/{asset_id}/download download={filename}> when asset_id is present, and no raw host/container path appears"
    requirement: "WEBART-04"
    verification:
      - kind: unit
        ref: "web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx::renders an authenticated same-origin download anchor when asset_id is present (+ never renders a raw path, asset_id present)"
        status: pass
    human_judgment: false
  - id: A4
    description: "when asset_id is absent the card degrades to filename + size + a 'delivery unavailable' note with no <a download> and no raw host/container path in either branch"
    requirement: "WEBART-04"
    verification:
      - kind: unit
        ref: "web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx::degrades to filename + size + 'delivery unavailable' when asset_id absent (+ never renders a raw path on the degraded card)"
        status: pass
    human_judgment: false
  - id: A5
    description: "internal/webui/dist rebuilt from the src changes and committed atomically; a fresh rebuild produces no further diff (web-dist-freshness clean); package.json/package-lock.json byte-unchanged"
    requirement: "WEBART-04"
    verification:
      - kind: build
        ref: "cd web && npm run build → git status internal/webui/dist == clean; go build ./... green (embed)"
        status: pass
    human_judgment: false

# Metrics
duration: ~27min (executor, cut off pre-SUMMARY; orchestrator closed out after independent verification)
completed: 2026-07-08
status: complete
---

# Phase 37A Plan 04: Web Artifact Consume + Authenticated Download Button Summary

**The user-visible payoff (WEBART-04): the web chat now consumes the `aura.artifact` SSE descriptor it used to drop — `sseAdapter` synthesizes a path-free `local_artifact` payload correlated by `tool_call_id`, and `LocalArtifactDisplay` renders an accessible same-origin download button targeting 37A-03's `GET /api/assets/{id}/download` (asset_id present) or a render-only "delivery unavailable" card (degraded) — the browser never receives a raw host/container path for any authenticated session. Frontend-only; the embedded `internal/webui/dist` is rebuilt + committed; no Go behavior change.**

## Performance

- **Duration:** ~27 min (3 atomic task commits)
- **Completed:** 2026-07-08
- **Tasks:** 3
- **Files modified:** 8 (7 `web/src` + `internal/webui/dist`) · **Files created:** 0

## Accomplishments
- **Task 1 (`691c8c43`, feat):** `web/src/chat/displays/types.ts` `DisplayArtifact` gained `asset_id?` + `mime_type?`. `sseAdapter.ts` added the `ArtifactDescriptor` shape (`tool_call_id`/`filename` required; `size_bytes?`/`asset_id?`/`mime_type?` optional; NO `path` in the shape — the reducer never reads it) + the `isArtifactDescriptor` guard, and an `aura.artifact` branch inside the existing CUSTOM case that synthesizes a `local_artifact` `DisplayPayload` via conditional spreads (only `filename`/`size_bytes`/`asset_id`/`mime_type`) and attaches it by `tool_call_id` (`ensureTool`→`writeTool`). The `aura.display` branch is untouched. `sseAdapter_frames.ts`'s stale "aura.artifact … not modelled" doc-comment updated. The `:383` no-op test was **rewritten** (Landmine 5, CLAUDE.md test-change exception in the commit message) to assert the attach + no-path contract (both the asset_id-present and the degraded path-carrying descriptor), and a new "without tool_call_id → ignored" test was added; the `aura.display` non-payload no-op test was kept intact.
- **Task 2 (`2f273f92`, feat):** `LocalArtifactDisplay.tsx` branches on `artifact.asset_id`: present → an accessible `<a href={`/api/assets/${assetId}/download`} download={filename}>` with an `aria-label`, a distinctive accented download-icon control (border-accent/hover/focus-visible ring — CLAUDE.md Frontend_aesthetics), the file icon, filename + size, and NO path chip; absent → a `role="note"` "delivery unavailable" warning (triangle icon) + filename + size, NO path chip. The header doc-comment now documents both branches + the no-path guarantee. `resources.display.ts` gained `display.artifact.download`, `downloadAria`, and `deliveryUnavailable` in BOTH locales (en: "Download"/"Download {{filename}}"/"Delivery unavailable"; it: "Scarica"/"Scarica {{filename}}"/"Consegna non disponibile"). `LocalArtifactDisplay.test.tsx` extended: anchor+download when asset_id set, degraded card when absent, and no-raw-path assertions in BOTH branches (even when a path is injected onto the artifact).
- **Task 3 (`35a2eb0e`, chore):** `internal/webui/dist` rebuilt via `npm run build` (vite `outDir: ../internal/webui/dist`) from the Task 1/2 src changes and committed atomically; the full web suite is green.

## Task Commits

Each task committed atomically on `master` (sequential-on-main mode — see Deviations):

1. **Task 1: sseAdapter aura.artifact branch + isArtifactDescriptor + DisplayArtifact fields + rewritten no-op test** — `691c8c43` (feat)
2. **Task 2: LocalArtifactDisplay download button + degraded card + i18n (en/it) + card tests** — `2f273f92` (feat)
3. **Task 3: rebuild embedded internal/webui/dist** — `35a2eb0e` (chore)

## Files Modified
- `web/src/chat/displays/types.ts` — `DisplayArtifact.asset_id?` + `mime_type?`.
- `web/src/chat/sseAdapter.ts` — `ArtifactDescriptor` + `isArtifactDescriptor` + the `aura.artifact` reducer branch (path-free synthesis).
- `web/src/chat/sseAdapter_frames.ts` — stale doc-comment truthed up (aura.artifact is now consumed).
- `web/src/chat/__tests__/sseAdapter.test.ts` — rewritten no-op test → attach + no-path; new no-tool_call_id-ignored test.
- `web/src/chat/displays/LocalArtifactDisplay.tsx` — download-button / degraded-card branches, no path chip (117 LOC, ≤600).
- `web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` — both branches + no-raw-path assertions.
- `web/src/i18n/resources.display.ts` — download / downloadAria / deliveryUnavailable in en + it.
- `internal/webui/dist` — rebuilt embedded bundle (hashed asset renames).

## Decisions Made
- **Synthesize, don't thread (Landmine 7).** `aura.display`'s `local_artifact` is produced only by `normalizeCode` for code-producing tools; `send_file` never flows through it. A fresh `local_artifact` payload is built in the `aura.artifact` branch and attached by `tool_call_id`.
- **Path never reaches the browser (D-13 tightened).** The reducer's conditional spreads exclude `path`; the card renders no `artifact.path` in either branch. Verified by a reducer assertion (payload has no `path`) + card assertions (no raw path string in either branch).
- **Degrade is authenticated-but-ingest-failed, not the CLI carve-out.** Every SSE frame is post-RequireAuth; 37A-02 emits `tool_call_id` on the degrade, so the reducer attaches a card and the "delivery unavailable" branch is reachable for the realistic D-02/D-05 case — surfacing filename + size, never a path.
- **No backend / no new display type.** Reused `local_artifact` + `DisplayRouter`'s existing route; the descriptor/event/translator are unchanged (Telegram parity D-01), and no new type risks the Elysia unknown-type-null footgun.

## Deviations from Plan

### Execution-mode deviation (orchestrator decision)
- **Ran in SEQUENTIAL mode on the main working tree, not an isolated worktree.** The orchestrator chose main-tree execution for this single-plan frontend wave: worktree isolation gives no parallelism benefit here, and a fresh worktree lacks `web/node_modules` — forcing a slow, network-fragile `npm ci`. The main tree already had `node_modules` (494 entries) + node v24.18.0 / npm 11.16.0, so the build reused them. Commits landed directly on `master`; there was no worktree merge-back. The GSD workflow explicitly supports this (`USE_WORKTREES_FOR_PLAN=false`).

### SUMMARY authorship (transparency)
- **The executor subagent was terminated by a provider session limit immediately BEFORE writing this SUMMARY** (its last recovered line: "Now update STATE.md…"). All three code tasks were already committed (`691c8c43`/`2f273f92`/`35a2eb0e`), and the tracking edits to `REQUIREMENTS.md`/`ROADMAP.md` were staged-uncommitted. The orchestrator closed the plan out per the safe-resume "close out manually" path: it did NOT re-dispatch (which would duplicate the committed work), independently re-ran every acceptance gate (below), authored this SUMMARY from the committed diffs + verified evidence, and committed the tracking. No functional deviation from the plan's `must_haves` was found.

No deviations from the plan's `must_haves`, prohibitions, or threat-model mitigations.

## Issues Encountered
- **Executor session-limit cutoff** (documented above) — recovered without re-dispatch; code work intact and independently verified.

## Verification (independently re-run by the orchestrator post-cutoff, on this Windows host)
- `cd web && npx tsc --noEmit` — clean.
- `cd web && npm run lint` (eslint `--max-warnings=0`) — clean.
- `cd web && npm test` (vitest run --coverage) — **127 files / 1051 tests passed, 0 failed.** The two changed files clear the ≥85% floor decisively: `sseAdapter.ts` 97.4% stmts / 90.14% branches, `src/chat/displays` 97.98% stmts. Global branches 85.71% ≥ the 85% web floor. (The `src/chat` *directory* aggregate 83.67% reflects pre-existing debt in untouched files — this plan only added coverage.)
- **web-dist-freshness:** `npm run build` then `git status internal/webui/dist` — **clean** (committed dist matches a fresh build of the committed src; deterministic no-diff rebuild).
- `go build ./...` — green (the embedded `//go:embed internal/webui/dist` compiles).
- `web/package.json` + `web/package-lock.json` — **byte-unchanged** (zero new deps, threat T-37A-04-SC honored).

## Known Stubs
None — the reducer + card are fully wired to 37A-02's descriptor contract and 37A-03's download route.

## User Setup Required
None — same-origin, inherits the existing `RequireAuth` gate; no new env or config.

## Manual-Only (deferred to /gsd-verify-work — cannot run in jsdom/vitest)
- Real-browser download UX: a live turn where the agent `send_file`s a DOCX → the button appears → clicking streams the file with the correct name + `attachment` disposition, and the browser never received a raw host/container path (full stack: serve + Garage + a real turn).
- Telegram artifact still delivered on the live Bot API after the descriptor enrichment (cross-channel non-regression).

## Self-Check: PASSED
- Files: all 7 `web/src` files + `internal/webui/dist` modified + `37A-04-SUMMARY.md` present.
- Commits: `691c8c43` (feat), `2f273f92` (feat), `35a2eb0e` (chore) — all on `master`.
- Gates: tsc + eslint clean; 1051 web tests pass; changed-file coverage 97%+ (≥85%); dist fresh (no-diff rebuild); `go build ./...` green; package.json/lock byte-unchanged.
- WEBART-04 delivered: `aura.artifact` consumed, authenticated download button + degraded card, no path to the browser, dist rebuilt.

---
*Phase: 37A-web-artifact-delivery-lane*
*Completed: 2026-07-08*
