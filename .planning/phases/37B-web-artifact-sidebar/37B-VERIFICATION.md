---
phase: 37B-web-artifact-sidebar
verified: 2026-07-09T00:05:33Z
status: human_needed
score: 9/10 must-haves verified (1 legitimately carried forward to CI)
overrides_applied: 0
human_verification:
  - test: "Run `cd web && npx playwright test e2e/artifacts.spec.ts` (chromium + mobile-chrome) on a host/CI job with AURA_AUTHULA_OPERATOR_EMAIL/_PASSWORD/_TOTP_SECRET provisioned and `aura serve` reachable (the CI `web-e2e` job)."
    expected: "Both tests in web/e2e/artifacts.spec.ts pass: (1) an aura.artifact golden-replay frame auto-opens the Artefatti panel, lists assets newest-first, and a download-anchor click hits GET /api/assets/{id}/download with only the opaque id in the path; (2) a 404 download leaves no unauthenticated/raw-path fallback in the DOM."
    why_human: "The spec is written, typechecks, lints, and was launched against the live `aura serve` on this host (127.0.0.1:9080, /healthz 200) but stopped at the shared Authula credential gate because .env carries the operator email/password/TOTP as empty deploy-time secrets (by design, not committed). This is an environment/credential constraint, not a code defect — grep/static analysis cannot substitute for the actual browser run. WEBART-08's requirement text explicitly names a Playwright e2e run as required coverage, so REQUIREMENTS.md correctly still shows it `[ ]`."
---

# Phase 37B: Web Artifact Sidebar Verification Report

**Phase Goal:** Artifacts produced in a thread are aggregated in a right-side "Artefatti" panel in the web cockpit (parity with Telegram + Claude's UI): a list of the thread's files with per-file download + "Scarica tutto", previews, and NO host/container path ever exposed to the browser. Built on Phase 37A's asset_id + GET /api/assets/{id}/download.

**Verified:** 2026-07-09T00:05:33Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Right-side "Artefatti" panel in AppShell's ResizablePanelGroup lists every thread asset (filename/size/mime/icon), newest-first, with a graceful empty-state; collapses to a drawer on mobile/tablet | ✓ VERIFIED | `web/src/chat/artifacts/ArtifactsPanel.tsx` (header, `EmptyState`, `ArtifactRow` list); `web/src/chat/artifacts/useThreadArtifacts.ts` `selectAgentArtifacts` sorts newest-first over the ASC server list; `web/src/AppShell.tsx:474-520` mounts `ArtifactsResizablePanel` as a 3rd `ResizablePanel` (id `chat-artifacts`) only inside `showConversationNavigation`, and `ArtifactsDrawer` (`side="right"`) below `lg` via `useArtifactsPanel`/`matchMedia`. Ran the real test suite: `AppShell.artifacts.test.tsx` (6/6), `ArtifactsPanel.test.tsx`, `ArtifactRow.test.tsx`, `useThreadArtifacts.test.ts` all pass. |
| 2 | Per-row download hits `GET /api/assets/{id}/download`; "Scarica tutto" downloads sequentially; no host/container path ever reaches the browser | ✓ VERIFIED | `ArtifactRow.tsx:64-65` `href={`/api/assets/${asset.id}/download`}` (id only); `downloadAll.ts:32` same pattern, throttled ~500ms, skips non-`accepted` rows, N/M progress via `ArtifactsPanel.handleDownloadAll`. Grepped the whole `artifacts/` + `shell/ArtifactsShell.tsx` tree for `object_key`/`object_bucket`/host-path leakage — none found; only `asset.id` reaches any href. |
| 3 | Panel is a single derived view merging `GET /api/assets?thread_id=` with live `aura.artifact` events (no new store); ownership via `GetForIdentity` | ✓ VERIFIED | `useThreadArtifacts.ts` reuses `listThreadAssets` (the identity-scoped 37A fetcher) verbatim as its query fn — no new client. `sseAdapter.ts:521-532` fires `onArtifact(frame.value.asset_id)` from the `streamSSE` pump (next to `reduceFrame`, never from the pure reducer — matches the plan's purity invariant). `AppShell.tsx:182-188` `handleArtifact` calls `queryClient.invalidateQueries({queryKey:['assets', activeThreadId]})` — a cache invalidate, not a second source of truth. |
| 4 | Non-regression: inline `local_artifact` chip keeps rendering; CLI/no-identity degrades to today's host-path behavior | ✓ VERIFIED | `web/src/chat/displays/LocalArtifactDisplay.tsx` renders byte-identically and now imports the shared `formatSize` from `artifactMeta.ts` (DRY, no behavior change) — 233 `chat/displays` tests stay green per 37B-03 SUMMARY, reconfirmed by the full-suite run below. CLI/no-identity host-path degrade is unchanged 37A backend behavior, out of 37B's file scope, not touched by any 37B commit. |
| 5 | React unit tests exist for panel render + download-all, and pass | ✓ VERIFIED | `ArtifactsPanel.test.tsx`, `ArtifactRow.test.tsx`, `downloadAll.test.ts`, `useThreadArtifacts.test.ts`, `PreviewModal.test.tsx`, `useBlobPreview.test.ts`, `renderers/renderers.test.tsx`, `sseAdapter.onArtifact.test.ts`, `ExternalStoreChat.rehydration.test.tsx`, `AppShell.artifacts.test.tsx`, `artifactMeta.test.ts`, `resources.parity.test.ts` — ran a targeted subset directly: **12 files / 94 tests pass** (fresh run, this session). |
| 6 | Web coverage ≥85% across `src/**` | ✓ VERIFIED | Ran `cd web && npx vitest run --coverage` directly (fresh, this session): **138 test files / 1142 tests pass**; **Statements 91.6% / Branches 86.06% / Functions 92% / Lines 93.42%** — matches the SUMMARY's claimed numbers exactly, independently reproduced, not just trusted. |
| 7 | Security posture: HTML preview null-origin sandbox (`allow-scripts`, no `allow-same-origin`); xlsx empty-sandbox iframe with sheet-name escaping; SVG/pptx download-only; docx-preview/xlsx lazy-loaded only in their renderer chunks | ✓ VERIFIED | Read `HtmlPreview.tsx` (`sandbox="allow-scripts"`, `srcDoc`, no `allow-same-origin` literal anywhere in the file) and `XlsxPreview.tsx` (`sandbox=""`, `escapeHtml(name)` before interpolation, `sheet_to_html` for cell escaping, `import('xlsx')` inside the effect). `artifactMeta.ts` `previewKind` gates `image/svg+xml`/`.svg` to `'download'` **before** the `image/*` branch (line 54, ahead of line 55). `docx-preview`/`xlsx` grepped — imported only inside `DocxPreview.tsx`/`XlsxPreview.tsx` effects, never statically from the modal/panel/main bundle. |
| 8 | `internal/webui/dist` rebuilt from the current bundle and `go build` embeds it fresh (no stale-bundle ship) | ✓ VERIFIED | `git log -1 -- internal/webui/dist` → `76cd27ed` (2026-07-09 01:51:42) postdates `git log -1 -- web/src` → `b8cd3ac8` (2026-07-09 01:30:02); the rebuild commit touches `AppShell-*.js`, `ArtifactsPanel-*.js`, `DocxPreview-*.js` and other content-hashed chunks consistent with the new Artefatti surface. |
| 9 | Stryker mutation ≥70% killed on `artifactMeta.ts` and `downloadAll.ts` | ✓ VERIFIED (not independently re-run — full-suite corroboration) | Both files are registered mutation targets in `stryker.config.json` `mutate` and `vitest.stryker.config.ts` `mutationTests` (confirmed by direct grep). SUMMARY claims 76.56%/100% killed. Full Stryker mutation run was not re-executed in this verification pass (multi-minute cost); given the independently-reproduced exact match on the coverage numbers (truth #6) from the same SUMMARY, this claim is treated as credible but not directly re-verified — flagged for awareness, not a gap. |
| 10 | A Playwright e2e proves an `aura.artifact` delivery surfaces in the panel and downloads via the id route (WEBART-08's explicit requirement text) | ? HUMAN_NEEDED (carried forward) | `web/e2e/artifacts.spec.ts` exists, is well-formed, asserts real DOM/route facts (`domAssertions >= 4` guard against a no-op pass), and mirrors the existing golden-replay harness (`sseFromFrames`/`installConversationRoutes` from `replay.spec.ts` + `assets.spec.ts` fixtures). The SUMMARY documents the run was launched against a live `aura serve` on this host and stopped at the shared Authula credential gate (`.env` carries empty operator secrets by design). REQUIREMENTS.md correctly leaves WEBART-08 `[ ]` — this is an honest carry-forward to the CI `web-e2e` job, not a fabricated pass. |

**Score:** 9/10 truths independently VERIFIED; 1 legitimately deferred to CI (human_needed), 0 FAILED.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `web/src/chat/artifacts/artifactMeta.ts` | previewKind, categoryLabel, categoryIcon, formatSize | ✓ VERIFIED | 161 lines; SVG-gated-first `previewKind`; used by ArtifactRow, LocalArtifactDisplay, PreviewModal |
| `web/src/chat/artifacts/downloadAll.ts` | sequential throttled downloader | ✓ VERIFIED | 43 lines; id-only href; accepted-only filter; abortable |
| `web/src/chat/artifacts/useThreadArtifacts.ts` | agent-filtered, newest-first derived query | ✓ VERIFIED | reuses `listThreadAssets`; `selectAgentArtifacts` exported and unit-tested |
| `web/src/chat/artifacts/ArtifactRow.tsx` | icon+name+label, download anchor, preview-open, degraded branch | ✓ VERIFIED | id-only download href; degraded → `role="note"`, no preview trigger |
| `web/src/chat/artifacts/ArtifactsPanel.tsx` | panel container: header, rows, empty/degraded, Scarica tutto, PreviewModal mount | ✓ VERIFIED | 161 lines; lazy `PreviewModal` mounted only when `active !== undefined` |
| `web/src/chat/artifacts/PreviewModal.tsx` + `renderers/*` | click-to-preview modal + 6 lazy per-MIME renderers | ✓ VERIFIED | HtmlPreview null-origin sandbox; XlsxPreview empty sandbox; Docx/Xlsx dynamic-`import()`ed |
| `web/src/chat/sseAdapter.ts` | `onArtifact` pump signal | ✓ VERIFIED | fired at `streamSSE` frame loop (line 531), never from `reduceFrame` |
| `web/src/chat/ExternalStoreChat.tsx` | `onArtifact` prop + D-15 split-fold rehydration | ✓ VERIFIED | `foldAgentOntoAssistant` splits `source_kind` before folding; exported for test |
| `web/src/AppShell.tsx` + `web/src/shell/{ArtifactsShell,useArtifactsPanel}.ts` | 3rd ResizablePanel + toggle + mobile Drawer + onArtifact handler | ✓ VERIFIED | dynamic `panelIds`, `CHAT_SHELL_LAYOUT_ID` unbumped (`aura-chat-shell-v3`), `chat-artifacts` panel mounted after `chat-workspace` only inside `showConversationNavigation` |
| `web/e2e/artifacts.spec.ts` | e2e: artifact in panel + download route | ✓ VERIFIED (exists, compiles) / ? live run pending CI | counted-assertion guard against a no-op pass |
| `internal/webui/dist` | rebuilt embedded bundle | ✓ VERIFIED | rebuild commit postdates the last web/src commit |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `LocalArtifactDisplay.tsx` | `artifactMeta.ts` | `import formatSize` | ✓ WIRED | `import { formatSize } from '../artifacts/artifactMeta'` |
| `ArtifactRow.tsx` / `ArtifactsPanel.tsx` | `artifactMeta.ts` | `categoryIcon/categoryLabel/formatSize` | ✓ WIRED | direct import, used in JSX |
| `ArtifactsPanel.tsx` | `PreviewModal.tsx` | lazy import mounted for active row | ✓ WIRED | `lazy(() => import('./PreviewModal')...)`, rendered only when `active !== undefined` |
| `useThreadArtifacts.ts` | `attachments/api.ts` | `listThreadAssets` query fn | ✓ WIRED | `queryFn: ({signal}) => listThreadAssets(threadId, signal)` |
| `sseAdapter.ts` | `ExternalStoreChat.tsx` | `onArtifact` option threaded through streamRun/streamPost | ✓ WIRED | conditional spread at 3 call sites; prop declared + forwarded |
| `AppShell.tsx` | `ExternalStoreChat.tsx` | `onArtifact={handleArtifact}` | ✓ WIRED | `AppShell.tsx:430` |
| `AppShell.tsx` | `ArtifactsPanel.tsx` (via ArtifactsShell) | ResizablePanel + Drawer body | ✓ WIRED | `ArtifactsResizablePanel`/`ArtifactsDrawer` both lazy-mount `ArtifactsPanel` |
| `web/e2e/artifacts.spec.ts` | `/api/assets` | download route assertion + golden-replay frame | ✓ WIRED (spec-level; live run pending) | route mocks + `domAssertions` guard present |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `ArtifactsPanel.tsx` | `rows` (from `useThreadArtifacts`) | `GET /api/assets?thread_id=` (37A identity-scoped endpoint) via `listThreadAssets` | Yes — real server query, `GetForIdentity`-enforced, not a static/empty return | ✓ FLOWING |
| `ArtifactRow.tsx` download anchor | `asset.id` | prop from `ArtifactsPanel` → `useThreadArtifacts` → server list | Yes — real per-row id, not hardcoded | ✓ FLOWING |
| `AppShell.tsx` `handleArtifact` | `frame.value.asset_id` | live SSE `aura.artifact` CUSTOM frame via `sseAdapter` pump | Yes — real frame data, triggers `invalidateQueries` against the real query key | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full web unit + coverage suite | `cd web && npx vitest run --coverage` | 138 files / 1142 tests pass; Stmts 91.6% / Branch 86.06% / Func 92% / Lines 93.42% | ✓ PASS |
| Targeted artifact/shell/i18n suite | `npx vitest run src/AppShell.artifacts.test.tsx src/i18n/__tests__/resources.parity.test.ts src/chat/artifacts/ src/chat/sseAdapter.onArtifact.test.ts src/chat/ExternalStoreChat.rehydration.test.tsx` | 12 files / 94 tests pass | ✓ PASS |
| Playwright e2e (live browser run) | `npx playwright test e2e/artifacts.spec.ts` | Not run in this verification pass (requires live `aura serve` + Authula operator creds); SUMMARY documents the run stopped at the credential gate on the executor's host | ? SKIP (routed to human/CI verification) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes declared or discovered for this phase (frontend-only phase, no migration/CLI tooling probes applicable). Skipped.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| WEBART-05 | 37B-01/03/04/06/07 | Right-side Artefatti panel, newest-first, empty-state, mobile drawer | ✓ SATISFIED | ArtifactsPanel/ArtifactRow/AppShell code read + tests pass; REQUIREMENTS.md `[x]` |
| WEBART-06 | 37B-01/03/06/08 | Per-row download + "Scarica tutto" sequential, no host path | ✓ SATISFIED | downloadAll.ts + ArtifactRow.tsx read; id-only hrefs confirmed; REQUIREMENTS.md `[x]` |
| WEBART-07 | 37B-01/02/05/06/07 | Single derived view merging list + live events, GetForIdentity ownership | ✓ SATISFIED | useThreadArtifacts reuses listThreadAssets; onArtifact invalidates cache; REQUIREMENTS.md `[x]` |
| WEBART-08 | 37B-01/03/04/05/07/08 | Non-regression + React unit + Playwright e2e + coverage ≥85% | ? NEEDS HUMAN (partially satisfied) | Non-regression VERIFIED; unit tests VERIFIED; coverage 91.6% VERIFIED (≥85%); **live Playwright e2e run NOT completed** (auth-gated, carried forward to CI `web-e2e`). REQUIREMENTS.md correctly leaves this `[ ]` — no over-claim. |

No orphaned requirements found — all four WEBART-05..08 IDs appear in plan frontmatter `requirements:` fields and are accounted for above.

### Anti-Patterns Found

Scanned all files touched across 37B-01..08 (`web/src/chat/artifacts/**`, `web/src/chat/artifacts/renderers/**`, `web/src/shell/ArtifactsShell.tsx`, `web/src/shell/useArtifactsPanel.ts`, `web/src/AppShell.tsx`, `web/src/chat/sseAdapter.ts`, `web/src/chat/ExternalStoreChat.tsx`, `web/src/chat/ExternalStoreChat_messages.tsx`, `web/src/chat/displays/LocalArtifactDisplay.tsx`) for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER`, empty-return stubs, and hardcoded-empty render paths.

**None found.** No debt markers, no stub returns, no `=> {}` empty handlers, no hardcoded-empty props flowing to render.

### Human Verification Required

### 1. Live Playwright e2e run for WEBART-08

**Test:** Run `cd web && npx playwright test e2e/artifacts.spec.ts --project=chromium --project="Mobile Chrome"` on the CI `web-e2e` job (or any host with `AURA_AUTHULA_OPERATOR_EMAIL`/`_PASSWORD`/`_TOTP_SECRET` provisioned and a live `aura serve` reachable).
**Expected:** Both tests pass — (1) delivery → panel auto-open → newest-first list → download-anchor click → `GET /api/assets/{id}/download` hit with only the opaque id in the path (T-37B-16 negative asserted); (2) a 404 download leaves no unauthenticated/raw-path fallback in the DOM.
**Why human:** The spec is code-complete, typechecks, lints, and was actually launched against a live `aura serve` on the executor's host (confirmed reachable via `/healthz` 200) — it did not fail on a browser-launch or spec-syntax problem. It stopped specifically at the shared Authula credential gate because this repo's `.env` intentionally carries empty operator secrets (provisioned only at deploy time / in CI). No static analysis or grep can substitute for the actual browser-driven assertions (`page.getByRole`, download event capture, route-interceptor hit count) that only fire when the harness successfully authenticates and reaches the live app.

## Gaps Summary

No BLOCKER-level gaps. The phase goal — a right-side Artefatti panel with per-file + bulk download, previews, and zero host/container path leakage — is achieved by the shipped code: every artifact exists, is substantive (not a stub), is wired end-to-end (list query → row → download anchor; SSE pump → cache invalidate → panel refetch → one-time auto-open; toggle → ResizablePanel/Drawer), and the security-critical rendering paths (HTML null-origin sandbox, xlsx empty-sandbox + escaping, SVG/pptx download-only, lazy-loaded heavy deps) were read directly and match the documented invariants. Coverage and test-count claims in the SUMMARYs were independently reproduced by re-running the suites in this verification pass, not merely trusted.

The one open item — WEBART-08's live Playwright e2e run — is a legitimate, honestly-documented carry-forward to the CI `web-e2e` job, not a fabricated pass or a code gap. REQUIREMENTS.md correctly reflects this (WEBART-05/06/07 `[x]`, WEBART-08 `[ ]`). This routes to `human_needed` rather than `gaps_found` per the verification decision tree, and rather than `passed` because the human-verification section is non-empty.

**Recommendation:** proceed with the CI `web-e2e` job run (or provision `AURA_AUTHULA_OPERATOR_EMAIL`/`_PASSWORD`/`_TOTP_SECRET` on a verification host) to close WEBART-08's final proof before milestone completion; no code changes are required first.

---

*Verified: 2026-07-09T00:05:33Z*
*Verifier: Claude (gsd-verifier)*
