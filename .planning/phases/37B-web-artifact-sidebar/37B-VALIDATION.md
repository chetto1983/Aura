---
phase: 37B
slug: web-artifact-sidebar
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-08
---

# Phase 37B — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> **Source of truth for the detailed strategy:** `37B-RESEARCH.md` → `## Validation Architecture`.
> Materialized post-planning against the final 8-plan / 17-task set (16 automated tasks + 1
> blocking-human package-legitimacy checkpoint — see Manual-Only).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | vitest 4.1.9 + React Testing Library 16.3 + jsdom 29 (web unit); Playwright 1.61 chromium + mobile-chrome (web e2e); Stryker 9.6.1 (mutation spot-check) |
| **Config file** | `web/vitest.config.ts` (unit + 85% coverage thresholds) · `web/playwright.config.ts` (e2e project matrix) · `web/vitest.stryker.config.ts` (mutation targets) |
| **Quick run command** | `cd web && npx vitest run <changed test file>` (e.g. `npx vitest run src/chat/artifacts/artifactMeta.test.ts`) |
| **Full suite command** | `cd web && npm test` (= `vitest run --coverage`, ≥85% src/**) then `cd web && npx playwright test` |
| **Mutation command** | `cd web && npx stryker run --config vitest.stryker.config.ts` (≥70% killed on artifactMeta.ts + downloadAll.ts) |
| **Estimated runtime** | quick single-file unit ~5–15s · full unit + coverage ~60s · Playwright e2e ~90s |

---

## Sampling Rate

- **After every task commit:** Run the quick web unit command for the task's test file (≤15s feedback)
- **After every plan wave:** Run the full web unit suite with coverage
- **Before `/gsd-verify-work`:** Full unit suite green + coverage ≥85% (WEBART-08) + Playwright e2e green + Stryker ≥70% on the two pure modules + `internal/webui/dist` rebuilt
- **Max feedback latency:** ~15 seconds (quick unit); ~90 seconds (full + e2e at wave boundaries)

---

## Per-Task Verification Map

*(One row per automated task. The plan 02 package-legitimacy gate (37B-02-01) is a blocking-human
checkpoint with no automated command — tracked under Manual-Only. STRIDE refs are drawn from each
plan's `<threat_model>`.)*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 37B-01-01 | 01 | 1 | WEBART-05,06,07,08 | T-37B-01, T-37B-02 | PRD records CVE-safe xlsx CDN source + null-origin HTML sandbox policy (no allow-same-origin) | static/grep | `grep -q "WEBART-05" prd.md && grep -q "WEBART-08" prd.md && grep -q "cdn.sheetjs.com" prd.md && grep -q "docx-preview" prd.md && grep -q "allow-scripts" prd.md` | ❌ W0 | ⬜ pending |
| 37B-02-02 | 02 | 2 | WEBART-05,07 | T-37B-03, T-37B-04 | xlsx installed ≥0.20.2 from CDN (not vulnerable npm 0.18.5); source_kind union type-checked | build/typecheck | `cd web && grep -q "cdn.sheetjs.com" package.json && grep -q "docx-preview" package.json && grep -q "'agent'" src/chat/attachments/types.ts && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-03-01 | 03 | 2 | WEBART-05,08 | T-37B-05 | previewKind gates SVG→download BEFORE image/* (XSS chokepoint); formatSize extraction non-regresses inline chip | unit (tdd) | `cd web && npx vitest run src/chat/artifacts/artifactMeta.test.ts && npx vitest run src/chat/displays/ && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-03-02 | 03 | 2 | WEBART-06 | T-37B-06, T-37B-07 | downloadAll uses id-only hrefs (no path leak), throttled, accepted-only, abortable | unit (tdd) | `cd web && npx vitest run src/chat/artifacts/downloadAll.test.ts` | ❌ W0 | ⬜ pending |
| 37B-03-03 | 03 | 2 | WEBART-05,06 | — (i18n/mutation registry) | en/it i18n parity holds; artifactMeta + downloadAll registered as Stryker targets | unit | `cd web && npx vitest run src/i18n/__tests__/resources.parity.test.ts && grep -q "artifactMeta" vitest.stryker.config.ts && grep -q "downloadAll" vitest.stryker.config.ts` | ❌ W0 | ⬜ pending |
| 37B-04-01 | 04 | 3 | WEBART-05 | T-37B-10, T-37B-11 | same-origin fetch; objectURL revoked + fetch aborted on unmount/id-change (no leak) | unit (tdd) | `cd web && npx vitest run src/chat/artifacts/useBlobPreview.test.ts` | ❌ W0 | ⬜ pending |
| 37B-04-02 | 04 | 3 | WEBART-05 | T-37B-08, T-37B-09, T-37B-05, T-37B-12 | HtmlPreview null-origin sandbox (allow-scripts, NO allow-same-origin); xlsx empty-sandbox; docx/xlsx lazy-only | unit (tdd, mocked) | `cd web && grep -q 'sandbox="allow-scripts"' src/chat/artifacts/renderers/HtmlPreview.tsx && ! grep -q "allow-same-origin" src/chat/artifacts/renderers/HtmlPreview.tsx && npx vitest run src/chat/artifacts/renderers/` | ❌ W0 | ⬜ pending |
| 37B-04-03 | 04 | 3 | WEBART-05 | T-37B-10, T-37B-08 | PreviewModal dispatches via previewKind to lazy renderers; download kinds get a download-only card | unit (tdd) | `cd web && grep -q "previewKind" src/chat/artifacts/PreviewModal.tsx && grep -q "w-\[90vw\]" src/chat/artifacts/PreviewModal.tsx && npx vitest run src/chat/artifacts/PreviewModal.test.tsx && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-05-01 | 05 | 3 | WEBART-07 | T-37B-15 | onArtifact fired at the streamSSE pump only; reduceFrame stays a pure reducer | unit (tdd) | `cd web && grep -q "onArtifact" src/chat/sseAdapter.ts && npx vitest run src/chat/sseAdapter.onArtifact.test.ts && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-05-02 | 05 | 3 | WEBART-07,08 | T-37B-13, T-37B-14 | split-fold by source_kind (agent→assistant, never user turn); id-only rehydrated download chip | unit (tdd) | `cd web && grep -q "foldAgentOntoAssistant" src/chat/ExternalStoreChat.tsx && grep -q "onArtifact" src/chat/ExternalStoreChat.tsx && npx vitest run src/chat/ExternalStoreChat.rehydration.test.tsx && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-06-01 | 06 | 4 | WEBART-05,07 | T-37B-17 | reuses identity-scoped listThreadAssets (IDOR-safe); agent-only newest-first select | unit (tdd) | `cd web && grep -q "listThreadAssets" src/chat/artifacts/useThreadArtifacts.ts && npx vitest run src/chat/artifacts/useThreadArtifacts.test.ts` | ❌ W0 | ⬜ pending |
| 37B-06-02 | 06 | 4 | WEBART-05,06 | T-37B-16 | ArtifactRow id-only download href; no object_key/object_bucket/host path in DOM (negative assert) | unit (tdd) | `cd web && npx vitest run src/chat/artifacts/ArtifactRow.test.tsx` | ❌ W0 | ⬜ pending |
| 37B-06-03 | 06 | 4 | WEBART-05,06,07 | T-37B-07, T-37B-16 | Panel lazy-imports PreviewModal (no dep bloat in panel chunk); Scarica-tutto disabled-during-run | unit (tdd) | `cd web && grep -q "downloadAll" src/chat/artifacts/ArtifactsPanel.tsx && grep -q "import('./PreviewModal')" src/chat/artifacts/ArtifactsPanel.tsx && npx vitest run src/chat/artifacts/ArtifactsPanel.test.tsx && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-07-01 | 07 | 5 | WEBART-05,07 | T-37B-18 | dynamic panelIds (no v4 key bump, no order prop); persisted 2-panel layout key untouched | unit (tdd) | `cd web && grep -q "chat-artifacts" src/AppShell.tsx && grep -q "aura-chat-shell-v3" src/AppShell.tsx && ! grep -q "aura-chat-shell-v4" src/AppShell.tsx && npx vitest run src/AppShell.artifacts.test.tsx && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-07-02 | 07 | 5 | WEBART-07,08 | T-37B-19, T-37B-17 | one-time-per-thread auto-open ref guard (reset on thread change); invalidate refetches identity-scoped list | unit (tdd) | `cd web && grep -q "onArtifact" src/AppShell.tsx && grep -q "invalidateQueries" src/AppShell.tsx && npx vitest run src/AppShell.artifacts.test.tsx && npx tsc --noEmit` | ❌ W0 | ⬜ pending |
| 37B-08-01 | 08 | 6 | WEBART-08 | T-37B-08 | golden-replay e2e: aura.artifact frame → panel row → download route hit; mobile Drawer no layout break | e2e (Playwright) | `cd web && npx playwright test e2e/artifacts.spec.ts` | ❌ W0 | ⬜ pending |
| 37B-08-02 | 08 | 6 | WEBART-08 | T-37B-20, T-37B-21, T-37B-08 | ≥85% coverage (no threshold-lowering / carve-out); ≥70% mutation; fresh embedded dist; go build green | coverage + mutation + build | `cd web && npm test && cd D:/Repo/Aura && go build ./...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*
*File Exists: ❌ W0 = test/source file created during execution (Wave 0 scaffolding not yet run).*

---

## Wave 0 Requirements

- [ ] Confirm `web/package.json` test stack (vitest 4.1.9 / RTL 16.3 + Playwright 1.61 + Stryker 9.6.1) + the quick/full commands above
- [ ] Confirm web coverage-gate config (`web/vitest.config.ts` src/** include + 85% thresholds) is the enforced floor
- [ ] Test stubs for WEBART-05..08 per RESEARCH.md `## Validation Architecture`
- [ ] Mock `docx-preview` / `xlsx` so lazy renderer chunks (37B-04-02) do not drag the coverage aggregate below 85% (Pitfall 4)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Package legitimacy gate — docx-preview, jszip, xlsx CDN tarball (task 37B-02-01) | WEBART-05,07 | Supply-chain approval of `[ASSUMED]` packages + an unusual `cdn.sheetjs.com` URL dependency; blocking-human, never auto-approvable (T-37B-SC) | Verify docx-preview (Apache-2.0) + jszip on npmjs.com; confirm `xlsx-0.20.3` on cdn.sheetjs.com is CVE-safe vs the frozen npm 0.18.5; approve the URL dependency before install |
| Visual parity vs Claude.ai reference screenshot (panel layout, row labels, toggle) | WEBART-05 | Pixel/visual judgment vs reference | Open a thread with delivered artifacts; compare panel against `D:\Immagini\Screenshots\Screenshot 2026-07-08 104850.png` |
| Mobile drawer parity (portal/backdrop/focus-trap/Esc/swipe) below `lg` | WEBART-05 | Cross-viewport interaction feel | Narrow viewport < lg; open Artefatti drawer; verify parity with the nav drawer |

---

## Validation Sign-Off

- [x] All automated tasks have a concrete `<automated>` verify command (16/16); the 1 checkpoint is tracked under Manual-Only
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (every automated task carries one)
- [ ] Wave 0 covers all MISSING references (test/source scaffolding created during execution)
- [x] No watch-mode flags (all commands are `vitest run` / `playwright test` one-shot)
- [x] Feedback latency < 15s (quick unit) / < 90s (full + e2e)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ready — materialized against the final 8-plan / 17-task set (16 automated + 1 blocking-human checkpoint). Wave 0 scaffolding pending execution.
</content>
