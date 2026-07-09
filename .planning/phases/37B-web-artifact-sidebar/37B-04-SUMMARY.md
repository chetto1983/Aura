---
phase: 37B-web-artifact-sidebar
plan: 04
subsystem: ui
tags: [webart, preview-modal, radix-dialog, iframe-sandbox, null-origin, docx-preview, sheetjs, lazy-chunk, blob-url, vitest, react]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery-lane
    provides: "GET /api/assets/{id}/download (identity-scoped, attachment/octet-stream) — the same-origin auth route every renderer + the header/download card fetch"
  - phase: 37B-web-artifact-sidebar (plan 03)
    provides: "artifactMeta.previewKind(mime,filename) — the SVG-gated-FIRST MIME→renderer gate the modal dispatches on (consumed, not re-derived)"
  - phase: 37B-web-artifact-sidebar (plan 02)
    provides: "docx-preview (Apache-2.0) + xlsx (SheetJS CE, CDN, CVE-safe) installed; Asset.source_kind widened to include 'agent'"
provides:
  - "useBlobPreview(assetId, mimeType?): fetch→relabel Blob→objectURL→revoke hook for the object-URL renderers (image/pdf)"
  - "PreviewModal({active,onClose}): ~90vw×90vh Radix Dialog dispatching previewKind → six Suspense-wrapped lazy renderer chunks, or a download-only card"
  - "renderers/{Image,Pdf,Text,Html,Docx,Xlsx}Preview.tsx: six default-export lazy boundaries; HTML null-origin sandbox, xlsx empty sandbox, docx/xlsx heavy deps confined to their chunks"
  - "renderers/PreviewStatus.tsx (shared loading/error + RendererProps) + renderers/useAssetContent.ts (shared same-origin fetch primitive)"
  - "artifacts.preview.{download,description,unsupported} i18n copy (en + it parity)"
affects: [37B-05, 37B-06, 37B-07, 37B-08]

# Tech tracking
tech-stack:
  added: []   # zero new deps — docx-preview/xlsx were installed in 37B-02; this plan only lazy-imports them
  patterns:
    - "Null-origin iframe isolation: sandbox='allow-scripts' WITHOUT the same-origin token, fed by srcDoc (fetched text), never src=downloadURL (T-37B-08)"
    - "Empty-sandbox iframe for parsed spreadsheet HTML (sandbox='') — inert frame as defense-in-depth over SheetJS cell escaping + our sheet-name escaping (T-37B-09)"
    - "Heavy deps behind dynamic import() INSIDE the renderer effect → their own lazy chunk, never the main/modal bundle (D-08)"
    - "Blob-URL lifecycle: AbortController + URL.revokeObjectURL in ONE cleanup keyed on the asset; render-time key gate hides a stale/revoked URL (T-37B-11)"
    - "Shared PreviewStatus + useAssetContent extracted so the six renderer chunks carry no duplicated fetch/chrome (jscpd threshold 0 / DRY)"

key-files:
  created:
    - web/src/chat/artifacts/useBlobPreview.ts
    - web/src/chat/artifacts/useBlobPreview.test.ts
    - web/src/chat/artifacts/PreviewModal.tsx
    - web/src/chat/artifacts/PreviewModal.test.tsx
    - web/src/chat/artifacts/renderers/ImagePreview.tsx
    - web/src/chat/artifacts/renderers/PdfPreview.tsx
    - web/src/chat/artifacts/renderers/TextPreview.tsx
    - web/src/chat/artifacts/renderers/HtmlPreview.tsx
    - web/src/chat/artifacts/renderers/DocxPreview.tsx
    - web/src/chat/artifacts/renderers/XlsxPreview.tsx
    - web/src/chat/artifacts/renderers/renderers.test.tsx
    - web/src/chat/artifacts/renderers/PreviewStatus.tsx   # DEVIATION (Rule 3): shared chrome + RendererProps (jscpd threshold 0)
    - web/src/chat/artifacts/renderers/useAssetContent.ts  # DEVIATION (Rule 3): shared fetch primitive (jscpd threshold 0)
  modified:
    - web/src/i18n/resources.display.ts

key-decisions:
  - "Every renderer takes the uniform RendererProps {assetId, mimeType, fileName} and does its OWN fetch (via useBlobPreview or useAssetContent) — the plan's RESEARCH examples took pre-fetched htmlText/blob, but self-fetching keeps the modal dispatch trivial and each chunk self-contained"
  - "Two shared helpers added beyond the plan's file list — PreviewStatus.tsx (loading/error + RendererProps) and useAssetContent.ts (text/blob/arrayBuffer fetch) — because the web jscpd gate FAILS on any ≥100-token clone (threshold 0) and four renderers would otherwise duplicate the identical fetch+abort block; CLAUDE.md also forbids duplication"
  - "useBlobPreview returns only the CURRENT [assetId,mimeType] key's result (render-time gate) instead of a synchronous setState reset — the codebase forbids setState-in-effect (react-hooks/set-state-in-effect); same render-derivation applied to useAssetContent"
  - "docx/xlsx dispose guard uses controller.signal.aborted (a property read), not a `let disposed` boolean — @typescript-eslint/no-unnecessary-condition narrows a re-assigned-in-closure boolean to a literal and false-flags `if (disposed)`"
  - "HtmlPreview source contains NO literal 'allow-same-origin' anywhere (comment reworded to 'the same-origin token') so the plan's `! grep -q allow-same-origin` acceptance holds against the file, not just the runtime attribute"

patterns-established:
  - "Pattern: a lazy per-MIME renderer = one default-export component + one lazy() boundary in PreviewModal; heavy deps dynamic-import()ed inside the effect"
  - "Pattern: untrusted-bytes isolation via null-origin (allow-scripts only) vs empty ('') sandbox chosen per content risk; download-only card when no safe renderer exists"

requirements-completed: []   # INTENTIONALLY EMPTY — WEBART-05/WEBART-08 are phase-spanning; per the 37B-01/37B-03 + 36-14..18 precedent they stay [ ] until the terminal acceptance plan (37B-08, Playwright e2e + aggregate). requirements mark-complete NOT run.

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "useBlobPreview fetches same-origin, relabels the octet-stream Blob with the SSE mime_type, exposes one objectURL, and revokes+aborts in one cleanup keyed on the asset (T-37B-11)"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/useBlobPreview.test.ts (6 tests: same-origin/relabel/no-mime/non-ok/unmount-revoke/rapid-change)"
        status: pass
    human_judgment: false
  - id: D2
    description: "HtmlPreview renders untrusted HTML in a NULL-ORIGIN iframe (sandbox='allow-scripts', no same-origin) via srcDoc, never src=downloadURL (T-37B-08 / WEBART-07)"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/renderers/renderers.test.tsx#HtmlPreview null-origin iframe (sandbox attrs + srcDoc-not-src)"
        status: pass
      - kind: other
        ref: "grep -q 'sandbox=\"allow-scripts\"' HtmlPreview.tsx && ! grep -q allow-same-origin HtmlPreview.tsx"
        status: pass
    human_judgment: false
  - id: D3
    description: "XlsxPreview parses with SheetJS (dynamic import) and renders the tables in an EMPTY-sandbox iframe with the sheet name escaped; docx-preview via dynamic import with className:'aura-docx' scoping (T-37B-09/T-37B-12, D-08)"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/renderers/renderers.test.tsx#Docx/Xlsx (mocked libs: renderAsync/read called; sandbox='' + escaped sheet name)"
        status: pass
    human_judgment: false
  - id: D4
    description: "PreviewModal (~90vw×90vh Radix Dialog) dispatches previewKind → six Suspense-wrapped lazy renderers or a download-only card (svg/pptx/unknown); header same-origin download anchor; onClose on dismiss"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/PreviewModal.test.tsx (13 tests: 6-row dispatch table + 3 download-only + 90vw/90vh + header href + onClose + closed-when-undefined)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Click-to-preview modal renders correctly against real agent files in a browser (visual/interaction fidelity across the six MIME kinds + the download card)"
    verification: []
    human_judgment: true
    rationale: "jsdom cannot exercise real iframe origin isolation, native PDF rendering, or docx/xlsx visual output — the live browser verification lands at the terminal Playwright plan (37B-08)"

# Metrics
duration: 35min
completed: 2026-07-09
status: complete
---

# Phase 37B Plan 04: Click-to-Preview Modal Summary

**A ~90vw×90vh Radix Dialog that dispatches by previewKind to six lazy per-MIME renderers — HTML in a null-origin `allow-scripts` iframe, xlsx tables in an empty sandbox, SVG/pptx download-only, and docx-preview/xlsx confined to their own dynamic-import chunks.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-08T23:40Z (approx, first task)
- **Completed:** 2026-07-09T00:15Z
- **Tasks:** 3 of 3
- **Files created:** 13 · **Files modified:** 1

## Accomplishments

- **useBlobPreview** (`f3ca15d1`): same-origin `fetch` of the 37A auth route, octet-stream Blob relabelled with the SSE `mime_type`, one `objectURL`, and `AbortController.abort()` + `URL.revokeObjectURL` in a single cleanup keyed on `[assetId, mimeType]`. A render-time key gate hides a stale/revoked URL during a rapid switch. 6 unit tests, all branches.
- **Six lazy renderers** (`35f27d94`): `Image`/`Pdf` (object URL → `<img>`/native `<iframe src>`), `Text` (React-escaped `<pre>`), `Html` (null-origin sandbox), `Docx` (docx-preview `renderAsync`), `Xlsx` (SheetJS `read`+`sheet_to_html`). `docx-preview` and `xlsx` are `import()`ed **inside** their effects so they land only in their own chunks. 16 tests mock the heavy libs.
- **PreviewModal** (`f38907f4`): Radix `Dialog` at `w-[90vw] h-[90vh]`, header = filename + same-origin download anchor + Radix close, `DialogDescription` for a11y, body dispatches `previewKind` to `Suspense`-wrapped `lazy()` renderers or a distinctive download-only card. 13 tests pin the full dispatch table.

## Security Invariants — how each is realized

| Invariant | Realization |
|-----------|-------------|
| HTML null-origin (T-37B-08) | `renderers/HtmlPreview.tsx`: `<iframe srcDoc={fetchedText} sandbox="allow-scripts">` — no same-origin token, no `src`. Text comes from `useAssetContent(assetId,'text')`, never the attachment download URL. |
| xlsx inert frame (T-37B-09) | `renderers/XlsxPreview.tsx`: `<iframe srcDoc={…} sandbox="">` (no allow-scripts). `sheet_to_html` escapes cell values; `escapeHtml()` escapes the sheet NAME before interpolation. |
| SVG/pptx download-only (T-37B-05) | `PreviewModal.renderKind` routes `previewKind==='download'` to `DownloadCard` — no renderer is mounted. `previewKind` (plan 03) already gates `image/svg+xml`→`download` before the image branch. |
| Heavy deps lazy (D-08) | `docx-preview` and `xlsx` are reached ONLY via `await import(...)` inside `DocxPreview`/`XlsxPreview` effects; the modal's static graph imports neither (verified by grep). Each renderer is a `lazy(() => import('./renderers/...'))` boundary. |
| Blob-URL no-leak (T-37B-11) | `useBlobPreview` cleanup calls `controller.abort()` + `URL.revokeObjectURL(objectUrl)` in the same `useEffect` return, keyed on `[assetId, mimeType]`; a rapid asset change revokes the prior URL before minting the next. |
| CSS scope (T-37B-12) | `DocxPreview` calls `renderAsync(blob, body, style, { className: 'aura-docx', … })` — injected rules are class-prefixed; cleanup clears the container `innerHTML`. |

## Deviations from Plan

### Auto-added (Rule 3 — blocking issue: jscpd threshold 0 forbids duplication)

**1. [Rule 3] Two shared helper modules beyond the plan's file list**
- **Found during:** Task 2 (six renderers)
- **Issue:** The web `dup` gate runs jscpd with `threshold: 0` (fails on ANY ≥100-token clone); four renderers self-fetching would duplicate the identical `fetch(...credentials:'same-origin'...) + AbortController` block, and all six would duplicate the loading/error chrome. CLAUDE.md also forbids duplication.
- **Fix:** Extracted `renderers/useAssetContent.ts` (shared text/blob/arrayBuffer fetch primitive) and `renderers/PreviewStatus.tsx` (shared `PreviewLoading`/`PreviewError` + the `RendererProps` type). Both are dep-light and dedupe across chunks.
- **Files:** `web/src/chat/artifacts/renderers/useAssetContent.ts`, `web/src/chat/artifacts/renderers/PreviewStatus.tsx`
- **Commit:** `35f27d94`

### Adjustments for CLAUDE.md / lint invariants (no behavior change)

**2. [Rule 3] Render-time key gate instead of synchronous setState reset**
- The hook/primitive would naturally `setUrl(undefined)` at effect start to clear a stale value, but `react-hooks/set-state-in-effect` (enforced, `--max-warnings=0`) forbids synchronous setState in an effect body. Realized the same "no stale value" guarantee by returning only the CURRENT key's result at render time (`useBlobPreview.ts`, `useAssetContent.ts`).

**3. [Rule 3] `controller.signal.aborted` dispose guard in docx/xlsx**
- A `let disposed = false` + `if (disposed) return` after `await` false-trips `@typescript-eslint/no-unnecessary-condition` (it narrows the literal). Switched both dynamic-import renderers to a local `AbortController` and `if (controller.signal.aborted) return` — a property read the rule cannot narrow.

**4. [Rule 3] HtmlPreview comment reworded to omit the literal `allow-same-origin`**
- The plan's acceptance is `! grep -q "allow-same-origin" HtmlPreview.tsx` (against the whole file). Reworded the doc comment to "the same-origin token" so the literal appears nowhere in the file; the runtime attribute is `sandbox="allow-scripts"`.

No architectural (Rule 4) changes. No auth gates. No package installs (docx-preview/xlsx shipped in 37B-02).

## Validation Results

- `npx tsc --noEmit` — clean.
- `npx vitest run src/chat/artifacts/` — new files: useBlobPreview (6), renderers (16), PreviewModal (13) all green.
- **Full suite:** 1109 tests passed (132 files); coverage **Statements 91.56% / Branches 85.93% / Functions 91.88% / Lines 93.37%** — all ≥85% (EXIT 0). PreviewModal 100% stmts; DocxPreview 96% / XlsxPreview 93% (only the irreducible mid-await abort branch uncovered).
- `eslint --max-warnings=0` + `prettier --check` — clean on all 14 touched files.
- Grep gates: `sandbox="allow-scripts"` present in HtmlPreview; `allow-same-origin` absent; `docx-preview`/`xlsx` imported only in DocxPreview/XlsxPreview; `previewKind` + `w-[90vw]` present in PreviewModal.
- i18n parity (en↔it) test green (added preview keys in both locales).
- Pre-commit hooks (jscpd/vet/file-size) green on all three task commits — no `--no-verify`.

## Deferred / Known Stubs

None. Every renderer is wired to real data (the 37A download route); the modal is not yet mounted by a panel — that is 37B-06's job (this plan produces the surface, `affects: 37B-06`). No stubs, no placeholder data.

## Commits

- `f3ca15d1` feat(37B-04): useBlobPreview hook — same-origin fetch, Blob relabel, objectURL lifecycle
- `35f27d94` feat(37B-04): six lazy per-MIME preview renderers (image/pdf/text/html/docx/xlsx)
- `f38907f4` feat(37B-04): PreviewModal — Radix Dialog dispatch to lazy per-MIME renderers

## Self-Check: PASSED

All 14 files (13 created + 1 modified) verified on disk; all three task commits (`f3ca15d1`, `35f27d94`, `f38907f4`) present in git history.
