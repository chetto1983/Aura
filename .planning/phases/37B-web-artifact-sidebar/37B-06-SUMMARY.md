---
phase: 37B-web-artifact-sidebar
plan: 06
subsystem: ui
tags: [webart, artifacts-panel, react-query, derived-view, lazy-import, degraded-branch, vitest, react]

# Dependency graph
requires:
  - phase: 37B-web-artifact-sidebar (plan 02)
    provides: "Asset.source_kind union widened to include 'agent' — the discriminant the panel filters on"
  - phase: 37B-web-artifact-sidebar (plan 03)
    provides: "artifactMeta (categoryIcon/categoryLabel/formatSize) + downloadAll (throttled sequential loop) + artifacts.* i18n (en+it)"
  - phase: 37B-web-artifact-sidebar (plan 04)
    provides: "PreviewModal (Radix 90vw/90vh previewKind dispatch + 6 lazy renderers) — mounted lazily by the panel"
  - phase: 37A-web-artifact-delivery-lane
    provides: "GET /api/assets?thread_id= (identity-scoped list) + GET /api/assets/{id}/download (auth route the rows target)"
provides:
  - "useThreadArtifacts(threadId): useQuery(['assets', threadId]) over listThreadAssets; select filters source_kind==='agent' + drops deleted/canceled + sorts newest-first client-side over the ASC server list (D-10/D-14)"
  - "selectAgentArtifacts(assets): the exported pure projection (filter + DESC sort) for direct unit coverage"
  - "ArtifactRow({ asset, onPreview }): icon tile + monospace name + 'Categoria · EXT · size' + id-only download anchor + click-to-preview body; degraded (status!=='accepted') → role='note', no preview"
  - "ArtifactsPanel({ threadId, onClose }): header (Artefatti + close) + Scarica tutto (N/M progress, disabled-during-run, abortable) + newest-first row list + considered empty-state + lazily-mounted PreviewModal"
affects: [37B-07, 37B-08]

# Tech tracking
tech-stack:
  added: []   # client-side only — no new deps, no backend change, no migration, no env
  patterns:
    - "Derived live view: the panel owns NO source of truth — a useQuery select projects the shared identity-scoped list (agent-only, newest-first) so a fresh aura.artifact frame only has to invalidate ['assets', threadId] (D-14)"
    - "Server-order preservation: the created_at ASC server order is NEVER reversed server-side (the upload fold depends on it) — newest-first lives only in the client select, on a .slice() copy"
    - "Two-state row (D-18): accepted → download anchor + preview; degraded → role='note' inert — mirrors LocalArtifactDisplay's asset_id split, keyed on status here"
    - "Lazy modal mount: PreviewModal is React.lazy'd AND rendered only when a row is active, so the modal + its 6 renderer chunks stay off the panel's static import graph until first preview (D-08)"
    - "Anchor-sibling-of-button: the row download <a> is a sibling of the preview <button> (never nested — invalid HTML) with stopPropagation, so downloading never also opens the preview"

key-files:
  created:
    - web/src/chat/artifacts/useThreadArtifacts.ts
    - web/src/chat/artifacts/useThreadArtifacts.test.ts
    - web/src/chat/artifacts/ArtifactRow.tsx
    - web/src/chat/artifacts/ArtifactRow.test.tsx
    - web/src/chat/artifacts/ArtifactsPanel.tsx
    - web/src/chat/artifacts/ArtifactsPanel.test.tsx
  modified: []

key-decisions:
  - "Degraded = status !== 'accepted' (not a separate asset_id field): the panel's Asset always has an id, so delivery-readiness is the status — matching downloadAll's accepted-only filter"
  - "PreviewModal rendered conditionally (active !== undefined) inside Suspense, not unconditionally — defers the chunk import to first preview rather than panel mount"
  - "Reused only EXISTING i18n keys (display.artifact.download/downloadAria/deliveryUnavailable + artifacts.*) — resources.display.ts is out of this plan's file scope"
  - "Bulk download passes the FULL row set to downloadAll (which skips non-accepted internally); the accepted count drives the disabled-state + N/M total"

patterns-established:
  - "selectAgentArtifacts exported separately from the hook so the filter+sort is unit-tested without a React render"
  - "In-flight downloadAll aborted via an AbortController ref in a threadId-keyed useEffect cleanup (no stale run across thread switch / unmount)"

requirements-completed: []  # WEBART-05/06/07 stay open — phase-spanning; live browser + aggregate land at terminal 37B-08

coverage:
  - id: D1
    description: "useThreadArtifacts derives an agent-only, newest-first view over the ASC identity-scoped server list (reusing listThreadAssets, no new client)"
    requirement: "WEBART-07"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/useThreadArtifacts.test.ts#reuses listThreadAssets and returns agent-only, newest-first rows"
        status: pass
      - kind: unit
        ref: "web/src/chat/artifacts/useThreadArtifacts.test.ts#keeps agent-only rows newest-first and drops uploads/deleted/canceled"
        status: pass
    human_judgment: false
  - id: D2
    description: "ArtifactRow renders id-only download + click-to-preview for accepted rows and a degraded role='note' branch, leaking no object_key/bucket/host path"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/ArtifactRow.test.tsx#renders the id-only download anchor with the file name"
        status: pass
      - kind: unit
        ref: "web/src/chat/artifacts/ArtifactRow.test.tsx#renders only the id in the download URL, never object_key/object_bucket/host path"
        status: pass
    human_judgment: false
  - id: D3
    description: "ArtifactsPanel lists rows newest-first with an empty-state, a Scarica-tutto bulk download (N/M progress, disabled during run), and a lazily-mounted PreviewModal"
    requirement: "WEBART-06"
    verification:
      - kind: unit
        ref: "web/src/chat/artifacts/ArtifactsPanel.test.tsx#invokes downloadAll and disables the button + shows N/M progress during the run"
        status: pass
      - kind: unit
        ref: "web/src/chat/artifacts/ArtifactsPanel.test.tsx#opens the lazy PreviewModal on a row click and closes it"
        status: pass
    human_judgment: false
  - id: D4
    description: "Panel visual language is distinctive/considered (glyph-plate empty-state, staggered row reveal, icon tiles) rather than generic AI-slop — visual sign-off"
    verification: []
    human_judgment: true
    rationale: "Aesthetic quality is a human judgment; the live browser render lands at 37B-07 integration + 37B-08 e2e."

# Metrics
duration: ~20min
completed: 2026-07-09
status: complete
---

# Phase 37B Plan 06: Artefatti Panel Summary

**The self-contained Artefatti panel — a derived agent-only, newest-first React Query view rendered as icon-tiled rows with id-only downloads, a throttled "Scarica tutto", a considered empty-state, and a lazily-mounted preview modal — ready for the AppShell to mount by `{ threadId, onClose }` alone.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-09T00:44Z
- **Completed:** 2026-07-09T01:00Z
- **Tasks:** 3 completed
- **Files created:** 6 (3 components/hooks + 3 test files)

## Accomplishments

- **Task 1 (`00cabe38`) — useThreadArtifacts.** A `useQuery(['assets', threadId])` that reuses the identity-scoped `listThreadAssets` verbatim as its query fn (no new client, ownership stays server-side), disabled until `threadId` is non-empty. The exported pure `selectAgentArtifacts` projection filters `source_kind === 'agent'`, drops `deleted`/`canceled`, and sorts a `.slice()` copy newest-first over the ASC server list — the server order is never reversed (the upload fold depends on it). 5 tests (pure projection + wired query + disabled-empty).
- **Task 2 (`b7878a3c`) — ArtifactRow.** Icon tile (`categoryIcon`) + monospace name + `"Categoria · EXT · size"` (`categoryLabel`/`formatSize`). Accepted rows get the 37A-proven `/api/assets/{id}/download` anchor (a sibling of the preview `<button>`, `stopPropagation` so downloading never opens a preview) and a click-to-preview body. Degraded rows (`status !== 'accepted'`) render a disabled `role="note"` delivery-unavailable affordance with no preview trigger — mirroring `LocalArtifactDisplay`'s split. 7 tests including the T-37B-16 no-path/key negative assertion.
- **Task 3 (`4ee8e660`) — ArtifactsPanel.** `{ threadId, onClose }` container: header (Artefatti title + close toggle), a "Scarica tutto" button driving `downloadAll` with `artifacts.downloadAllProgress` N/M feedback, disabled during the run and disabled when there are zero accepted rows, aborted on unmount/thread switch via a threadId-keyed `AbortController` cleanup. The newest-first `ArtifactRow` list has a staggered reveal; a considered glyph-plate empty-state renders on zero. `PreviewModal` is `lazy(() => import('./PreviewModal'))` AND only rendered when a row is active, so its chunk (and the six renderer chunks it owns) load on first preview, not panel mount. 6 tests (newest-first render, empty-state, close, downloadAll invoke+disable+progress, no-accepted disable, preview open/close).

## Derived query / filter / sort (how it works)

`listThreadAssets(threadId)` returns the identity-scoped thread assets in `created_at ASC`. The hook keeps that fetch as-is (so `GetForIdentity` still 404s non-owners) and does all projection in `select`: `.filter(a => a.source_kind === 'agent' && a.status !== 'deleted' && a.status !== 'canceled')` then `.slice().sort((a,b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''))`. Newest-first is a client-only re-order on a copy — no new source of truth, and a live `aura.artifact` frame (plan 07) only needs to invalidate `['assets', threadId]`.

## ArtifactRow degraded branch

The panel's `Asset` always carries an `id`, so "degraded" is `status !== 'accepted'` (still processing, failed, refused) — the same predicate `downloadAll` uses to skip a row. A degraded row drops the download anchor AND the clickable preview body, rendering instead a disabled `role="note"` with the warning glyph + `display.artifact.deliveryUnavailable`; clicking it does nothing (no `<button>`, verified by the test).

## PreviewModal lazy mount

`const PreviewModal = lazy(() => import('./PreviewModal').then((m) => ({ default: m.PreviewModal })))` — the `.then` maps the named export to the default shape `React.lazy` needs, and the dynamic `import()` puts it in its own chunk. It is rendered only inside `{active !== undefined && <Suspense>…</Suspense>}`, so the import fires on the first row-preview click, keeping the modal + docx-preview/xlsx renderer chunks out of the panel's static graph (D-08). Row `onPreview` sets `active`; the modal's `onClose` clears it (unmounts).

## Deviations from Plan

None — plan executed exactly as written. Two within-scope refinements worth noting (not deviations): (1) the degraded predicate is `status !== 'accepted'` rather than the plan's `!asset_id` since the panel's list `Asset` always has an `id` and no separate `asset_id` field — status is the delivery-readiness signal; (2) only pre-existing i18n keys were used, since `resources.display.ts` is outside this plan's `files_modified` (it already carries every `artifacts.*` key from plan 03).

## Validation

- `npx tsc --noEmit` — clean (fixed one `exactOptionalPropertyTypes` test-fixture case).
- `npx vitest run src/chat/artifacts/` — 8 files / 76 tests pass (3 new: useThreadArtifacts 5, ArtifactRow 7, ArtifactsPanel 6).
- `npx vitest run src/chat/` — full chat suite 44 files / 471 tests pass (no regression).
- Coverage `src/chat/artifacts/` — 98.8% Stmts / 94.64% Branch / 100% Funcs / 99.25% Lines (≥85% floor).
- `eslint --max-warnings=0` + `prettier --check` — clean on all 6 touched files.
- Grep gates — `listThreadAssets` present in the hook; `downloadAll` + `import('./PreviewModal')` present in the panel.
- File sizes — useThreadArtifacts.ts 38, ArtifactRow.tsx 104, ArtifactsPanel.tsx 160 LOC (all ≤600).
- Pre-commit hooks (dup/vet/file-size) green on all 3 feat commits (no `--no-verify`).

No backend change, no server list-order change, no new deps/migrations/env — client-side only. The panel is not yet mounted into AppShell (that is 37B-07). WEBART-05/06/07 stay `[ ]` (phase-spanning: live browser verification + the aggregate coverage/e2e land at terminal 37B-08, matching the 37B-04/05 precedent).

## Self-Check: PASSED

- Files created: all 6 FOUND on disk.
- Commits: `00cabe38`, `b7878a3c`, `4ee8e660` all present in `git log`.
- Tree clean after each task commit; all 3 files under the 600-LOC cap.
