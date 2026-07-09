---
phase: 37B-web-artifact-sidebar
plan: 02
subsystem: ui
tags: [docx-preview, xlsx, sheetjs, jszip, supply-chain, typescript, attachments]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery
    provides: "backend source_kind='agent' widen (migration 0035) + authenticated web asset download"
  - phase: 37B-01
    provides: "wave-1 predecessor in the 37B artifact-sidebar phase"
provides:
  - "docx-preview@0.4.0 (Apache-2.0) + transitive jszip@3.10.1 installed and resolvable"
  - "xlsx SheetJS CE 0.20.3 (Apache-2.0) installed from cdn.sheetjs.com tarball (CVE-safe, NOT frozen npm 0.18.5)"
  - "Asset.source_kind TS union widened to 'web'|'telegram'|'cli'|'agent'"
affects: [37B-preview-renderers, 37B-agent-panel, 37B-split-fold]

# Tech tracking
tech-stack:
  added: ["docx-preview@0.4.0", "jszip@3.10.1 (transitive)", "xlsx@0.20.3 (SheetJS CE, CDN tarball)"]
  patterns: ["CDN-tarball URL dependency in package.json to bypass a frozen+CVE-laden npm registry copy"]

key-files:
  created: []
  modified:
    - web/package.json
    - web/package-lock.json
    - web/src/chat/attachments/types.ts

key-decisions:
  - "xlsx installed from https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz (URL dependency), NEVER `npm i xlsx` — the npm registry copy is frozen 0.18.5 with CVE-2023-30533 (prototype pollution HIGH) + CVE-2024-22363 (ReDoS)"
  - "No `overrides` pin needed — docx-preview@0.4.0 pulls jszip@3.10.1 cleanly; nothing dragged xlsx@0.18.5"
  - "react-doc-viewer rejected (its Office path proxies through Microsoft Office Online requiring public URLs — incompatible with Aura isolation)"

patterns-established:
  - "Supply-chain: a CDN-tarball URL dependency is the sanctioned form for a package whose registry copy is CVE-frozen; gated by a blocking-human legitimacy checkpoint"

requirements-completed: [WEBART-05, WEBART-07]

coverage:
  - id: D1
    description: "docx-preview@0.4.0 (Apache-2.0) + transitive jszip@3.10.1 installed and resolvable"
    requirement: "WEBART-05"
    verification:
      - kind: automated_ui
        ref: "cd web && npm ls docx-preview (→0.4.0) && npm ls jszip (→3.10.1)"
        status: pass
    human_judgment: false
  - id: D2
    description: "xlsx SheetJS CE 0.20.3 installed from cdn.sheetjs.com tarball (URL dependency, CVE-safe, never 0.18.5)"
    requirement: "WEBART-05"
    verification:
      - kind: automated_ui
        ref: "cd web && npm ls xlsx (→0.20.3) && grep cdn.sheetjs.com package.json"
        status: pass
    human_judgment: false
  - id: D3
    description: "Asset.source_kind TS union widened to include 'agent'; project type-checks clean"
    requirement: "WEBART-07"
    verification:
      - kind: automated_ui
        ref: "cd web && grep \"'agent'\" src/chat/attachments/types.ts && npx tsc --noEmit"
        status: pass
    human_judgment: false

# Metrics
duration: 8min
completed: 2026-07-08
status: complete
---

# Phase 37B Plan 02: Preview-Deps + source_kind Widen Summary

**docx-preview@0.4.0 + transitive jszip@3.10.1 + SheetJS CE xlsx@0.20.3 (from the cdn.sheetjs.com CVE-safe tarball) installed, and the `Asset.source_kind` TS union widened to include `'agent'` — the supply-chain + type foundation for the artifact-preview renderers.**

## Performance

- **Duration:** ~8 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (Task 1 checkpoint pre-approved; Task 2 executed)
- **Files modified:** 3

## Accomplishments
- Installed `docx-preview@0.4.0` (Apache-2.0) which pulled `jszip@3.10.1` (≥3.0.0) transitively.
- Installed `xlsx@0.20.3` (SheetJS CE, Apache-2.0) from `https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz` as a package.json URL dependency — the CVE-safe form; the frozen npm registry copy (0.18.5, CVE-2023-30533 + CVE-2024-22363) was never installed.
- Widened `Asset.source_kind` from `'web' | 'telegram' | 'cli'` to `'web' | 'telegram' | 'cli' | 'agent'` (RESEARCH Pitfall 3 — 37A widened the backend via migration 0035 but not the frontend type), unblocking the `source_kind === 'agent'` panel filter + the D-15 split-fold.
- `npx tsc --noEmit` clean across all existing consumers; `npm audit` reported 0 vulnerabilities.

## Task Commits

1. **Task 1: Package legitimacy gate (checkpoint:human-verify / blocking-human)** — pre-approved by the human user on 2026-07-08 (no re-prompt; see Checkpoint Evidence below).
2. **Task 2: Install docx-preview + xlsx (CDN) and widen the source_kind union** — `6262db71` (feat)

**Plan metadata:** committed separately (docs: complete 37B-02 plan)

## Checkpoint Evidence (Task 1 — APPROVED)

Task 1 is a `blocking-human` package-legitimacy gate; the orchestrator performed supply-chain due diligence and the human user explicitly APPROVED it on 2026-07-08. Recorded evidence:

- **docx-preview:** Apache-2.0, v0.4.0 (published 2026-07-07, actively maintained), repo github.com/VolodymyrBaydalka/docxjs, declares dep `jszip>=3.0.0`. APPROVED.
- **jszip (transitive):** MIT-OR-GPL, v3.10.1, repo github.com/Stuk/jszip, long-established. APPROVED.
- **xlsx:** npm registry copy is FROZEN at 0.18.5 (2022) with CVE-2023-30533 (prototype pollution) + CVE-2024-22363 (ReDoS). Installed SheetJS CE 0.20.3 from the CDN tarball https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz (verified live: HTTP 200, 2.4MB; 0.20.3 fixes both CVEs). APPROVED — `npm i xlsx` forbidden.
- **react-doc-viewer:** evaluated and REJECTED (Office path proxies through Microsoft Office Online requiring public URLs — incompatible with Aura isolation). NOT installed.

## Files Created/Modified
- `web/package.json` — added `docx-preview: ^0.4.0` + `xlsx: https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`
- `web/package-lock.json` — lockfile refreshed (docx-preview, jszip, xlsx from CDN)
- `web/src/chat/attachments/types.ts` — `Asset.source_kind` widened to the four-member union including `'agent'`

## Decisions Made
- xlsx sourced from the CDN tarball (URL dependency), never the npm registry — the registry copy is CVE-frozen at 0.18.5.
- No `overrides` pin required: the install produced xlsx@0.20.3 and jszip@3.10.1 directly with nothing dragging the frozen 0.18.5.

## Deviations from Plan

None - plan executed exactly as written. The plan's contingency `overrides` pin (Task 2 action step 3) was NOT needed because no transitive resolution dragged `xlsx@0.18.5`.

## Issues Encountered
None.

## Verification Results
- `npm ls docx-preview` → 0.4.0
- `npm ls jszip` → 3.10.1 (≥3.0.0, transitive of docx-preview)
- `npm ls xlsx` → 0.20.3 (never 0.18.5)
- `package.json` xlsx value contains `cdn.sheetjs.com` (URL dependency, not a bare `^0.18.x` semver)
- `src/chat/attachments/types.ts` source_kind is the 4-member union including `'agent'`
- `npx tsc --noEmit` → exit 0
- Automated verify chain → `DEPS_AND_TYPE_OK`
- `npm audit` → 0 vulnerabilities

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The two lazy-loaded preview libs (docx/xlsx) are available for the preview-renderer chunks.
- The widened `source_kind` union unblocks the agent-scoped derived view + rehydration fold (D-15 split-fold) and the `source_kind === 'agent'` panel filter.

## Self-Check: PASSED
- SUMMARY.md exists at .planning/phases/37B-web-artifact-sidebar/37B-02-SUMMARY.md
- Task 2 commit 6262db71 present in git log
- web/src/chat/attachments/types.ts present with widened union

---
*Phase: 37B-web-artifact-sidebar*
*Completed: 2026-07-08*
