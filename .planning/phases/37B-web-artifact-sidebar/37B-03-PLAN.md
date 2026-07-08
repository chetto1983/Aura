---
phase: 37B-web-artifact-sidebar
plan: 03
type: execute
wave: 2
depends_on: ["37B-01"]
files_modified:
  - web/src/chat/artifacts/artifactMeta.ts
  - web/src/chat/artifacts/artifactMeta.test.ts
  - web/src/chat/artifacts/downloadAll.ts
  - web/src/chat/artifacts/downloadAll.test.ts
  - web/src/chat/displays/LocalArtifactDisplay.tsx
  - web/src/i18n/resources.display.ts
  - web/vitest.stryker.config.ts
autonomous: true
requirements: [WEBART-05, WEBART-06, WEBART-08]
must_haves:
  truths:
    - "previewKind(mime, filename) returns 'download' for SVG (gated FIRST), and the correct kind for image/pdf/text/html/docx/xlsx"
    - "category label + lucide icon are derived from mime/extension per the CitationBubble precedent"
    - "downloadAll sequences one <a download> per accepted asset, throttled, skipping degraded rows, reporting N/M"
    - "formatSize lives in artifactMeta.ts and is imported by LocalArtifactDisplay.tsx (no duplication) — inline chip still renders identically"
    - "artifacts.* i18n keys exist in BOTH en and it resource modules"
  artifacts:
    - path: "web/src/chat/artifacts/artifactMeta.ts"
      provides: "previewKind, categoryLabel, categoryIcon, formatSize, PreviewKind type"
      min_lines: 40
    - path: "web/src/chat/artifacts/downloadAll.ts"
      provides: "downloadAll sequential throttled downloader"
      min_lines: 15
  key_links:
    - from: "web/src/chat/displays/LocalArtifactDisplay.tsx"
      to: "web/src/chat/artifacts/artifactMeta.ts"
      via: "import formatSize"
      pattern: "from '.*artifacts/artifactMeta'"
  prohibitions:
    - "MUST NOT render markdown/HTML in the text path — previewKind text family is raw <pre> only (zero injection surface)"
    - "MUST NOT let previewKind return 'image' for image/svg+xml — SVG is download-only (XSS exclusion), gated before the image/* branch"
    - "MUST NOT duplicate formatSize — extract from LocalArtifactDisplay and import it back (refactor-on-touch, DRY)"
    - "MUST NOT add artifacts.* keys to only one locale — the parity test fails unless en AND it both have them"
---

<objective>
Build the pure, 100%-testable foundation modules the panel/preview surfaces consume: the MIME gate + category/icon/label map (`artifactMeta.ts`), the sequential throttled downloader (`downloadAll.ts`), the shared `formatSize` (extracted from `LocalArtifactDisplay.tsx` — refactor-on-touch, also a WEBART-08 non-regression touch), and the `artifacts.*` i18n keys (en+it). These are the mutation-test targets; keeping the decision logic pure protects the 85% coverage aggregate against the lazy-renderer drag (RESEARCH Pitfall 4).

Purpose: Provide deterministic, mutation-hardened logic that every downstream 37B UI plan imports.
Output: `artifactMeta.ts` + `downloadAll.ts` (+ tests) at ~100%, `formatSize` shared, `artifacts.*` copy in both locales, both pure modules added to the Stryker list.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md
@web/src/chat/displays/CitationBubble.tsx
@web/src/chat/displays/LocalArtifactDisplay.tsx
</context>

<artifacts_produced>
This plan produces:
- `web/src/chat/artifacts/artifactMeta.ts` → `type PreviewKind = 'image'|'pdf'|'text'|'html'|'docx'|'xlsx'|'download'`; `previewKind(mime, filename): PreviewKind`; `categoryLabel(mime, filename): string` (i18n key producer, e.g. "Documento · MD"); `categoryIcon(mime, filename): LucideIcon`; `formatSize(bytes, t): string` (moved here).
- `web/src/chat/artifacts/downloadAll.ts` → `downloadAll(assets, onProgress, opts?)`.
- i18n keys under `artifacts.*` and category keys (en + it).
- Stryker `mutationTests` entries for `artifactMeta.test.ts` + `downloadAll.test.ts`.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: artifactMeta.ts — previewKind + category label/icon + extracted formatSize</name>
  <files>web/src/chat/artifacts/artifactMeta.ts, web/src/chat/artifacts/artifactMeta.test.ts, web/src/chat/displays/LocalArtifactDisplay.tsx</files>
  <behavior>
    - previewKind('image/svg+xml', 'x.svg') === 'download'  (SVG gated FIRST)
    - previewKind('image/png', 'x.png') === 'image'
    - previewKind('application/pdf', 'x.pdf') === 'pdf'; previewKind('application/octet-stream', 'x.pdf') === 'pdf' (ext fallback)
    - previewKind('text/html', 'x.html') === 'html'; previewKind('', 'x.htm') === 'html'
    - previewKind('', 'x.docx') === 'docx'; previewKind('', 'x.xlsx') === 'xlsx'
    - previewKind('text/plain', 'x.txt') === 'text'; previewKind('', 'x.md'|'x.csv'|'x.json'|'x.sh'|'x.yaml'|'x.go') === 'text'
    - previewKind('application/vnd.ms-powerpoint', 'x.pptx') === 'download' (no free renderer)
    - categoryLabel produces a friendly category word + uppercase extension (e.g. "Documento · MD", "Foglio · XLSX", "Immagine · PNG", "Codice · SH") via i18n keys
    - categoryIcon returns the mapped lucide icon (FileText/FileSpreadsheet/FileImage/FileCode/Code2/Globe) with `File` fallback
    - formatSize matches the pre-extraction LocalArtifactDisplay output byte-for-byte across B/KB/MB/GB buckets
  </behavior>
  <read_first>
    - web/src/chat/displays/CitationBubble.tsx:2,27-37 — the `Partial<Record<..., LucideIcon>>` + `File` fallback icon-map precedent (mirror this shape).
    - web/src/chat/displays/LocalArtifactDisplay.tsx:22-32 — the `formatSize` implementation + its i18n size keys to move verbatim.
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Code Examples — previewKind" — the exact pure gate to implement (SVG-first ordering, TEXT_EXT set).
  </read_first>
  <action>
    Create `web/src/chat/artifacts/artifactMeta.ts` exporting `PreviewKind`, `previewKind(mime, filename)` (SVG/`image/svg+xml`→`'download'` gated BEFORE the `image/*` branch; then image, pdf, html, docx, xlsx, text-family via a `TEXT_EXT` set + `text/*` prefix; default `'download'`), `categoryLabel(mime, filename)` (returns a localized "Categoria · EXT" string via new `artifacts.category.*` keys + uppercased extension), `categoryIcon(mime, filename)` (a `Partial<Record<...>>` map to `FileText`/`FileSpreadsheet`/`FileImage`/`FileCode`/`Code2`/`Globe` from lucide-react with `File` fallback), and `formatSize(bytes, t)` MOVED from LocalArtifactDisplay. Then edit `web/src/chat/displays/LocalArtifactDisplay.tsx` to delete its local `formatSize` and import it from `artifactMeta.ts` (leave the chip's rendered output unchanged — WEBART-08 non-regression). Write `artifactMeta.test.ts` covering every `<behavior>` case to 100%.
  </action>
  <acceptance_criteria>
    - `artifactMeta.ts` exports `previewKind`, `categoryLabel`, `categoryIcon`, `formatSize`, `PreviewKind`.
    - `LocalArtifactDisplay.tsx` imports `formatSize` from `../artifacts/artifactMeta` and defines it no longer locally.
    - `cd web && npx vitest run src/chat/artifacts/artifactMeta.test.ts` exits 0.
    - `cd web && npx vitest run src/chat/artifacts/artifactMeta.test.ts --coverage` reports 100% for `artifactMeta.ts` (or the file is at 100% in the aggregate run).
    - Existing `LocalArtifactDisplay` test(s) stay green: `cd web && npx vitest run src/chat/displays/`.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/artifacts/artifactMeta.test.ts && npx vitest run src/chat/displays/ && npx tsc --noEmit && echo ARTIFACTMETA_OK</automated>
  </verify>
  <done>previewKind SVG-gated + all MIME cases pass; category label/icon mapped; formatSize shared with LocalArtifactDisplay; tests 100%; inline chip unchanged.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: downloadAll.ts — sequential throttled downloader (mutation target)</name>
  <files>web/src/chat/artifacts/downloadAll.ts, web/src/chat/artifacts/downloadAll.test.ts</files>
  <behavior>
    - downloadAll([a1,a2,a3]) triggers 3 `HTMLAnchorElement.click()` calls, each href `/api/assets/{id}/download`, download={file_name}
    - a degraded asset (status !== 'accepted') is skipped and NOT clicked
    - onProgress fires (done, total) with total = count of accepted rows; final call is (N, N)
    - between clicks a ~500ms delay elapses (assert with fake timers); no delay after the last
    - an aborted signal stops the loop early
  </behavior>
  <read_first>
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 7 — Scarica tutto" — the exact recipe (filter accepted, delay 400–600ms, createElement('a')+click, N/M, abortable).
    - web/src/chat/displays/LocalArtifactDisplay.tsx:65-89 — the `/api/assets/{id}/download` + `download={filename}` anchor convention to replicate in the loop.
    - web/src/test/setup.ts — the test harness (fake-timer + jsdom conventions).
  </read_first>
  <action>
    Create `web/src/chat/artifacts/downloadAll.ts` exporting `downloadAll(assets, onProgress, opts?: { delayMs?: number; signal?: AbortSignal })`: filter `status === 'accepted'`, for each create an `<a href="/api/assets/${id}/download" download={file_name}>`, append/click/remove, call `onProgress(i+1, total)`, `await` `opts.delayMs ?? 500` between (not after last), break on `opts.signal?.aborted`. Pure/DOM-only — no React. Write `downloadAll.test.ts` with fake timers + a spy on `HTMLAnchorElement.prototype.click` covering every `<behavior>` case to 100%.
  </action>
  <acceptance_criteria>
    - `downloadAll.ts` exports `downloadAll`; hrefs target `/api/assets/{id}/download`; degraded rows skipped.
    - `cd web && npx vitest run src/chat/artifacts/downloadAll.test.ts` exits 0 with `downloadAll.ts` at 100%.
    - No host/container path or `object_key`/`object_bucket` appears in any generated href (negative assertion in the test).
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/artifacts/downloadAll.test.ts && echo DOWNLOADALL_OK</automated>
  </verify>
  <done>downloadAll sequences accepted rows, throttled, skips degraded, reports N/M, abortable; test at 100%.</done>
</task>

<task type="auto">
  <name>Task 3: artifacts.* i18n keys (en+it) + register pure modules with Stryker</name>
  <files>web/src/i18n/resources.display.ts, web/vitest.stryker.config.ts</files>
  <read_first>
    - web/src/i18n/resources.display.ts — the en+it resource structure to extend (match the existing `display.artifact.*` nesting).
    - web/src/i18n/__tests__/resources.parity.test.ts — the parity test that enforces en/it key symmetry (will fail if a key is added to only one locale).
    - web/vitest.stryker.config.ts — the `mutationTests` curated list to append to.
  </read_first>
  <action>
    Extend `web/src/i18n/resources.display.ts` (both en and it) with the `artifacts.*` keys the panel/preview/downloadAll surfaces need: at minimum `artifacts.title` ("Artefatti"), `artifacts.downloadAll`, `artifacts.downloadAllProgress` (with `{done}`/`{total}`), `artifacts.empty` + `artifacts.emptyHint`, `artifacts.toggleAria`, `artifacts.category.document`/`.spreadsheet`/`.image`/`.code`/`.text`/`.file`, `artifacts.preview.loading`/`.error`/`.downloadFallback`, and `shell.resizeArtifacts`. Then append `src/chat/artifacts/artifactMeta.test.ts` and `src/chat/artifacts/downloadAll.test.ts` (with their source files) to the Stryker `mutationTests` list in `web/vitest.stryker.config.ts` targeting ≥70% killed.
  </action>
  <acceptance_criteria>
    - `cd web && npx vitest run src/i18n/__tests__/resources.parity.test.ts` exits 0 (en/it symmetry holds with the new keys).
    - `web/vitest.stryker.config.ts` references `artifactMeta` and `downloadAll` in its mutation targets.
    - `grep -q "artifacts" web/src/i18n/resources.display.ts` and both locale blocks contain the new keys.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/i18n/__tests__/resources.parity.test.ts && grep -q "artifactMeta" vitest.stryker.config.ts && grep -q "downloadAll" vitest.stryker.config.ts && echo I18N_STRYKER_OK</automated>
  </verify>
  <done>artifacts.* keys present in en+it (parity green); artifactMeta + downloadAll registered as Stryker mutation targets.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| agent-file MIME/filename → preview routing | An untrusted mime/filename must not route a script-bearing file (SVG) into an executing renderer |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-05 | Tampering / Elevation (stored XSS via SVG) | previewKind routing | mitigate | SVG/`image/svg+xml` gated to `'download'` FIRST, before the `image/*` branch — the pure gate is the single chokepoint, mutation-tested |
| T-37B-06 | Information Disclosure (path leak) | downloadAll href construction | mitigate | hrefs use only `id` → `/api/assets/{id}/download`; never `object_key`/`object_bucket`/host path (negative test assertion) |
| T-37B-07 | Denial of Service (self, multi-download burst) | downloadAll loop | mitigate | ~500ms throttle + accepted-only filter + abortable signal |
</threat_model>

<verification>
- `npx vitest run src/chat/artifacts/` green with `artifactMeta.ts` + `downloadAll.ts` at 100%.
- i18n parity test green; Stryker list updated.
- `LocalArtifactDisplay` tests still green (formatSize extraction is behavior-preserving).
</verification>

<success_criteria>
- The MIME gate (SVG-download-first), category label/icon map, sequential downloader, and shared formatSize are implemented and pure-tested to 100%.
- artifacts.* copy exists in en+it; both pure modules are Stryker targets.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-03-SUMMARY.md` when done.
</output>
</content>
