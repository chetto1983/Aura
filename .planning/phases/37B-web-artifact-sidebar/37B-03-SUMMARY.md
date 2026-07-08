---
phase: 37B-web-artifact-sidebar
plan: 03
subsystem: ui
tags: [webart, artifact-sidebar, previewKind, mime-gate, svg-xss, lucide, i18n, stryker, vitest, react-query, formatSize]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery-lane
    provides: "GET /api/assets/{id}/download (identity-scoped, attachment/octet-stream) + the aura.artifact descriptor (asset_id/filename/size_bytes/mime_type) that the panel/preview surfaces route on"
  - phase: 37B-web-artifact-sidebar (plan 01)
    provides: "PRD-amendment #78 — the Artefatti sidebar architectural record (D-07 renderer set, D-16 label/icon, T-37B-05 SVG-download gate)"
provides:
  - "artifactMeta.ts: previewKind(mime,filename) SVG-download-gated-FIRST MIME→renderer gate (image/pdf/text/html/docx/xlsx/download)"
  - "artifactMeta.ts: categoryLabel + categoryIcon (mime/ext → 'Categoria · EXT' i18n label + lucide icon, CitationBubble Partial+File-fallback shape, D-16)"
  - "artifactMeta.ts: formatSize(bytes,t) extracted from LocalArtifactDisplay and shared (WEBART-08 refactor-on-touch, DRY)"
  - "downloadAll.ts: sequential throttled <a download> loop (accepted-only, ~500ms throttle, N/M progress, abortable) — the 'Scarica tutto' engine"
  - "artifacts.* i18n copy (en+it) + shell.resizeArtifacts — the panel/preview/downloadAll surface strings"
  - "artifactMeta + downloadAll registered as Stryker mutation targets (mutate array + mutationTests)"
affects: [37B-04, 37B-05, 37B-06, 37B-07, 37B-08]

# Tech tracking
tech-stack:
  added: []   # zero new deps — pure modules over existing react/i18next/lucide-react; docx-preview/xlsx belong to 37B-02
  patterns:
    - "Pure decision-logic modules (no React/DOM) kept 100%-testable + Stryker-targeted so the lazy renderer chunks (37B-04) don't drag the ≥85% aggregate (RESEARCH Pitfall 4)"
    - "previewKind gates image/svg+xml → download BEFORE the image/* branch — the single mutation-tested XSS chokepoint (T-37B-05)"
    - "categoryIcon mirrors CitationBubble's Partial<Record<..,LucideIcon>> + File fallback"

key-files:
  created:
    - web/src/chat/artifacts/artifactMeta.ts
    - web/src/chat/artifacts/artifactMeta.test.ts
    - web/src/chat/artifacts/downloadAll.ts
    - web/src/chat/artifacts/downloadAll.test.ts
  modified:
    - web/src/chat/displays/LocalArtifactDisplay.tsx
    - web/src/i18n/resources.display.ts
    - web/src/i18n/resources.ts
    - web/vitest.stryker.config.ts
    - web/stryker.config.json

key-decisions:
  - "categoryLabel signature is (mime, filename, t) — matches the formatSize(bytes, t) precedent; the pure category() classifier is internal, so the whole surface tests to 100% via a key-echoing stub t"
  - "categoryLabel/categoryIcon use an 8-value category taxonomy (document/text/spreadsheet/image/code/data/web/file) so all 6 required lucide icons (FileText/FileSpreadsheet/FileImage/FileCode/Code2/Globe) + the File fallback are reachable — added artifacts.category.data + .web beyond the plan's 6 required keys (the plan says 'at minimum')"
  - "shell.resizeArtifacts placed in resources.ts (NOT resources.display.ts): the shell namespace lives in resources.ts and ...displayEn spreads AFTER it, so a shell key in the display module would clobber the whole namespace"
  - "artifactMeta.ts + downloadAll.ts added to stryker.config.json `mutate` (the actual mutation-target registration), in addition to their tests in vitest.stryker.config.ts `mutationTests`"
  - "Tests are CO-LOCATED (src/chat/artifacts/*.test.ts) exactly per the plan's files_modified, not under a __tests__/ dir"

patterns-established:
  - "Pattern: pure MIME/ext gate + category map + byte formatter live in one testable .ts module the panel and the inline chip both import"

requirements-completed: []   # INTENTIONALLY EMPTY — WEBART-05/06/08 are phase-spanning; per the 37B-01 + 36-14..18 precedent they stay [ ] until the terminal acceptance plan (37B-08, which adds the Playwright e2e + ≥85% aggregate). See "Requirements Handling".

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "previewKind routes image/pdf/text/html/docx/xlsx and gates image/svg+xml → download FIRST (T-37B-05); category label + lucide icon derived from mime/extension (D-16)"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/artifactMeta.test.ts (previewKind/categoryLabel/categoryIcon suites; artifactMeta.ts 100% stmts/branch/func/lines)"
        status: pass
    human_judgment: false
  - id: D2
    description: "downloadAll sequences one <a href=/api/assets/{id}/download download={file_name}> per accepted asset, ~500ms throttle, no trailing delay, skips degraded, reports N/M, abortable; only the id reaches the href (T-37B-06)"
    requirement: "WEBART-06"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/downloadAll.test.ts (fake timers + HTMLAnchorElement.click spy; downloadAll.ts 100% branch/func)"
        status: pass
    human_judgment: false
  - id: D3
    description: "formatSize extracted to artifactMeta and imported back by LocalArtifactDisplay — the inline chip renders byte-for-byte identically (WEBART-08 non-regression)"
    requirement: "WEBART-08"
    verification:
      - kind: unit
        ref: "web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx (233 displays tests green; 2.0 KB / 512 B / 5.0 MB / 3.0 GB unchanged)"
        status: pass
    human_judgment: false
  - id: D4
    description: "artifacts.* copy exists in BOTH en and it (parity holds); artifactMeta + downloadAll are Stryker mutation targets"
    verification:
      - kind: unit
        ref: "web/src/i18n/__tests__/resources.parity.test.ts (3 pass)"
        status: pass
      - kind: other
        ref: "grep -q artifactMeta vitest.stryker.config.ts && grep -q downloadAll vitest.stryker.config.ts; both source files in stryker.config.json mutate[]"
        status: pass
    human_judgment: false

# Metrics
duration: 25min
completed: 2026-07-08
status: complete
---

# Phase 37B Plan 03: Pure Artifact Foundation Summary

**The pure, 100%-tested 37B foundation: `previewKind` (SVG-download-gated-first MIME→renderer gate), the mime/ext→`categoryLabel`+`categoryIcon` map (D-16), the `formatSize` extracted from `LocalArtifactDisplay` and shared, and the sequential throttled `downloadAll` — with `artifacts.*` i18n copy (en+it) and both pure modules registered as Stryker mutation targets.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-08
- **Tasks:** 3 (2 TDD)
- **Files:** 9 (4 created, 5 modified)

## Accomplishments
- **`artifactMeta.ts` (100% covered):** `previewKind(mime, filename)` routes image/pdf/text/html/docx/xlsx, gating `image/svg+xml` (and `.svg`) to `download` **before** the `image/*` branch so a script-bearing SVG never reaches an executing `<img>` (T-37B-05); pptx/unknown → `download`. `categoryLabel(mime, filename, t)` builds a localized `"Categoria · EXT"` string; `categoryIcon(mime, filename)` returns the mapped lucide icon (FileText/FileSpreadsheet/FileImage/FileCode/Code2/Globe) with a `File` fallback (CitationBubble Partial+fallback precedent). `formatSize` moved here and re-imported by `LocalArtifactDisplay` (DRY, WEBART-08).
- **`downloadAll.ts` (100% covered):** filters to `status==='accepted'`, fires one same-origin `<a href="/api/assets/{id}/download" download={file_name}>` click per row through the 37A auth route, throttled ~500ms (configurable) with no trailing delay, reporting `(done, total)` over the accepted count, skipping degraded rows, abortable via `opts.signal`. The negative test proves only the asset id reaches the href — never a host/container path or storage key (T-37B-06).
- **i18n + Stryker:** new top-level `artifacts.*` namespace in `resources.display.ts` (en+it, parity green) + `shell.resizeArtifacts` in `resources.ts`; `artifactMeta`/`downloadAll` tests added to `vitest.stryker.config.ts` and their sources to `stryker.config.json` `mutate` (≥70%-killed target).
- **Non-regression:** all 233 `chat/displays` tests stay green after the `formatSize` extraction; `tsc --noEmit`, `eslint --max-warnings=0`, and `prettier --check` all clean on every touched file.

## Task Commits

Each task was committed atomically (TDD RED observed locally before GREEN for tasks 1–2):

1. **Task 1: artifactMeta (previewKind + category label/icon + extracted formatSize)** — `b4b3bcb1` (feat)
2. **Task 2: downloadAll (sequential throttled downloader)** — `2ff9a54d` (feat)
3. **Lint/format touch-ups on task-1/2 files (import-order + prettier)** — `43cc7dd3` (style)
4. **Task 3: artifacts.* i18n (en+it) + Stryker registration** — `2b5f0f4c` (feat)

**Plan metadata:** this SUMMARY + STATE.md + ROADMAP.md committed as the plan-completion commit.

## Files Created/Modified
- `web/src/chat/artifacts/artifactMeta.ts` — previewKind, categoryLabel, categoryIcon, formatSize, PreviewKind (pure, 100%).
- `web/src/chat/artifacts/artifactMeta.test.ts` — 17 exact-output cases (SVG-first gate, all MIME/ext, icon identity, byte buckets).
- `web/src/chat/artifacts/downloadAll.ts` — sequential throttled downloader.
- `web/src/chat/artifacts/downloadAll.test.ts` — 6 cases (throttle/skip-degraded/abort/custom-delay/href negative), fake timers + click spy.
- `web/src/chat/displays/LocalArtifactDisplay.tsx` — deleted the local formatSize (+ KB/MB/GB consts + unused TFunction import), imports it from `../artifacts/artifactMeta`; rendered chip unchanged.
- `web/src/i18n/resources.display.ts` — new `artifacts.*` namespace (title/downloadAll/downloadAllProgress/empty/emptyHint/toggleAria/category.*/preview.*) in en + it.
- `web/src/i18n/resources.ts` — `shell.resizeArtifacts` in en + it (beside `resizeNavigation`).
- `web/vitest.stryker.config.ts` — added the two artifact test files to `mutationTests`.
- `web/stryker.config.json` — added the two source files to `mutate`.

## Decisions Made
- **`categoryLabel(mime, filename, t)`** takes `t` (formatSize precedent) and localizes via new `artifacts.category.*` keys; the pure `category()` classifier is internal and tested to 100% through a key-echoing stub `t` (mutation-resistant literal assertions).
- **8-value category taxonomy** (document/text/spreadsheet/image/code/data/web/file) so all six required lucide icons + the `File` fallback are reachable; this needs `artifacts.category.data` + `.web` beyond the plan's six required keys ("at minimum" in the plan permits the extras; both locales get them, parity holds).
- **Co-located tests** (`src/chat/artifacts/*.test.ts`) exactly per the plan's `files_modified`.
- **Strictly avoided the out-of-scope files** the plan forbade: `web/package.json`, `web/package-lock.json`, and `web/src/chat/attachments/types.ts` are byte-unchanged (downloadAll filters on `status`, which is already in the `Asset` type — no `source_kind` widen needed here; that belongs to 37B-02).

## Deviations from Plan

Two files were touched **beyond** the plan's `files_modified`, both required for a task's own stated goal (deviation rules, no scope creep):

### Auto-fixed Issues

**1. [Rule 3 - Corrected location] `shell.resizeArtifacts` added to `resources.ts`, not `resources.display.ts`**
- **Found during:** Task 3 (i18n keys).
- **Issue:** The plan lists `shell.resizeArtifacts` under the `resources.display.ts` keys, but the `shell` namespace is defined in `resources.ts`, and `...displayEn`/`...displayIt` are spread **after** the inline `shell` object — so adding a `shell` key inside `resources.display.ts` would clobber the entire existing `shell` namespace (resizeNavigation, modes, etc.) via object-spread override, breaking the app + other tests.
- **Fix:** Added `resizeArtifacts` directly to the `resources.ts` `shell` object in both locales, beside its `resizeNavigation` sibling. No consumer exists in this plan (the artifacts `ResizableHandle` ships in a later plan); added now per the plan's explicit key list.
- **Files modified:** `web/src/i18n/resources.ts`.
- **Verification:** i18n parity test green (en/it symmetry); `tsc` clean; no `shell.*` key lost.
- **Committed in:** `2b5f0f4c`.

**2. [Rule 2 - Missing critical] `artifactMeta.ts` + `downloadAll.ts` registered in `stryker.config.json` `mutate`**
- **Found during:** Task 3 (register pure modules with Stryker).
- **Issue:** Task 3's goal is "register pure modules with Stryker" / "both pure modules are Stryker targets (≥70% killed)". A module only becomes a mutation target when it is in `stryker.config.json`'s `mutate` array; `vitest.stryker.config.ts`'s `mutationTests` is only the test-include list. Adding the tests alone (the plan's single named file) would run the tests but mutate nothing → the ≥70% goal would be unmeetable.
- **Fix:** Added the two source files to `stryker.config.json` `mutate` alongside adding their tests to `vitest.stryker.config.ts` `mutationTests`. Both are Stryker config files directly serving Task 3's goal.
- **Files modified:** `web/stryker.config.json`.
- **Verification:** `stryker.config.json` is valid JSON; the plan's verify grep (`artifactMeta`/`downloadAll` in `vitest.stryker.config.ts`) passes.
- **Committed in:** `2b5f0f4c`.

---

**Total deviations:** 2 (1 corrected-location, 1 missing-critical). **Impact:** both are minimal, correctness-preserving config touches that make Task 3's own stated goal true; no behavior change, no scope creep. A separate `style` commit (`43cc7dd3`) carried the eslint import-order + prettier touch-ups on the already-committed task-1/2 files (the plan's per-task `<verify>` ran vitest + tsc; eslint/prettier are the pre-push gate).

## Requirements Handling

The plan frontmatter lists `requirements: [WEBART-05, WEBART-06, WEBART-08]`. Plan 03 builds the **pure foundation** these requirements ultimately rely on (the icon/label map, the "Scarica tutto" engine, the shared formatSize non-regression), but does **not** complete any of them: WEBART-05 needs the actual panel + empty-state + mobile drawer (37B-06); WEBART-06 needs the per-row download UI + wiring (37B-06); WEBART-08 explicitly requires a Playwright e2e + the ≥85% web-coverage aggregate (terminal 37B-08). Per the established phase precedent (37B-01 and 36-14..36-18 kept phase-spanning requirements `[ ]` until the terminal acceptance plan) and CLAUDE.md's Definition of Done, `requirements-completed` is left empty and WEBART-05/06/08 remain `[ ]` in REQUIREMENTS.md; they are marked complete at 37B-08. `requirements mark-complete` intentionally NOT run.

## Issues Encountered
- **Prettier reformat after commit:** the plan's per-task `<verify>` ran vitest + `tsc --noEmit` (green), but eslint (`array-type`, `import-x/order`) and prettier line-wrapping only surface at the pre-push `web` gate. Fixed forward with a `style` commit rather than rewriting history. No functional impact.
- No other issues. `go vet ./...` (pre-commit hook, runs on every commit) stayed green throughout (no Go touched).

## User Setup Required
None — no external service configuration and no new dependencies (docx-preview / xlsx installs belong to 37B-02).

## Next Phase Readiness
- **37B-04** (preview modal + lazy renderers) can import `previewKind`/`PreviewKind` for its per-MIME dispatch.
- **37B-06** (panel + row) can import `categoryLabel`/`categoryIcon`/`formatSize` for row rendering and `downloadAll` for the "Scarica tutto" control; the `artifacts.*` copy is in place.
- No blockers. The Stryker ≥70% run on the two modules is a phase-gate item (RESEARCH sampling rate), not a per-plan gate; the targets are registered.

## Self-Check: PASSED
- **Created files exist:** `artifactMeta.ts`, `artifactMeta.test.ts`, `downloadAll.ts`, `downloadAll.test.ts`, `37B-03-SUMMARY.md` — all FOUND.
- **Task commits exist:** `b4b3bcb1`, `2ff9a54d`, `43cc7dd3`, `2b5f0f4c` — all FOUND in `git log`.
- **key_link:** `LocalArtifactDisplay.tsx` imports `formatSize` from `'../artifacts/artifactMeta'` — FOUND.
- **Automated verify:** `artifactMeta.ts` + `downloadAll.ts` at 100% (branch/func/stmt/line); 26 artifacts+parity tests pass; 233 displays tests pass; `tsc --noEmit`, `eslint --max-warnings=0`, `prettier --check` clean on all touched files; `stryker.config.json` valid JSON with both `mutate` entries.

---
*Phase: 37B-web-artifact-sidebar*
*Completed: 2026-07-08*
