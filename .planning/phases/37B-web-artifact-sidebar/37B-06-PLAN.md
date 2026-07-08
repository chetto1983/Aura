---
phase: 37B-web-artifact-sidebar
plan: 06
type: execute
wave: 4
depends_on: ["37B-02", "37B-03", "37B-04"]
files_modified:
  - web/src/chat/artifacts/useThreadArtifacts.ts
  - web/src/chat/artifacts/useThreadArtifacts.test.ts
  - web/src/chat/artifacts/ArtifactRow.tsx
  - web/src/chat/artifacts/ArtifactRow.test.tsx
  - web/src/chat/artifacts/ArtifactsPanel.tsx
  - web/src/chat/artifacts/ArtifactsPanel.test.tsx
autonomous: true
requirements: [WEBART-05, WEBART-06, WEBART-07]
must_haves:
  truths:
    - "useThreadArtifacts(threadId) is a useQuery(['assets', threadId]) over listThreadAssets whose select filters source_kind==='agent' (dropping deleted/canceled) and sorts newest-first client-side (D-10)"
    - "ArtifactRow renders icon + name + 'Categoria · EXT' + a trailing download anchor to /api/assets/{id}/download; row body click opens the preview; degraded rows render a disabled delivery-unavailable note (D-05, D-12)"
    - "ArtifactsPanel renders rows newest-first, a header ('Artefatti' + 'Scarica tutto'), a graceful empty-state, and mounts the lazy PreviewModal for the active row (D-17)"
    - "'Scarica tutto' calls downloadAll with N/M progress, disabled during the run, skipping degraded (D-13)"
    - "no host/container path, object_key, or object_bucket ever appears in the DOM — only id"
  artifacts:
    - path: "web/src/chat/artifacts/useThreadArtifacts.ts"
      provides: "agent-filtered, newest-first derived asset query"
      contains: "'assets'"
    - path: "web/src/chat/artifacts/ArtifactsPanel.tsx"
      provides: "panel container: header, rows, empty/degraded, Scarica tutto, PreviewModal mount"
      min_lines: 40
    - path: "web/src/chat/artifacts/ArtifactRow.tsx"
      provides: "row: icon+name+label, download anchor, preview-open, degraded branch"
      min_lines: 25
  key_links:
    - from: "web/src/chat/artifacts/useThreadArtifacts.ts"
      to: "web/src/chat/attachments/api.ts"
      via: "listThreadAssets query fn"
      pattern: "listThreadAssets"
    - from: "web/src/chat/artifacts/ArtifactsPanel.tsx"
      to: "web/src/chat/artifacts/PreviewModal.tsx"
      via: "lazy import mounted for the active row"
      pattern: "PreviewModal"
  prohibitions:
    - "MUST NOT change the server list order (created_at ASC) or the shared listThreadAssets client — sort newest-first in the select only (it is load-bearing for the upload fold)"
    - "MUST NOT render object_key/object_bucket/any storage or host path — use only id for the download URL"
    - "MUST NOT statically import PreviewModal in a way that pulls docx-preview/xlsx into the panel chunk — lazy-import it"
    - "MUST NOT show user uploads in the panel — filter to source_kind==='agent' (matches the Artefatti reference + D-15 split)"
---

<objective>
Build the Artefatti panel surface: the derived `useThreadArtifacts` query (agent-filtered, newest-first client-side over the ASC server list), the `ArtifactRow` (icon + name + category·EXT + trailing download + row-body preview + degraded branch), and the `ArtifactsPanel` container (header + "Scarica tutto" + empty-state + lazy `PreviewModal` mount). These are self-contained (props: `threadId`, `onClose`) so the AppShell integration (plan 07) mounts the panel without re-deriving contracts.

Purpose: The WEBART-05/06/07 list-view + per-row/all downloads + click-to-preview, over 37A's substrate, with no new source of truth and no path leak.
Output: `useThreadArtifacts.ts`, `ArtifactRow.tsx`, `ArtifactsPanel.tsx` (+ tests).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md
@web/src/chat/attachments/api.ts
@web/src/chat/displays/LocalArtifactDisplay.tsx
</context>

<artifacts_produced>
This plan produces:
- `web/src/chat/artifacts/useThreadArtifacts.ts` → `useThreadArtifacts(threadId)` (React Query hook; `['assets', threadId]`; select = filter agent + drop deleted/canceled + sort DESC).
- `web/src/chat/artifacts/ArtifactRow.tsx` → `ArtifactRow` (props: asset, onPreview).
- `web/src/chat/artifacts/ArtifactsPanel.tsx` → `ArtifactsPanel` (props: `threadId`, `onClose`).
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: useThreadArtifacts — derived agent-filtered newest-first query</name>
  <files>web/src/chat/artifacts/useThreadArtifacts.ts, web/src/chat/artifacts/useThreadArtifacts.test.ts</files>
  <behavior>
    - queryKey is ['assets', threadId]; queryFn reuses listThreadAssets(threadId, signal) verbatim
    - enabled only when threadId.length > 0
    - select filters source_kind === 'agent' and drops status 'deleted'/'canceled'
    - given a server list in created_at ASC, the hook returns rows in DESC (newest-first) order
    - non-agent (upload) assets are excluded
  </behavior>
  <read_first>
    - web/src/chat/attachments/api.ts:47-57 — `listThreadAssets` (reuse as the query fn; do NOT write a new client).
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 2 — Derived live view" (the exact useQuery + select).
    - web/src/queryClient.ts + an existing useQuery test for the `new QueryClient({defaultOptions:{queries:{retry:false}}})` wrapper convention.
  </read_first>
  <action>
    Create `web/src/chat/artifacts/useThreadArtifacts.ts` exporting `useThreadArtifacts(threadId)` = `useQuery({ queryKey: ['assets', threadId], enabled: threadId.length > 0, queryFn: ({signal}) => listThreadAssets(threadId, signal), select: (assets) => assets.filter(a => a.source_kind === 'agent' && a.status !== 'deleted' && a.status !== 'canceled').slice().sort((a,b) => (b.created_at ?? '').localeCompare(a.created_at ?? '')) })`. Write `useThreadArtifacts.test.ts` mocking `listThreadAssets` to return an ASC list mixing upload + agent + deleted rows, asserting the hook yields agent-only, newest-first, deleted-dropped.
  </action>
  <acceptance_criteria>
    - hook uses `['assets', threadId]` and reuses `listThreadAssets`.
    - test: given ASC server input, output is DESC and agent-only.
    - `cd web && npx vitest run src/chat/artifacts/useThreadArtifacts.test.ts` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "listThreadAssets" src/chat/artifacts/useThreadArtifacts.ts && npx vitest run src/chat/artifacts/useThreadArtifacts.test.ts && echo USETHREADARTIFACTS_OK</automated>
  </verify>
  <done>Derived query returns agent-only, newest-first rows over the ASC server list; test green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: ArtifactRow — icon + name + category·EXT + download + preview + degraded</name>
  <files>web/src/chat/artifacts/ArtifactRow.tsx, web/src/chat/artifacts/ArtifactRow.test.tsx</files>
  <behavior>
    - renders the category icon (from artifactMeta.categoryIcon), the file name, the "Categoria · EXT" label (categoryLabel), and formatSize
    - the trailing download control is an <a href="/api/assets/{id}/download" download={file_name}> (accepted rows)
    - clicking the row body (not the download anchor) calls onPreview(asset)
    - a degraded asset (no asset_id / status !== 'accepted') renders a disabled role="note" delivery-unavailable branch (mirroring LocalArtifactDisplay) and does NOT open a preview
    - the DOM contains no object_key/object_bucket/host path (negative assertion)
  </behavior>
  <read_first>
    - web/src/chat/displays/LocalArtifactDisplay.tsx:65-113 — the download anchor + the degraded role="note" branch to mirror (D-18).
    - web/src/chat/artifacts/artifactMeta.ts — categoryIcon/categoryLabel/formatSize (from plan 03).
    - .planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md "ArtifactRow.tsx".
  </read_first>
  <action>
    Create `web/src/chat/artifacts/ArtifactRow.tsx`: `ArtifactRow({ asset, onPreview })` rendering `categoryIcon` + name + `categoryLabel` + `formatSize`, a trailing `/api/assets/{id}/download` download anchor (accepted), a clickable row body → `onPreview(asset)`, and the degraded `role="note"` branch for `!asset_id`/`status !== 'accepted'` (disabled, no preview). Use `id` only — never `object_key`/`object_bucket`. Write `ArtifactRow.test.tsx` asserting the download href, the row-body → onPreview, the degraded branch, and the no-path negative assertion.
  </action>
  <acceptance_criteria>
    - accepted row anchor href is `/api/assets/{id}/download` with `download={file_name}`.
    - row-body click calls `onPreview`; degraded row does not.
    - test asserts no `object_key`/`object_bucket` string in the rendered DOM.
    - `cd web && npx vitest run src/chat/artifacts/ArtifactRow.test.tsx` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/artifacts/ArtifactRow.test.tsx && echo ARTIFACTROW_OK</automated>
  </verify>
  <done>Row shows icon/name/label/size, downloads via id-only anchor, opens preview on body click, degrades gracefully; test green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: ArtifactsPanel — header, rows, empty-state, Scarica tutto, PreviewModal mount</name>
  <files>web/src/chat/artifacts/ArtifactsPanel.tsx, web/src/chat/artifacts/ArtifactsPanel.test.tsx</files>
  <behavior>
    - renders a header with the "Artefatti" title + a "Scarica tutto" button
    - lists ArtifactRow items from useThreadArtifacts, newest-first
    - empty result → a graceful empty-state (icon + i18n copy), not a crash
    - "Scarica tutto" calls downloadAll(rows, onProgress), shows N/M progress, is disabled during the run, and skips degraded rows
    - selecting a row sets active state and mounts the lazy PreviewModal; closing clears it
    - onClose prop is wired (used by the mobile drawer / header toggle in plan 07)
  </behavior>
  <read_first>
    - web/src/AppShell.tsx (the navigation panel body) — the panel layout/scroll conventions to mirror.
    - web/src/chat/artifacts/downloadAll.ts + useThreadArtifacts.ts + ArtifactRow.tsx (this plan's earlier tasks) + PreviewModal.tsx (plan 04).
    - .planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md "ArtifactsPanel.tsx" + RESEARCH "Recommended Project Structure".
  </read_first>
  <action>
    Create `web/src/chat/artifacts/ArtifactsPanel.tsx`: `ArtifactsPanel({ threadId, onClose })` calling `useThreadArtifacts(threadId)`, rendering the header (`artifacts.title` + a "Scarica tutto" button driving `downloadAll` with an `artifacts.downloadAllProgress` N/M indicator, disabled during the run, abortable), the newest-first `ArtifactRow` list, and the empty-state (`artifacts.empty`/`artifacts.emptyHint`) when zero rows. Hold `active` asset state; render `const PreviewModal = lazy(() => import('./PreviewModal'))` under `<Suspense>` for the active row; row `onPreview` sets active, modal `onClose` clears it. Keep the file ≤600 LOC (split a `ScaricaTuttoButton` sub-component if it grows). Write `ArtifactsPanel.test.tsx` (QueryClientProvider wrapper) covering: rows render newest-first, empty-state, Scarica-tutto invokes downloadAll + disables during run, and preview open/close.
  </action>
  <acceptance_criteria>
    - panel renders rows from `useThreadArtifacts`, an empty-state on zero, and a "Scarica tutto" control calling `downloadAll`.
    - `PreviewModal` is mounted via `lazy(() => import('./PreviewModal'))` (not a static import).
    - `cd web && npx vitest run src/chat/artifacts/ArtifactsPanel.test.tsx` exits 0.
    - `cd web && npx vitest run src/chat/artifacts/ --coverage` keeps the artifacts dir ≥85%.
    - `cd web && npx tsc --noEmit` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "downloadAll" src/chat/artifacts/ArtifactsPanel.tsx && grep -q "import('./PreviewModal')" src/chat/artifacts/ArtifactsPanel.tsx && npx vitest run src/chat/artifacts/ArtifactsPanel.test.tsx && npx tsc --noEmit && echo ARTIFACTSPANEL_OK</automated>
  </verify>
  <done>Panel lists rows newest-first with empty-state, Scarica-tutto (progress+disable+skip-degraded), and a lazy PreviewModal; tests green + dir ≥85%.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| server asset JSON → panel DOM | The list JSON carries internal store keys that must never reach the browser DOM |
| user → bulk download action | A multi-download control must not be abusable / hang the tab |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-16 | Information Disclosure (path/key leak) | ArtifactRow / panel render | mitigate | Render only `id`; never `object_key`/`object_bucket`/host path (negative test assertion); download URL is `/api/assets/{id}/download` |
| T-37B-17 | Information Disclosure (IDOR) | useThreadArtifacts | mitigate | Reuses `listThreadAssets` → the already identity-scoped `GET /api/assets?thread_id=` (`GetForIdentity`, 404 non-owner); no new endpoint |
| T-37B-07 | Denial of Service (self) | Scarica tutto | mitigate | `downloadAll` throttle + disabled-during-run + accepted-only (from plan 03) |
</threat_model>

<verification>
- `npx vitest run src/chat/artifacts/` green; artifacts dir coverage ≥85%.
- Panel/row render agent-only, newest-first, id-only downloads; empty + degraded states covered.
- `npx tsc --noEmit` clean.
</verification>

<success_criteria>
- The Artefatti panel lists agent deliverables newest-first with per-row + "Scarica tutto" downloads, click-to-preview, empty/degraded states, and zero path leakage.
- The derived view reuses the identity-scoped list endpoint with no new source of truth.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-06-SUMMARY.md` when done.
</output>
</content>
