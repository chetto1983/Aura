---
phase: 37B-web-artifact-sidebar
plan: 01
subsystem: docs
tags: [prd-amendment, webart, artifact-sidebar, docx-preview, sheetjs, xlsx, iframe-sandbox, react-query, supply-chain]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery-lane
    provides: "asset_id delivery handle + GET /api/assets/{id}/download (identity-scoped, attachment/octet-stream/nosniff) + the aura.artifact SSE event (asset_id/filename/size_bytes/mime_type) + the D-10 top-level-navigation XSS guard"
provides:
  - "PRD-amendment #78 (prd.md): the architectural record for the 37B Artefatti web sidebar"
  - "The WEBART-05..08 requirement group transcribed into the PRD truth-source"
  - "The Artefatti sidebar surface contract (toggleable 3rd ResizablePanel + right Drawer + derived useQuery view + click-to-preview modal), no new source of truth / no new endpoint"
  - "The preview renderer set + two new web deps with corrected provenance: docx-preview Apache-2.0, SheetJS xlsx from cdn.sheetjs.com >=0.20.2 (NOT CVE-ridden npm 0.18.5)"
  - "The null-origin HTML sandbox policy (allow-scripts, NO allow-same-origin)"
  - "The D-14 threadId-keyed durable query + D-15 source_kind split-fold saved-conversation download-persistence behavior"
affects: [37B-02, 37B-03, 37B-04, 37B-05, 37B-06, 37B-07, 37B-08, 37F-conversation-artifact-sharing]

# Tech tracking
tech-stack:
  added: []   # docs-only: docx-preview + SheetJS xlsx are RECORDED for later plans (37B-02+), not installed here
  patterns:
    - "PRD-first BLOCKING amendment: the git-log ordering (amendment commit before any code) is the gate (same pattern as #44/#62/#63/#64)"

key-files:
  created:
    - .planning/phases/37B-web-artifact-sidebar/37B-01-SUMMARY.md
  modified:
    - prd.md

key-decisions:
  - "docx-preview license recorded as Apache-2.0 (CONTEXT D-07 said MIT; RESEARCH A4 correction — both permissive, PRD now records the accurate license)"
  - "SheetJS xlsx supply-chain shape recorded as the CDN tarball https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz, NOT `npm i xlsx` (frozen 0.18.5 + CVE-2023-30533 + CVE-2024-22363)"
  - "HTML preview policy recorded as null-origin sandbox=\"allow-scripts\" with NO allow-same-origin; the allow-scripts+allow-same-origin combination is explicitly forbidden"
  - "Saved-conversation download persistence recorded: D-14 (threadId-keyed useQuery = durable, no reload) + D-15 (split attachAssetsToUserMessages by source_kind, fold agent deliverables onto assistant turns)"
  - "Amendment placed in the web-cockpit amendment cluster (#78 after #77): prd.md has no literal WEBART-01..04 delivery-lane section (37A recorded WEBART in REQUIREMENTS.md, not prd.md), so the web-cockpit amendment cluster is the correct semantic neighbor"

patterns-established:
  - "PRD-first gate: a docs-only PRD-amendment lands as a standalone commit before any phase implementation code"

requirements-completed: []   # INTENTIONALLY EMPTY — see "Requirements Handling" below. WEBART-05..08 are DOCUMENTED here, not implemented/tested; per project precedent (36-14..36-18) phase-spanning requirements stay [ ] until the terminal acceptance plan (37B-08).

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "prd.md records the WEBART-05..08 requirement group, the Artefatti sidebar surface, the preview renderer set + two web deps (docx-preview Apache-2.0, SheetJS xlsx via CDN), the null-origin allow-scripts HTML sandbox policy, and the D-14/D-15 saved-conversation download persistence — the plan's acceptance is fully machine-checkable"
    verification:
      - kind: automated_ui
        ref: "grep -q WEBART-05 prd.md && grep -q WEBART-08 prd.md && grep -q cdn.sheetjs.com prd.md && grep -q docx-preview prd.md && grep -q allow-scripts prd.md → PRD_AMENDMENT_OK"
        status: pass
      - kind: other
        ref: "git diff --name-only HEAD~1 HEAD (task commit 33b1f423) shows only prd.md; no web/ or package.json touched"
        status: pass
    human_judgment: false

# Metrics
duration: 6min
completed: 2026-07-08
status: complete
---

# Phase 37B Plan 01: PRD-Amendment Gate (Artefatti Web Sidebar) Summary

**PRD-amendment #78 lands the 37B architectural record — the Artefatti right-side sidebar surface, the WEBART-05..08 group, the docx-preview (Apache-2.0) + SheetJS-xlsx-via-CDN preview deps, the null-origin `allow-scripts` HTML sandbox policy, and the D-14/D-15 saved-conversation download persistence — before a single line of 37B code (D-19, PRD-first absolute).**

## Performance

- **Duration:** 6 min
- **Started:** 2026-07-08T20:36:03Z
- **Completed:** 2026-07-08T20:42:02Z
- **Tasks:** 1
- **Files modified:** 1 (prd.md)

## Accomplishments
- Added **Amendment #78** to `prd.md` (in the web-cockpit amendment cluster, after #77) covering all five mandated items: (1) the WEBART-05..08 requirement group transcribed from REQUIREMENTS.md:69-72; (2) the Artefatti sidebar surface (toggleable 3rd right `ResizablePanel` + `Drawer side="right"` mobile fallback + derived `useQuery(['assets', threadId])` view over the shipped `GET /api/assets?thread_id=`, ownership via `GetForIdentity`, per-row + "Scarica tutto" downloads, click-to-preview modal) — **no new source of truth, no new backend endpoint**; (3) the MIME-gated renderer set + the two new web deps with corrected provenance; (4) the null-origin HTML sandbox policy; (5) the D-14/D-15 saved-conversation download-persistence behavior.
- Carried the three **RESEARCH corrections over stale CONTEXT**: `docx-preview` is **Apache-2.0** (not MIT); SheetJS `xlsx` installs from **`https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`** (not the frozen npm 0.18.5 carrying CVE-2023-30533 + CVE-2024-22363); the server list order stays `created_at ASC` (client sorts newest-first).
- Satisfied the **PRD-first D-19 gate**: the amendment is a standalone docs-only commit landing before any 37B implementation (git-log ordering is the gate).

## Task Commits

1. **Task 1: Amend prd.md with the 37B Artefatti sidebar + WEBART-05..08 + preview deps + persistence** — `33b1f423` (docs)

**Plan metadata:** (this SUMMARY + STATE.md/ROADMAP.md) committed separately as the plan-completion commit.

## Files Created/Modified
- `prd.md` — added Amendment #78 (12 insertions): the 37B Artefatti sidebar PRD record (WEBART-05..08 + surface + preview deps + null-origin sandbox policy + D-14/D-15 persistence + scope guard).
- `.planning/phases/37B-web-artifact-sidebar/37B-01-SUMMARY.md` — this summary.

## Decisions Made
- **License correction recorded:** `docx-preview` = Apache-2.0 (RESEARCH A4 corrects CONTEXT D-07's "MIT").
- **Supply-chain shape recorded:** SheetJS `xlsx` from the CDN tarball (≥0.20.2), never `npm i xlsx` (frozen 0.18.5 + 2 CVEs). This unusual `package.json` dependency shape is now an auditable PRD record (threat T-37B-01).
- **Security posture recorded as contract, not accident:** null-origin `sandbox="allow-scripts"` with NO `allow-same-origin` for HTML preview; the combined `allow-scripts`+`allow-same-origin` is explicitly forbidden (threat T-37B-02).
- **Placement:** Amendment #78 sits in the web-cockpit amendment cluster (after #77) because `prd.md` contains no literal WEBART-01..04 delivery-lane section — 37A recorded WEBART in `REQUIREMENTS.md`, not `prd.md`; the web-cockpit cluster is the semantic neighbor and keeps the amendment numbering contiguous.

## Deviations from Plan

None affecting the artifact — **the PRD amendment was written exactly as the plan's `<action>` specified**, and every acceptance criterion passes. Two process/context notes (not code deviations):

**1. [Context] The plan's `read_first` assumed a "WEBART-01..04 delivery-lane" section in `prd.md`; none exists.**
- **Found during:** Task 1 (locating the append point).
- **Detail:** `grep WEBART prd.md` → 0 matches. 37A recorded WEBART-01..04 in `.planning/REQUIREMENTS.md`, not in `prd.md`. This is a pre-existing 37A documentation gap, out of scope for this docs-only 37B plan (scope control).
- **Resolution:** Placed Amendment #78 in the existing web-cockpit amendment cluster (after #77), the closest semantic neighbor, preserving the PRD's `Amendment #NN` convention and contiguous numbering. No 37A content was added or altered.

**2. [State-management] `requirements mark-complete` intentionally NOT run for WEBART-05..08.** See "Requirements Handling" below.

## Requirements Handling

The plan frontmatter lists `requirements: [WEBART-05, WEBART-06, WEBART-07, WEBART-08]`. This docs-only plan **documents** that group in the PRD; it does **not** implement or test it (the panel, downloads, preview renderers, non-regression tests, and the ≥85% web-coverage gate ship in plans 37B-02..08). Marking these requirements complete now would be false — WEBART-08's acceptance explicitly requires React unit tests + a Playwright e2e + web coverage ≥85%, none delivered here.

Per the established project precedent (36-14 through 36-18 all kept phase-spanning requirements `[ ]` with "`requirements mark-complete` intentionally NOT run" until the terminal acceptance plan) and CLAUDE.md's Definition of Done, `requirements-completed` is left empty and WEBART-05..08 remain `[ ]` in REQUIREMENTS.md. They are marked complete at the phase's terminal acceptance gate (37B-08).

## Issues Encountered
- **SDK arg interface:** the `state record-metric` / `state add-decision` handlers use named flags (`--phase/--plan/--duration/--tasks/--files`, `--phase/--summary/--rationale`), not the positional args shown in the workflow doc. Adapted after reading `state-command-router.cjs`.
- **Progress/roadmap counts read SUMMARYs from disk:** the first `roadmap update-plan-progress` run reported `summary_count: 0` (SUMMARY not yet written); re-synced after writing this file. No other issues.

## User Setup Required
None — no external service configuration required. (The two new web deps are recorded for later plans; no install happened here.)

## Next Phase Readiness
- **PRD-first D-19 gate satisfied** — the architectural record every downstream 37B plan builds against is now in `prd.md`. Plan **37B-02** (deps + `Asset.source_kind` type widening + pure-logic modules) is unblocked.
- No blockers. The two web deps (`docx-preview`, SheetJS `xlsx` via CDN) install at 37B-02+; the CDN-tarball supply-chain shape is pre-recorded so the install path is unambiguous.

## Self-Check: PASSED
- **Created file exists:** `.planning/phases/37B-web-artifact-sidebar/37B-01-SUMMARY.md` — FOUND (this file).
- **Modified file exists + committed:** `prd.md` — committed in `33b1f423`, `git diff --name-only HEAD~1 HEAD` shows only `prd.md`, no deletions.
- **Task commit exists:** `33b1f423` — FOUND in `git log`.
- **Plan automated verify:** `PRD_AMENDMENT_OK` (WEBART-05, WEBART-08, cdn.sheetjs.com, docx-preview, allow-scripts all present; `allow-same-origin` present only as forbidden/negative).

---
*Phase: 37B-web-artifact-sidebar*
*Completed: 2026-07-08*
