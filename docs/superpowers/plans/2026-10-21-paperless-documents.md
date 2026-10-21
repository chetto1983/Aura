# Paperless-style Documents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. The user requires working directly in `D:/Repo/Aura`, without a worktree.

**Goal:** Rebuild Aura's Documents area around the tested Paperless workflow, with real search, navigation, uploads, saved views, previews and metadata editing.

**Architecture:** Extend the existing tenant-scoped document store/API and reuse the object-store upload routes. Compose a persistent document sidebar, overview/library/processing screens and a routed detail view from Aura's React components. Keep Chat/Board/Studio/Graph/Governance intact and preserve all field/provenance/file-viewer contracts.

**Tech Stack:** Go, ArcadeDB 26.10.1, React 19, React Router, TanStack Query, existing Radix/UI primitives, SVAR Grid and Filemanager, existing Poppler utilities and Craft viewers.

**Spec:** `docs/superpowers/specs/2026-10-21-paperless-document-workspace-design.md`

## Global Constraints

- Work in the existing checkout, on `feat/paperless-documents`; no worktrees.
- Implement the confirmed Documents-only A/B scope. Notes, file versions, trash, document sharing/rights, new workflow/mail intake and advanced query syntax are outside this implementation.
- Preserve identity isolation and operator metadata/provenance; use existing stores and stable document IDs.
- No inactive controls. No fabricated ingestion progress or full-parity claims.
- English/Italian and light/dark work. Mobile at 390 px has readable document names and no mandatory horizontal list scrolling.
- Files touched remain at most 600 LOC. TDD for behavior; run required Go/web checks and live acceptance.
- Use GHCR images for the existing stack. Do not rebuild all infrastructure images. Test changed application code locally, then publish/pull through the existing release workflow at job close.
- Copying Paperless code is permitted by the user; preserve any copied code's provenance/license terms. Do not change Aura's license without an actual reuse decision requiring it.

## Review Focus

- Switching identity must not retain another identity's uploads, views, content or previews.
- Same-name uploads must not overwrite an existing object silently; use a unique `uploads/<batch UUID>/` prefix for the new workspace uploader.
- Content matches across many passages must produce unique documents, exact total and correct pagination; the agent's top-k cap is not an archive limit.
- A late query/upload response must not overwrite a newer filter or unsaved form.
- Reload/back/direct links and a failed API request must preserve meaningful context and expose failure rather than show a falsely empty library.

## Decisions taken while writing this plan

- Saved views live with document metadata in each identity's ArcadeDB database, using the existing named-record CRUD helpers. The Board implementation is a behavior/persistence reference, not a shared table with mixed resource types.
- Saved views store a versioned URL query string plus display mode and pin/dashboard flags. The server validates allowed query fields, values and bounded length.
- Processing shows states this stack can establish: uploading, upload failed, waiting for indexing, ready. An unindexed successful upload stays waiting with an explanation; no invented failed/indexing percentage.
- Content tab displays ordered indexed passages with pagination, explicitly labeled indexed content. It does not promise lossless original-file text or editable OCR.
- Thumbnails render PDF first page and supported raster images. Unsupported formats get a named file-type fallback; every existing full viewer/editor remains available.
- Inbox means `reviewed_at` is absent. Suggestions remains the existing `needs_review` filter.
- Custom-field filtering initially offers exact value/unassigned; all five existing types retain their validators. No new numeric/range query language.

## File structure

Backend additions stay in `internal/docfields`, `internal/arcadedb` and `internal/agui`, next to existing document code. Thumbnail rendering has a small focused package if no existing renderer can be reused. Frontend additions stay in `web/src/documents`, split into route/state, navigation, overview, list/cards, saved views, uploads and detail concerns. Only route mounting/surface synchronization and hiding document telemetry touch the outer shell.

### Task 1: Document workspace data contracts

**Files:** `internal/docfields/library.go`, new `library_search.go`, `views.go`, `content.go` and focused tests; `internal/arcadedb/document_fields_schema.go`; `internal/agui/documents_api.go`, new `documents_workspace_api.go` and tests; `prd.md` after live measurement.

**Interfaces:**
- Existing `GET /api/documents` retains its response and gains `search_mode=name|content`, `inbox=true`, repeated `source_key` and `custom` (JSON array of `{field, operator: eq|unassigned, value}`) query parameters. Default mode remains name for compatibility. `List` and `Select` apply identical predicates.
- `GET /api/documents/views` → `{views: SavedView[]}`; `POST /api/documents/views`, `PUT /api/documents/views/{id}` → `{view: SavedView}`; `DELETE .../{id}` → `{deleted:true}`. `SavedView={id,name,version:1,query,display:table|compact|cards,pinned,dashboard}`. Names 1–80 runes, query at most 16 KiB, validated and tenant-owned.
- `GET /api/documents/{id}/content?offset=0&limit=25` → `{passages:[{ordinal,text}],offset,has_more}`; max 50 passages per call. Document existence and tenant membership are checked before reading passages.
- Existing `GET /api/documents?source_key=<key>&...` provides the uploaded-file lookup without adding fictitious queue records. `DocumentRow` can add a bounded `excerpt` and `raw_sha256` for display/cache identity.

- [x] Write failing tests for content-only matches; more than 50 matching passages/documents with exact total, stable pages and no duplicate rows; content+metadata filters; Inbox versus suggestions; empty/missing index; malformed search mode/custom filters; exact upload key lookup.
- [x] Read official ArcadeDB query docs and run disposable live probes for the query forms. Record actual supported SQL on 26.10.1; append the measured PRD amendment before implementation.
- [x] Implement scoped search/filter/content behavior using the existing full-text indexes and named field records. Do not load only a top-k agent result or interpolate user text into SQL.
- [x] Write failing CRUD/validation/isolation tests for saved views, including new Store instance persistence and unknown/foreign IDs.
- [x] Extend idempotent document schema and named-record/API helpers; keep old API clients compatible and split oversized files on touch.
- [x] Run `go vet ./...`, `go build ./...`, package unit/race tests, and disposable `arcadedb_integration` tests with actual database credentials; record commands/results.
- [x] Commit task, run fresh review, resolve findings, merge to master and run fresh targeted verification before Task 2.

### Task 2: Real document thumbnails

**Files:** new focused thumbnail renderer/tests; `internal/agui/documents_preview_api.go` and tests; existing document route registration/wiring only where necessary.

**Interfaces:** `GET /api/documents/{id}/thumbnail` returns an authorized JPEG/PNG thumbnail for PDF/raster inputs, or explicit unsupported/not-found/error status. Client URL may include `v=<raw_sha256 or indexed_at>` for invalidation. No externally supplied source URLs. Use existing document lookup and file-object opener.

- [ ] Inventory existing Poppler/image helpers and read their installed docs. Write failing tests for PDF/raster output, unsupported input, corrupt input, cancellation, size bounds, foreign/missing document IDs and same-name different-identity separation.
- [ ] Render only a bounded first-page/raster preview, with bounded input/output/runtime and concurrency. Reuse the shipped `pdftoppm`; no application-stack rebuild or new unbounded converter.
- [ ] Verify actual sample PDF and raster output and reject malformed/malicious dimensions before expensive allocation. Never serve active file content as an image.
- [ ] Run Go checks, focused race/integration tests and review; commit/merge with fresh targeted verification.

### Task 3: Connected workspace, uploads and saved views

**Files:** `web/src/documents/DocumentsSurface.tsx`, `DocumentsWorkspace.tsx`, `documentsApi.ts`, `FilterBar.tsx`, `BulkBar.tsx`, `bulkModel.ts`, existing tests; new `DocumentsNavigation`, `DocumentsOverview`, `DocumentCards`, `DocumentUpload`, `SavedViews`, route/query hooks and tests; `web/src/i18n/resources.documents.ts`; `web/src/main.tsx`, `AppShell.tsx` and actual telemetry owner.

**Interfaces:** consume Task 1/2 DTOs. Mount `/documents/*` through the existing authenticated AppShell and preserve the document surface on direct navigation. Document routes include overview/library/inbox/processing/lists and `document/:id`; query state uses existing API parameter names plus display/page-size. Legacy Documents entry moves into the new workspace without changing other app destinations.

- [ ] Write failing interaction tests for default document entry, sidebar destinations, URL round trips/back/reload, content/name mode, Inbox and Suggestions distinction, empty/error/retry states and stale-result protection.
- [ ] Implement sidebar/drawer and overview using live counts, recent documents and pinned saved views; library has table/compact/cards, visible Upload, explicit sort/page-size, retained filter chips and bulk-selection contracts.
- [ ] Write failing uploader tests for multi-file/drop, real file-manager multipart response IDs, individual errors/retry, unique batch keys, ready lookup and library/count invalidation. Preserve pending successful uploads across workspace navigation and avoid replaying upload after refresh.
- [ ] Implement upload state with polling of exact source keys; show waiting honestly and offer Files/Open when appropriate. Keep processing status and polling identity-scoped; avoid refetch storms.
- [ ] Write saved-view tests for save/apply/rename/delete/pin/dashboard and fresh session/server persistence. Add custom-field filters and bulk field edits with explicit confirmation, reusing existing Diff validation.
- [ ] Implement real thumbnails with named fallbacks; responsive cards/compact rows keep names readable. Hide global token/cost telemetry only while working in Documents.
- [ ] Run focused Vitest tests, typecheck, lint and build; inspect both themes and languages; review, commit/merge and fresh targeted verification.

### Task 4: Focused document detail and organization

**Files:** `DocumentDetail.tsx`, `DetailFields.tsx`, `DocumentHistory.tsx`, `LabelsAdmin.tsx`, `CustomFieldsAdmin.tsx`, `ListsPage.tsx`; new focused detail tabs/navigation/contextual label controls and tests; document translations.

**Interfaces:** consume the routed selection/result context from Task 3 and indexed-content endpoint from Task 1. Existing metadata/preview/editor APIs remain authoritative.

- [ ] Write failing tests for direct-link detail, previous/next in current results, return context, unsaved-change guards, failed label loads, searchable/create-in-context labels and mobile Details/Preview tabs.
- [ ] Implement desktop fields beside preview with the existing resizable-pane library, and clear Details/Content/History tabs. Mobile separates preview from metadata. Preserve every full preview/Craft edit path and provenance control.
- [ ] Add organization name search and visible document counts; retain matching-rule validation and existing custom-field types. Creating a label from detail preserves the unsaved form.
- [ ] Run focused tests, full document regression, typecheck/lint/build; review, commit/merge and fresh targeted verification.

### Task 5: Live acceptance and publication

**Files:** focused `web/e2e` workflow tests; `docs/superpowers/verification/2026-10-21-paperless-documents.md`; plan checkbox updates; only measured PRD/evidence corrections.

- [ ] Exercise the synthetic PDF set on the real backend: upload → waiting → ready → content search → classify → review → saved view → reload/direct link/back; compare against the captured Paperless reference on desktop and 390 px viewport.
- [ ] Check independent identities for saved views/content/thumbnails; confirm unauthorized responses never expose another identity's data. Verify all tested file previews still open and no unsupported controls are shown.
- [ ] Run required backend/web quality, coverage, mutation and live integration gates for changed packages; fix observed defects, not assertions that correctly expose regressions.
- [ ] Obtain whole-change review, address findings and verify the final tree. Record measured limits, screenshot comparisons and test results; do not invent a UX score.
- [ ] Merge task branch into master, run fresh post-merge checks, push using the repository's WSL hook recipe, check CI and publication for that SHA, then pull/apply the published GHCR image set and verify the running commit.
