# Paperless-style Documents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. The user requires working directly in `D:/Repo/Aura`, without a worktree.

**Goal:** Rebuild Aura's Documents area around the tested Paperless workflow, with real search, navigation, uploads, saved views, previews and metadata editing.

**Architecture:** Extend the existing tenant-scoped document store/API and reuse the object-store upload routes. Compose a persistent document sidebar, overview/library/processing screens and a routed detail view from Aura's React components. Keep Chat/Board/Studio/Graph/Governance intact and preserve all field/provenance/file-viewer contracts.

**Tech Stack:** Go, ArcadeDB 26.10.1, React 19, React Router, TanStack Query, existing Radix/UI primitives, SVAR Grid and Filemanager, native Craft CLI/apps and existing viewers.

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
- Same-name uploads must not overwrite an existing object silently, including within one batch; use a unique `uploads/<item UUID>/` prefix per queue entry in the new workspace uploader.
- Content matches across many passages must produce unique documents, exact total and correct pagination; the agent's top-k cap is not an archive limit.
- A late query/upload response must not overwrite a newer filter or unsaved form.
- Reload/back/direct links and a failed API request must preserve meaningful context and expose failure rather than show a falsely empty library.

## Decisions taken while writing this plan

- Saved views live with document metadata in each identity's ArcadeDB database, using the existing named-record CRUD helpers. The Board implementation is a behavior/persistence reference, not a shared table with mixed resource types.
- Saved views store a versioned URL query string plus display mode and pin/dashboard flags. The server validates allowed query fields, values and bounded length.
- Processing shows states this stack can establish: uploading, upload failed, waiting for indexing, ready. An unindexed successful upload stays waiting with an explanation; no invented failed/indexing percentage.
- Content tab displays ordered indexed passages with pagination, explicitly labeled indexed content. It does not promise lossless original-file text or editable OCR.
- User correction: previews must reuse native Craft apps for PDFs and all supported Office families. No new Poppler/x-image rendering implementation. Unsupported formats get a named fallback; existing CraftFrame/EditInCraft and native audio/video viewers remain available.
- Documents UI and API default to document/Office/text file types; pictures, videos, audio, code and All files are explicit filters. No old default-all compatibility branch.
- User clarified ingestion: **keep extraction; replace UI/rendering paths**. Retain CocoIndex, Tika/LibreOffice normalization and media/source-text processing. Reuse existing per-identity IngestStatus; its counters count work components, not documents.
- Inbox means `reviewed_at` is absent. Suggestions remains the existing `needs_review` filter.
- Custom-field filtering initially offers exact value/unassigned; all five existing types retain their validators. No new numeric/range query language.

## File structure

Backend additions stay in `internal/docfields`, `internal/arcadedb` and `internal/agui`, next to existing document code. Native preview orchestration reuses `internal/documents` and sandbox transport; Craft owns rendering. Frontend additions stay in `web/src/documents`, split into route/state, navigation, overview, list/cards, saved views, uploads and detail concerns. Only route mounting/surface synchronization and hiding document telemetry touch the outer shell.

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

### Task 1b: Separate documents from pictures, video, audio and code

**Files:** `internal/docfields/library_query.go`, `library_search.go`, `library.go`, new focused file-type helper/tests, workspace integration tests; `internal/agui/documents_api.go` and workspace API tests.

**Interfaces:** add `file_type=all|documents|pdf|word|spreadsheet|presentation|text|image|video|audio|code` to GET and bulk filter JSON. Omitted/empty means documents; explicit all accesses all categories. Saved-view validation accepts the same enum. `DocumentRow.file_type` reports its canonical inferred category, including `other` for unknown extensions. Documents includes pdf/word/spreadsheet/presentation/text; unknown files are available only under all. Reuse `filecard.SourceExtensions()` for source-code formats, with JSON/XML/HTML/HTM classified as code/config alongside that registry; prose txt/md/markdown remains text.

- [x] TDD: uppercase extensions, names with multiple dots, unknown types, documents excluding images/video/audio/code, exact groups, query+metadata+kind composition, consistent List/Select/bulk and saved-query validation.
- [x] Implement one shared extension inventory, with case-insensitive SQL suffix predicates and response classification from the same definitions. Include common Office/ODF/macro-enabled/template and media suffixes; unsupported editor formats may still be listed and downloaded.
- [x] Run package unit/race and real disposable ArcadeDB tests, vet/build, then independent review, commit/merge and post-merge verification.

### Task 2: Reuse Craft for document and Office previews

**Files:** focused orchestration beside existing `internal/documents` and sandbox helpers; `internal/agui/documents_preview_api.go` and tests; composition-root wiring only where necessary. No new document/image rendering engine.

**Interfaces:** `GET /api/documents/{id}/thumbnail` returns authorized PNG from the native Craft apps for PDF, supported Word/spreadsheet/presentation families, supported raster inputs and supported video, or explicit unsupported/not-found/error status. Client URL may include `v=<raw_sha256 or indexed_at>`. No externally supplied source URLs. Use existing document lookup, object opener and SandboxRouter.

Also expose `GET /api/documents/ingestion` → `{indexing: IngestState|null}` by reusing the existing
`internal/arcadedb/document_ingest_state.go` reader and tenant resolution, not a second status
store/publisher. Preserve status/observed_at/in_progress/finished/errors exactly. Missing state
is null, query failure is an error. Ready with nonzero errors must not appear successful.

- [x] Inventory full native Craft tool/CLI surfaces and measure PDF, Word, spreadsheet, presentation and photo preview paths. Existing `CraftFrame`, `EditInCraft`, `PreviewByKind`, per-identity SandboxRouter, streamed copy-in/out and native batch protocols are the reuse boundary.
- [x] TDD for scoped preview orchestration, unsupported/corrupt input, cancellation, byte/time/concurrency bounds and cross-identity isolation. Native renderer correctness is tested with actual Craft binaries, not reimplemented.
- [x] Stage in a unique request directory in the caller's existing box; run independent native CLI processes so previews cannot change a mounted agent editor session. Stream input with WriteFileStream and copy out only a bounded PNG. Use PdfCraft bounded render_preview on PDF/native Office exports; use the apps' own image preview capabilities.
- [x] Verify real PDF, Word, spreadsheet, presentation and photo output; document font/layout/import limitations and any explicitly bounded sheet window. Never return SVG/HTML as image content. No whole-stack rebuild.
- [x] Include video per the latest user instruction. Use FilmCraft's native probe/import/newSequence/render path with bounded JSON, returned media IDs, a unique project/data directory and dimension/byte/time guards. Derive a bounded render scale from probed video dimensions; refuse offline placeholders. Verify actual MP4 output and preserve video playback/FilmCraft editing in the UI.
- [x] Add endpoint/tenant/null/error tests for existing ingestion status; do not alter extraction or fabricate per-file failures from aggregate counters.
- [x] Run Go checks, focused race/integration tests and review; commit/merge with fresh targeted verification.

### Task 3: Connected workspace, uploads and saved views

**Files:** `web/src/documents/DocumentsSurface.tsx`, `DocumentsWorkspace.tsx`, `documentsApi.ts`, `FilterBar.tsx`, `BulkBar.tsx`, `bulkModel.ts`, existing tests; new `DocumentsNavigation`, `DocumentsOverview`, `DocumentCards`, `DocumentUpload`, `SavedViews`, route/query hooks and tests; `web/src/i18n/resources.documents.ts`; `web/src/main.tsx`, `AppShell.tsx` and actual telemetry owner.

**Interfaces:** consume Task 1/2 DTOs. Mount `/documents/*` through the existing authenticated AppShell and preserve the document surface on direct navigation. Document routes include overview/library/inbox/processing/lists and `document/:id`; query state uses existing API parameter names plus display/page-size. Legacy Documents entry moves into the new workspace without changing other app destinations.

- [x] Write failing interaction tests for default document entry, sidebar destinations, URL round trips/back/reload, content/name mode, Inbox and Suggestions distinction, empty/error/retry states and stale-result protection.
- [x] Mount the existing route table through React Router's createBrowserRouter/RouterProvider so Task 4 can use the installed useBlocker for unsaved navigation, including browser Back. Preserve auth, lazy boundaries and TanStack fetching; adapt document test fixtures with createMemoryRouter.
- [x] Implement sidebar/drawer and overview using live counts, recent documents and pinned saved views; library has table/compact/cards, visible Upload, explicit sort/page-size, retained filter chips and bulk-selection contracts.
- [x] Write failing uploader tests for multi-file/drop, two same-named files in one batch, real file-manager multipart response IDs, individual errors/retry, unique item keys, ready lookup and library/count invalidation. Preserve pending successful uploads across workspace navigation and avoid replaying upload after refresh.
- [x] Implement upload state with polling of exact source keys; show waiting honestly and offer Files/Open when appropriate. Keep processing status and polling identity-scoped; avoid refetch storms.
- [x] Write saved-view tests for save/apply/rename/delete/pin/dashboard and fresh session/server persistence. Add custom-field filters and bulk field edits with explicit confirmation, reusing existing Diff validation.
- [x] Implement real thumbnails with named fallbacks; responsive cards/compact rows keep names readable. Hide global token/cost telemetry only while working in Documents.
- [ ] Run focused Vitest tests, typecheck, lint and build; inspect both themes and languages; review, commit/merge and fresh targeted verification.

### Task 4: Focused document detail and organization

**Files:** `DocumentDetail.tsx`, `DetailFields.tsx`, `DocumentHistory.tsx`, `LabelsAdmin.tsx`, `CustomFieldsAdmin.tsx`, `ListsPage.tsx`; shared artifact preview dispatch, CraftFrame save/guard behavior and format mapping; new focused detail tabs/navigation/contextual label controls and tests; document translations.

**Interfaces:** consume the routed selection/result context from Task 3 and indexed-content endpoint from Task 1. Existing metadata/preview/editor APIs remain authoritative.

- [ ] Write failing tests for direct-link detail, previous/next in current results, return context, unsaved-change guards, failed label loads, searchable/create-in-context labels and mobile Details/Preview tabs.
- [ ] Implement desktop fields beside preview with the existing resizable-pane library, and clear Details/Content/History tabs. Mobile separates preview from metadata. Preserve every full preview/Craft edit path and provenance control.
- [ ] Route PDF and supported Office formats through the existing CraftFrame in shared preview dispatch. Remove superseded PDF/DOCX viewer code and dependencies once callers move, with replacement behavior tests. Keep existing audio/video players and escaped code text; do not maintain a second Documents-only Office dispatch or broaden active HTML/SVG rendering.
- [ ] Include code-file rendering per the latest user instruction. Reuse TextPreview/HighlightedSource and the existing lazy Shiki engine for original source, with copy/download and readable wrapping. Route canonical Code rows even when their suffix is absent from artifactMeta's short list. Add missing common grammars through existing fine-grained loaders, retain an escaped fallback for unknown languages, bound source reading/highlighting, and detect binary content. Preserve existing sandboxed HTML Source/Preview behavior; never execute other source languages. Test language/theme changes, failed fetches, large files and HTML-source escaping.
- [ ] Preserve native drafts across tab changes and include pending/failed host persistence in Craft's unsaved guard: native dirty=false is not storage acknowledgment. Serialize repeated saves, provide a real failed-save retry, retain converted Save As filenames, and distinguish Save details from file-content Save. Abort stale identity/unmount continuations and test delayed/failed writes and refreshed metadata without losing untouched fields.
- [ ] Add organization name search and visible document counts; retain matching-rule validation and existing custom-field types. Creating a label from detail preserves the unsaved form.
- [ ] Run focused tests, full document regression, typecheck/lint/build; review, commit/merge and fresh targeted verification.

### Task 5: Live acceptance and publication

**Files:** focused `web/e2e` workflow tests; `docs/superpowers/verification/2026-10-21-paperless-documents.md`; plan checkbox updates; only measured PRD/evidence corrections.

- [ ] Exercise the synthetic PDF set on the real backend: upload → waiting → ready → content search → classify → review → saved view → reload/direct link/back; compare against the captured Paperless reference on desktop and 390 px viewport.
- [ ] Check independent identities for saved views/content/thumbnails; confirm unauthorized responses never expose another identity's data. Verify all tested file previews still open and no unsupported controls are shown.
- [ ] Run required backend/web quality, coverage, mutation and live integration gates for changed packages; fix observed defects, not assertions that correctly expose regressions.
- [ ] Obtain whole-change review, address findings and verify the final tree. Record measured limits, screenshot comparisons and test results; do not invent a UX score.
- [ ] Merge task branch into master, run fresh post-merge checks, push using the repository's WSL hook recipe, check CI and publication for that SHA, then pull/apply the published GHCR image set and verify the running commit.
