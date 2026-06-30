# Design - Aura document library product UX

- **Date:** 2026-06-30
- **Status:** Approved direction; implementation plan pending user review of this spec.
- **Approved approach:** Native Aura file-manager UX inspired by SVAR, not a wholesale embedded vendor widget.
- **Scope:** React document workspace, document catalog API usage, upload/update/delete UX, document chat entry points, and E2E validation strategy.
- **Out of scope for this spec:** Deep backend lifecycle redesign already covered by `2026-06-30-document-ingestion-industrial-audit-design.md`.

## Intent

Aura is a product, not a technical preview. The document area must open as a confident library for real work: fast to scan, easy to search, safe to operate, and visibly connected to chat/RAG. Internal facts such as hashes, graph events, storage orphans, and pipeline internals remain available, but they must not dominate the first screen.

The target experience is closer to a modern file manager: title, search, create/upload action, tabs, list/grid controls, rows with file thumbnails/icons, modified date, size, status, selection, and context actions. Aura then adds its own value: document status, tags, versions, citations, and "ask this document" workflows.

## Evidence

Local evidence:

- Current document workspace is an inspector-style split view: `web/src/documents/DocumentsWorkspace.tsx`.
- Current API client supports list/detail/update/delete/events/orphan cleanup: `web/src/documents/documentApi.ts`.
- Current tests cover list/detail, tag save, delete confirmation, retry, and storage orphan cleanup: `web/src/documents/__tests__/DocumentsWorkspace.test.tsx`.
- Current shell already treats documents as a focused workspace: `web/src/AppShell.tsx`.

External references reviewed:

- SVAR React FileManager provides file-manager primitives such as upload, download, list/tile views, tree, preview pane, context menu, toolbar, keyboard navigation, localization, TypeScript, and backend integration: <https://github.com/svar-widgets/react-filemanager>
- SVAR backend integration supports server-loaded data and a `RestDataProvider` flow: <https://docs.svar.dev/react/filemanager/guides/working_with_server/>
- SVAR open issue #17 flags large-directory performance and virtualization as a relevant integration risk: <https://github.com/svar-widgets/react-filemanager/issues/17>
- Google Drive current help describes filter chips for narrowing file search by type, people, modified date, and more: <https://support.google.com/drive/answer/2375114>
- Microsoft SharePoint current Copilot file-processing docs make processing status visible in document-library activity, with in-progress, completed, and failed states: <https://learn.microsoft.com/en-us/sharepoint/copilot-in-sharepoint-file-processing-status>
- Adobe Acrobat citation UX shows that AI answers over documents need source/citation visibility: <https://helpx.adobe.com/acrobat/desktop/explore-pdf-spaces/view-citations.html>
- NN/g recommends AI answer experiences that reduce chat friction and answer directly with useful context: <https://www.nngroup.com/articles/less-chat-more-answer/>
- WAI-ARIA APG grid pattern defines expectations for interactive tabular lists, keyboard navigation, row selection, focus, and sort semantics: <https://www.w3.org/WAI/ARIA/apg/patterns/grid/>

## Product Principles

1. **Library first, internals second.** The first viewport shows files and actions. Versions, hashes, traces, and cleanup live in a details drawer or advanced section.
2. **Visible lifecycle.** Uploading, processing, ready, failed, deleting, archived, and deleted states appear on rows and in details. A user should not need logs to understand whether a document can be searched.
3. **Search is a product feature.** Search combines text input, filter chips, tabs, and tags. It should support common user questions: "show PDFs", "failed documents", "robot manuals", "updated this week".
4. **Safe destructive operations.** Delete remains confirmed and clear. Failed pipeline cleanup, orphan cleanup, and hard-delete operations are advanced/admin actions.
5. **Document-to-chat loop.** A ready document can be opened, previewed, tagged, updated, and used as the scope for chat. Chat answers should expose document sources/citations.
6. **Industrial density.** The UI should be compact and scannable like the provided screenshot: no marketing hero, no oversized panels, no nested cards, no decorative background.
7. **Accessible file manager behavior.** Rows, grids, sort controls, selection, menus, drawers, and dialogs must be keyboard and screen-reader usable.

## Recommended Approach

Build a native Aura document library with file-manager interactions inspired by SVAR.

Why this approach:

- It preserves Aura's document-specific semantics: RAG readiness, graph lifecycle, tags, versions, citations, retries, and safe deletion.
- It avoids binding core UX to a third-party widget whose generic file model may not fit versioned searchable documents.
- It keeps visual language consistent with Aura's tokens and existing shell.
- It allows focused tests against current contracts without waiting for a vendor adapter.

SVAR remains valuable as a reference and optional spike. If implementation time becomes constrained, a hybrid can be considered: SVAR for the center list with an Aura-native adapter and drawer. The direct-embed path is not the default.

## User Experience

### Main Layout

The document workspace becomes a single product surface:

- Header: `Libreria` / `Document library`, compact search, `Nuovo` / `New` menu.
- Tabs: `Tutti`, `Documenti`, `Immagini`, `File`, plus status chips such as `Falliti` and `In elaborazione` when counts are non-zero.
- Toolbar: filter button, list/grid toggle, sort controls, refresh, and selection-aware actions.
- Content: dense list by default, grid optional for images and visual files.
- Details drawer: opens on row click or info action without leaving the library.

The existing left-filter/sidebar layout is removed from the default experience. Filters move into top chips and an optional filter popover.

### Document Row

Each row contains:

- Selection checkbox with stable width.
- Type icon or thumbnail.
- Title with tooltip/truncation protection.
- Tags as compact chips when space allows.
- Status badge: ready, processing, failed, deleting, archived.
- Modified date.
- Size.
- More-actions menu.

Rows support hover, selected, focused, loading, failed, and disabled states. Text must not resize the row or overflow the controls.

### Actions

Primary actions, shown when supported by the available backend contract:

- Upload document.
- Upload new version for selected document.
- Rename/edit title.
- Edit tags.
- Ask this document.
- Download, if available.
- Retry processing for failed documents.
- Delete document with confirmation.

Advanced actions:

- View versions.
- View processing events.
- View hashes/storage IDs.
- Scan storage orphans.
- Delete orphan objects.

The `Nuovo` menu should start with upload actions because the library's main job is document work, not folder decoration.

### Details Drawer

The drawer contains product-level sections in this order:

1. Overview: title, status, type, size, modified time, tags, scope.
2. Actions: ask, update version, retry, rename, edit tags, delete.
3. Preview/citations entry point: preview if supported; otherwise file metadata and ask action.
4. Versions: concise version list with active marker.
5. Processing: latest lifecycle events with clear failure messages.
6. Technical: IDs, hashes, storage object IDs, graph state, behind an expandable advanced area.

This keeps production diagnostics available without making them the default visual weight.

### Search And Filters

Use one search box and chip filters:

- Text query maps to existing `/api/documents?q=...`.
- Tag chips map to `/api/documents?tag=...`.
- Scope chips map to `/api/documents?scope=library|thread`.
- Status filtering can be client-side initially if the backend does not expose it yet; backend status query can be added later.
- Type filtering can be inferred from active version MIME type when detail is loaded, or from metadata if already present.

The first implementation should not invent folders unless the backend has a real folder model. Tags and filters are enough for product search now.

### Empty, Loading, And Failure States

Empty state:

- Show an upload-first message and a primary upload button.
- Do not show a giant debug placeholder.

Loading state:

- Use stable skeleton rows sized like final rows.

Failed list load:

- Show a concise inline alert with retry.

Failed document processing:

- Row status is visible.
- More menu and drawer expose retry.
- Drawer shows the latest failure event.

### Mobile

Mobile keeps the same product hierarchy:

- Header with title, search icon, and new/upload action.
- Tabs horizontally scroll.
- List rows compress to title, status, modified/size, and menu.
- Details drawer becomes a full-screen sheet.
- Grid view can be hidden on narrow screens if it reduces usability.

## Architecture

### React Components

Refactor `DocumentsWorkspace.tsx` into focused units:

- `DocumentsWorkspace`: data loading, URL/local state, layout composition.
- `DocumentLibraryHeader`: title, search, new menu.
- `DocumentFilterBar`: tabs, chips, filter popover trigger.
- `DocumentFileList`: table/list view, sorting, selection, keyboard behavior.
- `DocumentGrid`: optional visual tile view.
- `DocumentActionMenu`: row and selected-actions menu.
- `DocumentDetailsDrawer`: overview, actions, tags, versions, processing, advanced technical fields.
- `DocumentUploadDialog`: upload/new-version flow if current APIs support it; otherwise limited to a route into existing attachment upload flow.
- `DocumentEmptyState`, `DocumentLoadingRows`, `DocumentErrorState`.
- `DocumentAdminPanel`: storage orphan cleanup and any future advanced maintenance tools.

Do not create nested cards. Use full-width bands, rows, drawers, dialogs, and menus.

### API/Data Flow

Initial load:

1. Workspace loads `/api/documents?limit=50` with query/tag/scope filters.
2. The list renders immediately from `DocumentItem[]`.
3. Selecting or opening details loads `/api/documents/:id`.
4. Drawer loads `/api/documents/:id/events` lazily.

Metadata edits:

1. Rename/tag edit uses existing `PATCH /api/documents/:id`.
2. The list row updates from the server response; no speculative mutation happens before success.
3. Errors keep the drawer open and show an inline message.

Delete:

1. Menu/drawer opens delete dialog.
2. User confirms.
3. Existing `DELETE /api/documents/:id` runs.
4. List refreshes and removed/deleted items do not remain selected.

Retry:

1. Retry uses active version `asset_id` and existing `/api/assets/:assetId/retry`.
2. List/detail refresh after success.

Upload/new version:

- If a document-specific upload/new-version API exists by implementation time, use it.
- If not, first implementation can expose product upload through the existing asset upload path and refresh the library after processing.
- New-version upload must not pretend to exist until the backend supports document-version binding.

### State

Keep state simple and local for the first implementation:

- query
- active tab
- filter chips
- view mode
- selected IDs
- active drawer document ID
- loading/error states per list/detail/action

Avoid global state until the library needs cross-surface synchronization with chat.

## Error Handling

- Every async action has a pending state and disabled duplicate submit.
- List-level load failure does not blank the whole shell.
- Row action failures show a toast/inline alert and keep context.
- Drawer action failures stay in the drawer.
- Delete confirmation remains typed or otherwise explicit for permanent destructive actions.
- Orphan cleanup remains typed and isolated in admin.

## Accessibility

- The list should be an HTML table or ARIA grid with proper row/column semantics.
- Sort headers expose sort direction.
- Selected rows expose selection state.
- The more-actions button has an accessible label with the document title.
- Menus trap focus correctly and return focus to the trigger.
- Drawer has title, close control, focus management, and escape behavior.
- All icon-only controls have tooltips or accessible names.
- Keyboard users can open details, action menu, select rows, and invoke primary actions.
- Tests should include at least smoke coverage for accessible names and menu/dialog behavior.

## Visual Direction

Match the screenshot's product density and calm:

- Dark default theme with Aura tokens.
- Compact rows around the existing density system.
- Rounded row hover/selected states, but not oversized cards.
- Status badges use meaningful semantic colors already in the token palette.
- Keep icons from `lucide-react`.
- No decorative backgrounds, hero blocks, oversized empty cards, or explanatory marketing copy.

## Validation Plan

Unit/component tests:

- Loads list and renders file-manager rows.
- Search query and filter chips call the expected API.
- Row selection and action menu behavior.
- Details drawer loads versions/events lazily.
- Tag save uses existing PATCH shape.
- Delete confirmation uses existing DELETE flow.
- Retry failed document uses existing asset retry flow.
- Admin/orphan cleanup remains reachable but not on the default first viewport.

Accessibility checks:

- Row/menu/drawer/dialog accessible names.
- Keyboard open/close for menu and drawer.
- No obvious text overflow in long filenames.

Backend/frontend gates:

- `go vet ./...`
- Relevant `go test` packages for documents/agui/assets if backend is touched.
- `npm.cmd run lint` in `web`.
- `npm.cmd run typecheck` or equivalent TypeScript check.
- Relevant Vitest document tests.
- Playwright E2E for authenticated document library flow.

Live E2E target:

1. Run current Docker stack with updated Aura container.
2. Act as a user in the browser.
3. Upload a real document from `D:\tmp\Rag_docs`.
4. Wait until it is visible and searchable/ready in the library.
5. Chat about the document and verify an answer uses document retrieval/citations.
6. Delete the document and verify it disappears from normal retrieval.
7. Upload/update a replacement or new version if backend support exists.
8. Verify the list, details drawer, processing state, and chat path still work.

The goal is not only green tests. The UX must feel like a shipped document product while proving the ingestion/retrieval/delete path.

## Implementation Boundaries

- Do not implement folder semantics without backend support.
- Do not make storage orphan cleanup part of the everyday library screen.
- Do not hide processing failures behind logs.
- Do not import SVAR as the first step. Build native Aura components first, then evaluate SVAR only if a focused spike proves it improves speed without harming product fit.
- Preserve current API contracts unless implementation planning identifies a necessary backend addition.
- Preserve unrelated worktree changes.

## Open Decisions For Implementation Plan

- Whether upload should be supported directly from the library in the first implementation using the existing asset upload APIs.
- Whether status/type filters should be client-side initially or require backend query parameters.
- Whether the details drawer preview can show extracted text snippets now or should start with metadata plus chat action.
- Whether Playwright should create a disposable test document via UI or reuse a fixture from `D:\tmp\Rag_docs`.

These are planning decisions, not blockers for the approved product direction.
