---
phase: 37B-web-artifact-sidebar
plan: 04
type: execute
wave: 3
depends_on: ["37B-02", "37B-03"]
files_modified:
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
autonomous: true
requirements: [WEBART-05, WEBART-08]
must_haves:
  truths:
    - "PreviewModal is a Radix Dialog sized ~90vw/90vh with header = filename + download anchor + close, and dispatches to a lazy per-MIME renderer via previewKind (D-09)"
    - "useBlobPreview fetches same-origin, re-labels the Blob with the SSE mime_type, and revokes the objectURL + aborts on unmount (D-06)"
    - "HtmlPreview renders a null-origin iframe: sandbox='allow-scripts' with NO allow-same-origin, fed by srcDoc (fetched text), never src=download URL"
    - "docx-preview + SheetJS are imported ONLY inside their lazy renderer chunks (never the main bundle)"
    - "xlsx table renders inside an empty-sandbox iframe (sheet name escaped); pptx/svg/unknown render a download-only card"
  artifacts:
    - path: "web/src/chat/artifacts/PreviewModal.tsx"
      provides: "click-to-preview modal with per-MIME dispatch + loading/error"
      min_lines: 40
    - path: "web/src/chat/artifacts/useBlobPreview.ts"
      provides: "fetch→relabel Blob→objectURL→revoke hook"
      min_lines: 20
    - path: "web/src/chat/artifacts/renderers/HtmlPreview.tsx"
      provides: "null-origin sandboxed HTML iframe"
      contains: "allow-scripts"
  key_links:
    - from: "web/src/chat/artifacts/PreviewModal.tsx"
      to: "web/src/chat/artifacts/artifactMeta.ts"
      via: "previewKind dispatch"
      pattern: "previewKind"
    - from: "web/src/chat/artifacts/renderers/DocxPreview.tsx"
      to: "docx-preview"
      via: "lazy import renderAsync"
      pattern: "docx-preview"
  prohibitions:
    - "MUST NOT add allow-same-origin to the HTML sandbox iframe (would let sandboxed script escape the sandbox)"
    - "MUST NOT point an <iframe src> at the download URL for HTML — the attachment disposition forces a download; use fetch()→srcDoc"
    - "MUST NOT statically import docx-preview or xlsx anywhere reachable from the main bundle — lazy chunks only (D-08)"
    - "MUST NOT render SVG as a blob <img> — SVG never reaches a renderer (previewKind returns 'download')"
    - "MUST NOT leak object URLs — revoke + abort in the same useEffect cleanup keyed on assetId"
---

<objective>
Build the click-to-preview surface: a Radix-`Dialog` modal (~90vw/90vh) that dispatches by `previewKind` to six lazy-loaded per-MIME renderers, plus the `useBlobPreview` hook (fetch → relabel Blob → objectURL → revoke). This is the marquee WEBART-05 feature and the security-critical surface: null-origin sandboxed HTML, empty-sandbox xlsx table, SVG/pptx download-only, and heavy deps confined to their chunks.

Purpose: Preview agent-delivered files in-cockpit without exposing host paths, weakening 37A's XSS guard, or bloating the main bundle.
Output: `PreviewModal.tsx`, `useBlobPreview.ts`, six `renderers/*.tsx` lazy chunks, tests (docx/xlsx mocked to protect the 85% aggregate).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md
@web/src/components/ui/dialog.tsx
@web/src/AppShell.tsx
</context>

<artifacts_produced>
This plan produces:
- `web/src/chat/artifacts/useBlobPreview.ts` → `useBlobPreview(assetId, mimeType?)` → `{ url?, error? }` (object-URL kinds).
- `web/src/chat/artifacts/PreviewModal.tsx` → `PreviewModal` component, props `{ active: Asset | undefined; onClose: () => void }`, dispatches via `previewKind`.
- `web/src/chat/artifacts/renderers/{ImagePreview,PdfPreview,TextPreview,HtmlPreview,DocxPreview,XlsxPreview}.tsx` → default-export components, each a single `React.lazy` boundary.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: useBlobPreview hook — same-origin fetch, Blob relabel, objectURL lifecycle</name>
  <files>web/src/chat/artifacts/useBlobPreview.ts, web/src/chat/artifacts/useBlobPreview.test.ts</files>
  <behavior>
    - fetch is called with credentials:'same-origin' and the abort signal
    - on a non-ok response the hook exposes an error (no url)
    - the fetched Blob is re-wrapped with the provided mimeType (relabel; server serves octet-stream)
    - URL.createObjectURL is called once and its return is exposed as url
    - on unmount: controller.abort() AND URL.revokeObjectURL(url) both fire (mocked) — keyed on assetId
    - a rapid assetId change revokes the previous url before creating the next
  </behavior>
  <read_first>
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 5 — Blob-URL lifecycle" — the exact fetch+relabel+revoke recipe.
    - web/src/chat/ExternalStoreChat.tsx:287,319-321 — the AbortController-in-cleanup convention to mirror.
    - web/src/chat/attachments/api.ts:47-57 — the `credentials:'same-origin'` fetch convention.
  </read_first>
  <action>
    Create `web/src/chat/artifacts/useBlobPreview.ts`: a hook `useBlobPreview(assetId, mimeType?)` that, in a `useEffect` keyed on `[assetId, mimeType]`, creates an `AbortController`, `fetch(`/api/assets/${assetId}/download`, { credentials:'same-origin', signal })`, throws on `!res.ok`, `res.blob()`, re-wraps as `new Blob([raw], { type: mimeType })` when `mimeType` is set, `URL.createObjectURL`, sets `url`; cleanup calls `controller.abort()` and `URL.revokeObjectURL(objectUrl)`. Returns `{ url, error }`. Used by ImagePreview + PdfPreview only (docx/xlsx/text/html consume bytes/text directly). Write `useBlobPreview.test.ts` mocking `fetch`, `URL.createObjectURL`, `URL.revokeObjectURL`, asserting every `<behavior>` case.
  </action>
  <acceptance_criteria>
    - `useBlobPreview.ts` fetches with `credentials: 'same-origin'` and relabels the Blob with `mimeType`.
    - test asserts `URL.revokeObjectURL` + `abort` fire on unmount and on assetId change.
    - `cd web && npx vitest run src/chat/artifacts/useBlobPreview.test.ts` exits 0 at 100% for the hook.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/artifacts/useBlobPreview.test.ts && echo USEBLOB_OK</automated>
  </verify>
  <done>Hook fetches same-origin, relabels the Blob, and revokes+aborts on cleanup; test 100%.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Six lazy per-MIME renderers (image/pdf/text/html/docx/xlsx)</name>
  <files>web/src/chat/artifacts/renderers/ImagePreview.tsx, web/src/chat/artifacts/renderers/PdfPreview.tsx, web/src/chat/artifacts/renderers/TextPreview.tsx, web/src/chat/artifacts/renderers/HtmlPreview.tsx, web/src/chat/artifacts/renderers/DocxPreview.tsx, web/src/chat/artifacts/renderers/XlsxPreview.tsx, web/src/chat/artifacts/renderers/renderers.test.tsx</files>
  <behavior>
    - ImagePreview renders <img src={objectURL}> from useBlobPreview
    - PdfPreview renders <iframe src={objectURL}> (native PDF)
    - TextPreview renders res.text() inside <pre> (React-escaped, no markdown)
    - HtmlPreview renders <iframe srcDoc={text} sandbox="allow-scripts"> with NO allow-same-origin
    - DocxPreview calls docx-preview.renderAsync(blob, bodyRef, styleRef, { className:'aura-docx', ... }); on reject shows an error card (docx-preview MOCKED in test — assert called with blob + className)
    - XlsxPreview calls SheetJS read(arrayBuffer,{type:'array'}) + sheet_to_html per sheet, rendered inside an empty-sandbox iframe with the sheet NAME escaped (xlsx MOCKED in test)
    - each renderer surfaces a loading + error state
  </behavior>
  <read_first>
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Code Examples" (HtmlPreview null-origin iframe, XlsxPreview empty-sandbox iframe + escapeHtml sheet name, DocxPreview renderAsync) + "Standard Stack" (docx-preview/SheetJS API signatures).
    - web/src/AppShell.tsx:29-47 — the `lazy(() => import(...))` + `<Suspense>` split precedent (each renderer is one lazy boundary).
    - web/src/chat/artifacts/artifactMeta.ts — `previewKind` (from plan 03) that decides which renderer to mount.
  </read_first>
  <action>
    Create the six files under `web/src/chat/artifacts/renderers/`, each a default-export React component and a single lazy boundary: `ImagePreview` (`useBlobPreview` → `<img>`), `PdfPreview` (`useBlobPreview` → native `<iframe src>`), `TextPreview` (fetch text → `<pre>{text}</pre>`), `HtmlPreview` (fetch text → `<iframe srcDoc={text} sandbox="allow-scripts" title=...>` — null origin, NO `allow-same-origin`, NO `src`), `DocxPreview` (fetch Blob → `docx-preview` `renderAsync(blob, bodyRef, styleRef, { className:'aura-docx', inWrapper:true, ignoreLastRenderedPageBreak:true })`, cleanup clears `innerHTML`), `XlsxPreview` (fetch ArrayBuffer → SheetJS `read(...,{type:'array'})` + `sheet_to_html` per `SheetNames`, escape the sheet NAME, render the concatenated table string inside `<iframe srcDoc={...} sandbox="" title=...>`). Keep DOM-injection glue minimal. Write `renderers/renderers.test.tsx` that MOCKS `docx-preview` and `xlsx` (`vi.mock`) — assert each is called with the right args + loading/error render + the HtmlPreview sandbox attributes (`allow-scripts`, absence of `allow-same-origin`). Wrap any irreducible untestable DOM glue in `/* v8 ignore */` to protect the 85% aggregate (Pitfall 4).
  </action>
  <acceptance_criteria>
    - `HtmlPreview.tsx` contains `sandbox="allow-scripts"` and does NOT contain `allow-same-origin`; uses `srcDoc`, not `src`.
    - `XlsxPreview.tsx` renders inside a `sandbox=""` iframe and escapes the sheet name.
    - `DocxPreview.tsx` + `XlsxPreview.tsx` are the ONLY files importing `docx-preview` / `xlsx` (grep the artifacts tree).
    - test mocks `docx-preview` + `xlsx` and asserts they are called; `cd web && npx vitest run src/chat/artifacts/renderers/` exits 0.
    - `cd web && npx vitest run src/chat/artifacts/renderers/ --coverage` keeps the renderers dir from dropping the aggregate under 85% (mocked libs + thin glue).
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q 'sandbox="allow-scripts"' src/chat/artifacts/renderers/HtmlPreview.tsx && ! grep -q "allow-same-origin" src/chat/artifacts/renderers/HtmlPreview.tsx && npx vitest run src/chat/artifacts/renderers/ && echo RENDERERS_OK</automated>
  </verify>
  <done>Six lazy renderers built; HTML is null-origin sandboxed; docx/xlsx confined to their chunks + mocked in tests; all renderer tests green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: PreviewModal — Radix Dialog dispatch to lazy renderers</name>
  <files>web/src/chat/artifacts/PreviewModal.tsx, web/src/chat/artifacts/PreviewModal.test.tsx</files>
  <behavior>
    - When active is set, a Dialog opens sized 90vw/90vh with the filename in the header + a download anchor to /api/assets/{id}/download + close
    - previewKind(mime, filename) selects the renderer; 'download' kinds (pptx/svg/unknown) render a download-only card, NOT a renderer
    - renderers are Suspense-wrapped lazy imports (docx-preview/xlsx never in the modal's static import graph)
    - onOpenChange(false) → onClose; Esc/backdrop close via Radix
    - a DialogDescription/aria-describedby is present (no Radix a11y warning)
  </behavior>
  <read_first>
    - web/src/components/ui/dialog.tsx — the Radix wrapper (export surface, `max-w-lg` default at :51 to override via `cn()` className, z-index layering).
    - .planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md "PreviewModal" + RESEARCH "Pattern 4".
    - web/src/chat/displays/LocalArtifactDisplay.tsx:65-89 — the download anchor to reuse in the modal header.
  </read_first>
  <action>
    Create `web/src/chat/artifacts/PreviewModal.tsx`: `PreviewModal({ active, onClose })` rendering `<Dialog open={!!active} onOpenChange={(o) => !o && onClose()}>` with `<DialogContent className="h-[90vh] w-[90vw] max-w-[90vw] gap-0 p-0">`, a header (`DialogTitle` = filename truncate + the `/api/assets/{id}/download` anchor + built-in close), a `DialogDescription` for a11y, and a body that computes `previewKind(active.mime_type, active.file_name)` and renders the matching `lazy(() => import('./renderers/...'))` under `<Suspense fallback=...>`, or a download-only card for `'download'`. Pass the SSE `mime_type` into the object-URL renderers. Write `PreviewModal.test.tsx` (mock the lazy renderers) asserting the dispatch table (each previewKind → right renderer / download card), the 90vw/90vh className, the header download href, and onClose on close.
  </action>
  <acceptance_criteria>
    - `PreviewModal.tsx` imports `previewKind` and each renderer via `lazy(() => import('./renderers/...'))` (no static renderer import that pulls docx-preview/xlsx into the modal chunk).
    - `DialogContent` className includes `w-[90vw]` and `h-[90vh]`.
    - header anchor href is `/api/assets/${active.id}/download`.
    - `cd web && npx vitest run src/chat/artifacts/PreviewModal.test.tsx` exits 0.
    - `cd web && npx tsc --noEmit` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "previewKind" src/chat/artifacts/PreviewModal.tsx && grep -q "w-\[90vw\]" src/chat/artifacts/PreviewModal.tsx && npx vitest run src/chat/artifacts/PreviewModal.test.tsx && npx tsc --noEmit && echo PREVIEWMODAL_OK</automated>
  </verify>
  <done>PreviewModal dispatches by previewKind to Suspense-wrapped lazy renderers, sized 90vw/90vh, header download + close; test green.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| agent-produced file bytes → browser renderer | Untrusted HTML/SVG/spreadsheet content must not execute in our origin or read our session |
| lazy dep bytes → main bundle | Heavy third-party libs must not enter the cache-hot main chunk |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-08 | Elevation (stored XSS via untrusted HTML) | HtmlPreview iframe | mitigate | `sandbox="allow-scripts"` with NO `allow-same-origin` (null origin) fed by `srcDoc`; script runs but has empty cookie, cross-origin parent, no ambient session — cannot reach cookies/DOM/Garage |
| T-37B-09 | Tampering (XSS via spreadsheet cell) | XlsxPreview | mitigate | SheetJS escapes cell values (A1) + render inside empty-`sandbox` iframe (no script exec) + escape the sheet name — defense-in-depth |
| T-37B-05 | Tampering / Elevation (SVG script) | renderer dispatch | mitigate | SVG never reaches a renderer — `previewKind` (plan 03) returns `'download'`; PreviewModal shows a download-only card |
| T-37B-10 | Information Disclosure (session ride on preview) | useBlobPreview / TextPreview / HtmlPreview fetch | mitigate | Bytes render only in blob/iframe/DOM, never as a top-level document from our origin; `src` never points at the `attachment` download URL for HTML |
| T-37B-11 | Denial of Service (memory) | blob-URL lifecycle | mitigate | `AbortController` + `URL.revokeObjectURL` in the same cleanup keyed on assetId (Pitfall 5) |
| T-37B-12 | Tampering (CSS bleed) | DocxPreview injected styles | mitigate | `className: 'aura-docx'` prefixes all injected rules (class-scoped, no `document.head` leak) |
</threat_model>

<verification>
- `npx vitest run src/chat/artifacts/` (this plan's files) green; docx-preview/xlsx mocked, renderers dir does not drag the aggregate <85%.
- `grep 'allow-scripts' HtmlPreview.tsx` present, `allow-same-origin` absent.
- `docx-preview`/`xlsx` imported only in `DocxPreview.tsx`/`XlsxPreview.tsx`.
</verification>

<success_criteria>
- Click-to-preview modal dispatches by previewKind to six lazy renderers with correct loading/error and 90vw/90vh sizing.
- HTML preview is null-origin sandboxed; xlsx table is empty-sandboxed; SVG/pptx are download-only; blob URLs are revoked.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-04-SUMMARY.md` when done.
</output>
</content>
