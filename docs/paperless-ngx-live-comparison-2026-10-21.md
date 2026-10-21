# Paperless-ngx and Aura: live workflow comparison

Date: 2026-10-21, Europe/Rome. Status: measured comparison; implementation is pending.

## Scope and running versions

The user requested studying Paperless-ngx's UI, UX and functionality, then bringing its workflow
to Aura because Aura is hard to use. They specifically required both stacks running, Aura on the
latest commit, and published GHCR images rather than local builds.

- Aura: `http://127.0.0.1:9080`, commit `9ef3a2e73128a4dba6016c719a7bcc931e04452d`.
- Paperless-ngx: `http://127.0.0.1:8010`, version **3.3.3**; isolated Compose project
  `aura-paperless-study`, SQLite, Valkey, Tika and Gotenberg, bound to loopback.
- Aura initially ran `48708ec62`. A healthy container was not evidence of current code. The old
  instance was replaced with GHCR images; `aura version` and image revision labels identify HEAD.
- The local `.env` now uses GHCR images and pull policies. Data volumes were retained. Postgres
  was backed up to `backups/before-ghcr-9ef3a2e73-20261021.dump` before applying the update.
- A local build was started before the user's GHCR correction, then stopped. The deployed images
  are the published images, not results of that aborted build.

The exercised corpus is three synthetic PDFs: two Northstar invoices and a Cedar service
agreement. All contain `UXSTUDY20261021`. They were uploaded through each application's UI.
Aura had 52 indexed documents before the upload; the synthetic corpus added three. Existing
documents were not edited. Only sample invoice types and the sample agreement's date/review
state were changed in Aura. A sample saved view was created in Paperless.

Evidence folder: `output/playwright/paperless-comparison/` (gitignored). Screenshots were captured
in this run and opened for inspection. Desktop captures are generally 1440×900; the first Aura
library capture was 1920×889. Mobile captures are desktop Chromium resized to 390×844, **not** an
iOS or Android device test. Aura's existing English/dark preference and Paperless's Italian/light
preference are visible; color differences alone are not scored as usability defects.

## Main finding

Aura has much of the document machinery, but its UI makes the operator bridge separate tools:
Folders for upload/file actions, Documents for fields, Chat for content retrieval, Lists and rules
for classification, and application Settings for other controls. Paperless keeps the main archive
tasks connected through persistent navigation, search, saved views and a document detail screen.

The highest-value work is a coherent document workspace with working navigation and state, not
copying green buttons or replacing Aura's storage and agent architecture.

## Measured journeys

| Step | Task and result | Health | Evidence |
|---|---|---|---|
| 1 | Enter the archive. Paperless has a named Documents destination, dashboard and persistent management links. Aura has Documents → Folders/Documents; a fresh preference defaults to Folders in code. | Aura needs simplification | `01-paperless-dashboard.png`, `03-aura-library-default.png`, `04-aura-folders.png`; `DocumentsSurface.tsx` |
| 2 | Upload three PDFs. Paperless exposes upload on the dashboard and reports per-file processing with Open document. Aura requires Folders → Add New → Upload file; the resulting file tiles have generic icons. | Aura loses the connection between upload and indexing | `05-aura-upload.png`, `23-paperless-processing.png`; upload DOM snapshots |
| 3 | Find the new uploads. At 21:17:24 Aura showed 0 matches for `UX-study`. At 21:18:18 its API returned all three, indexed at 21:17:58, while the visible library still showed 0. Changing the search fetched the new result. | High-priority defect | `06-aura-indexing-gap.png`; `/api/documents?q=UX-study&limit=50` |
| 4 | Search inside a document. `Cedar` returned the sample contract in Paperless. The same text returned 0 rows in Aura's library. Searching the filename found it. | High-priority capability gap | `07-paperless-content-search.png`, `09-aura-content-search.png`; `internal/docfields/library.go` |
| 5 | Open and review. Both show preview and fields together. Aura's date save and Reviewed, save and next persisted correctly. Paperless also exposes title, ASN, text, metadata, notes, history, permissions and file versions. | Aura's existing detail is a useful foundation | `08-paperless-detail.png`, `10-aura-detail.png`, `11-paperless-extracted-text.png` |
| 6 | Refresh an open document. Aura went from the sample invoice detail at `/` to the unfiltered library at `/`; the search became empty. Paperless remained at `/documents/2/details` after reload. | High-priority navigation defect | Live URL/heading checks; local React state in `DocumentsWorkspace.tsx` |
| 7 | Organize types/tags/counterparties. Aura has CRUD, document counts and manual matching rules. Its lists are long rows with no visible name search. Paperless provides searchable tables, tag colors/hierarchy/inbox flags and object permissions. | Partial parity; improve discovery and scale | `12-aura-attributes.png`; both rule/tag editor DOM snapshots |
| 8 | Bulk edit. Aura selected exactly the two sample invoices, assigned existing type `fattura`, showed a two-document confirmation, and persisted both values. Paperless's selection toolbar additionally offers custom fields, storage paths, permissions, download, send, delete and document actions. | Core Aura operation works; action coverage is smaller | `14-paperless-bulk.png`, `15-aura-bulk.png`; readback of the three sample rows |
| 9 | Save a frequent search. Paperless saved `Cedar` as `UX study - Cedar` and displayed it in both sidebar and dashboard after navigation. Aura's document area has no saved-view control or document-view API. | Major repeat-work gap | `16-paperless-saved-view.png`; document route inventory |
| 10 | Work on a narrow screen. Aura's table clips sample filenames and has horizontal scrolling; the filter/header and runtime footer take considerable space. Paperless presents cards with previews and moves the preview into a detail tab. Both still have dense controls. | Aura needs a mobile-specific presentation | `17-aura-mobile-library.png` through `20-aura-mobile-detail.png` |
| 11 | Automate classification. Aura has label matching rules. Paperless has a separate workflow editor with triggers and actions. Its creation form was inspected and cancelled, not executed. | Different capability scope | `13-paperless-workflow.png`; editor DOM snapshots |
| 12 | Monitor and recover. Paperless exposes searchable processing tasks; moving the synthetic contract to trash showed 29 days remaining, and Restore returned it at the same `/documents/2/details` route. Aura's document UI exposes neither processing status nor trash; file deletion is in Folders. | Recovery/status gap | `22-paperless-trash.png`, `23-paperless-processing.png`, `24-paperless-trash-restore.png`; file/document route inventory |
| 13 | Configure mail and application preferences. Paperless separates mail accounts/rules and general/document/permissions/notification preferences. Aura's settings center on profile, model routing, budgets, backends, identities, channels, conversation sharing and remote access. | Preserve Aura's capabilities; add document-specific entry points | `21-paperless-mail.png`; settings UI and `settingsSections.ts` |

The file originally called `02-paperless-upload.png` captured the completed document **library**,
not the transient upload state. It is evidence for thumbnails only; processing evidence is the
observed upload DOM plus screenshot 23. No upload-duration benchmark is claimed.

## Capability matrix

Legend: **Executed** = a live operation and its result were checked; **Inspected** = live controls
were examined; **Source** = repository/docs evidence, not a completed live operation. “Missing”
below refers to the document workspace/API inspected, not a claim that Aura has no related
capability anywhere.

| Capability | Paperless 3.3.3 | Aura at the tested commit | Action |
|---|---|---|---|
| Document home/dashboard | Saved-view dashboard, upload, counts (executed) | Opens the selected application surface; no document dashboard | Add document home/inbox |
| Stable navigation | Sidebar destinations and open documents (inspected) | Top-level app tabs plus a second Folders/Documents switch | One document navigation model |
| Upload | Multi-file upload; global drop documented; per-file progress observed | Multi-file upload works in Folders | Offer Upload from the document workspace |
| Processing visibility | Stages, success/failure tasks, direct open links (executed/inspected) | Files appear before indexed rows; no document-level progress in the UI | Connect upload records to ingest status |
| Library refresh | Upload task links lead to completed documents | Loaded result stayed empty after indexing completed (executed) | Invalidate/refetch from actual ingest state |
| Review/inbox meaning | Inbox is configured with tags/workflows/saved views | `needs_review` describes pending suggestions; fresh unclassified samples had false | Distinguish unreviewed uploads from pending suggestions |
| Content search | Title + content mode found `Cedar` (executed) | Library query is a filename substring; agent retrieval exists separately | Expose existing retrieval through a paged UI contract |
| Search modes | Title/content and advanced syntax (inspected/source) | Filename search only in the library | Start with explicit name/content modes; define advanced parity separately |
| Search results | Preview cards; list/small/large card controls; searchable PDF (inspected) | Text grid; folder tiles show file-type icons | Add real preview thumbnails and useful result context |
| Filters | Tags, correspondent, type, path, dates, permissions; extended filters documented | Include/exclude, any/all tags, unassigned and date range exist | Retain current semantics and improve presentation |
| Dates/sort | Created/added/modified options and sort menu | UI sorts name/date; API additionally supports indexed time | Expose explicit sort choices and date semantics |
| Saved views | Search saved, pinned to sidebar/dashboard (executed) | Document saved views absent; Board saved views already exist elsewhere | Reuse Board persistence pattern where suitable |
| URL state | Filter query and document/tab in URL (executed) | Open document, filter, sort and offset are component state; reload loses them | Add routes and serializable query state |
| Detail preview | PDF.js-style preview integrated with fields (inspected) | Native PDF preview plus Craft Edit; other format viewers exist | Preserve format viewers; unify document actions |
| Content editing | Extracted text is editable in Content tab (inspected) | No extracted-text tab in DocumentDetail | Add readable extracted content before considering OCR corrections |
| Title versus filename | Separate document title and original file metadata | Document heading uses `file_name`; rename belongs to Folders | Define title semantics without breaking stable document IDs |
| Metadata editing | Correspondent/type/tags/path/ASN/date (inspected) | Counterparty/type/tags/date/custom fields; save works (executed) | Add missing fields only with defined storage semantics |
| Create labels in context | Plus controls beside fields (inspected) | Detail selects existing labels; creation is on Lists and rules | Search/create from the field without losing the document |
| Tags | Colors, hierarchy, inbox flag (inspected) | Flat labels with rules | Add display/color first; hierarchy changes need explicit contracts |
| Custom fields | Multiple types, filters and bulk support (source/inspected) | Date, amount, integer, text, choice; detail and column support (inspected/source) | Add filtering/bulk support before broader field types |
| Bulk operations | Metadata, permissions, download, send, delete and PDF actions (inspected) | Type/counterparty/tags with explicit summary and history (executed) | Keep confirmation; add supported actions incrementally |
| Review history | History tab (inspected) | Field provenance, suggestions, rejection and reviewed state (executed/source) | Preserve Aura's provenance model |
| Notes | Dedicated tab (inspected) | No document notes in inspected document routes | Add durable notes with author/time |
| File versions | Version controls and merge-as-versions documented | No document version UI/API; editing is a different capability | Design actual version history; do not relabel edits as versions |
| PDF transformations | Merge/split/rotate/reprocess documented and actions exposed | Craft PDF editor is present | Inventory Craft's capabilities before adding transformations |
| Download/export | Per-document and bulk controls; original/archive choices | File-manager downloads and viewer save; not a unified library toolbar | Reuse download paths and add selection-aware export |
| Sharing | Document link/email controls and share management (inspected/source) | Conversation sharing exists; no document-detail Share action | Do not claim conversation sharing equals document sharing |
| Trash/restore | Synthetic contract moved to trash and restored under the same ID (executed); retention expiry was not tested | File delete route; no document trash contract found | Implement recoverable deletion before exposing library Delete |
| Automation | Intake/document-change/scheduled workflows (source; editor inspected) | Matching rules and agent/scheduler capabilities are separate | Define document trigger/actions; reuse scheduler where appropriate |
| Mail import | IMAP accounts, rules and processed-mail UI (inspected) | PIM MCP/email integration, not the same intake-rule UI | Audit PIM ingestion contracts before promising parity |
| OCR/formats | PDF/image OCR, text, Office via Tika/Gotenberg (PDF executed, rest source) | CocoIndex/Tika/LibreOffice/filecards and media ingestion | Retain Aura's ingestion stack; compare formats with fixtures |
| Duplicates | Detection and configurable duplicate handling (source) | File collision handling and ingestion identity differ | Test identical bytes, same name and new versions separately |
| Permissions | Global and per-object/document rights (inspected/source) | Per-identity libraries and app capabilities | Preserve tenant isolation; define any new sharing separately |
| AI | Optional AI suggestions/document chat (source; not configured here) | General agent, retrieval, graph, tools and multi-format editing | Keep Aura's AI strengths; add contextual document entry points |
| Keyboard/accessibility | Document shortcuts documented; some mobile controls icon-only | Grid/file-manager keyboard support; some buttons expose font glyphs as names | Verify keyboard names/focus/order; do not claim compliance |
| Mobile | Cards and separate detail Preview tab (inspected) | Horizontal table, stacked preview, global telemetry/footer (inspected) | Cards/compact list and Preview/Details tabs |

## Priority findings

1. **P0 — Uploaded documents appear to disappear.** File upload succeeds, but the document list
   has no queue state and can remain stale after ingestion. Show a visible per-file lifecycle,
   connect its completed ID to Open, and refresh the library when indexing changes.
2. **P0 — Search does not meet the archive mental model.** `Cedar` is in the document but not the
   filename. Expose content search with actual total counts, paging and filter compatibility.
   The agent's top-k results are not a substitute for an archive list.
3. **P0 — Navigation state is disposable.** Deep links, refresh, browser Back and returning from
   detail must preserve search, filters, sort, page and selection rules.
4. **P1 — The work is split between views.** One Documents area should expose Upload, All
   documents, Inbox/review, Saved views, Types, Counterparties and Tags in persistent navigation.
5. **P1 — Review count is easy to misread.** The three new documents were not reviewed, yet
   To review was 0 because there were no suggestions. Separate “unreviewed” from “suggestions.”
6. **P1 — Repeat work cannot be named.** Save a search/filter/sort/display state and reopen it
   from the sidebar. Persist it per identity; browser-only storage is not cross-device parity.
7. **P1 — Visual scanning is weak.** Real thumbnails, human titles and readable date/type/tag
   summaries are more useful than folder glyphs or rows full of dashes. Empty results need an
   explanation and recovery actions. Existing runtime cost/token telemetry distracts here.
8. **P1 — Mobile needs its own layout.** Use a compact list/cards; keep names readable and put
   metadata and preview in tabs. Keep selection and primary actions reachable without swiping a
   desktop table sideways.
9. **P2 — Full archive parity needs backend work.** Notes, versions, document sharing, recoverable
   deletion and workflow triggers cannot be delivered by adding inactive buttons.

## What should not be copied blindly

- Paperless extracted the agreement's `2026-08-01` as **8 January 2026**, and the first invoice's
  `2026-10-01` as **10 January 2026** in this configuration. The second invoice displayed the
  ingestion date. This is evidence that automatic date proposals need review, not a universal
  accuracy comparison. Aura's sample date was absent until entered manually.
- Paperless's narrow layout has many icon-only controls and a horizontally scrolling detail tab
  strip. Copy the responsive task flow, not every control arrangement.
- Clicking a card title did not open the document in the tested narrow layout; the explicit Open
  action did. Aura should give the title an unambiguous open action.
- Aura's AI, graph, board, studio, multi-format viewers/editors and tenant isolation have distinct
  purposes. A document redesign must not erase those capabilities or move data into Paperless.

## Reuse inventory and likely seams

The existing `docs/paperless-ngx-ui-behaviours-2026-10-18.md` and backend study from 2026-10-17
already inventoried Paperless source without running it. This report adds live evidence rather
than replacing that research. The 2026-10-18 document-fields design deliberately excluded saved
views, nested tags, per-document permissions and a full-text query language; it also records that
the operator dropped versioning on 2026-10-17. Those differences were originally scope choices.
The new request makes them relevant for comparison, but it does not justify silently declaring
every previously excluded feature approved for implementation.

| Existing Aura surface | Keep/reuse | Missing connection |
|---|---|---|
| `web/src/documents/DocumentsSurface.tsx`, `DocumentsWorkspace.tsx` | Existing query/filter/selection behavior | Coherent navigation, upload entry, routes, display modes |
| `FilterBar.tsx`, `FieldMenu.tsx`, `documentsModel.ts` | Include/exclude/unassigned and any/all chips | Saved views, clear search modes, URL serialization |
| `BulkBar.tsx`, `useBulkEdit.ts`, `BulkConfirmDialog.tsx` | Selection scope, explicit confirmation, field diffs | More supported operations and filter context while selected |
| `DocumentDetail.tsx`, `DetailFields.tsx`, `DocumentHistory.tsx` | Edits, review, provenance, preview/Craft action | Route identity, contextual label creation, notes/text/actions |
| `web/src/files/filesApi.ts`, `FilesWorkspace.tsx` | Actual file upload/direct/download/rename contracts | Document upload must report the subsequent ingest state |
| `internal/agui/documents_api.go`, `documents_labels_api.go`, `documents_bulk_api.go` | Authenticated tenant-scoped API | Search/read models and durable view/notes/lifecycle contracts |
| `internal/docfields/library.go` | Library pagination, facets, sort and field filters | Filename-only search; unreviewed lifecycle separate from suggestions |
| `internal/arcadedb/document_lexical.go`, `document_retrieval.go` | Existing content index and retrieval primitives | Exhaustive archive-search contract; don't copy top-k agent behavior |
| `web/src/board/*`, `internal/agui/board_views_api.go` | Saved-view precedent | Document scope/identity ownership and schema validation |
| `web/src/components/ui/*`, React Router, TanStack Query, SVAR Grid/Filemanager | Existing UI/routing/query libraries | Compose them consistently; verify their public APIs before extensions |
| `internal/agui/share_api.go` and `internal/share` | Expiry/capability principles | Existing service is conversation-based, not generic document sharing |
| Craft viewers/editors and `PreviewByKind` | Real content viewing/editing | Thumbnail/preview permissions, caching and document actions |

## Evidence limits

This is a live task-based comparison plus a source/documentation inventory. It is **not** an
exhaustive behavioral equivalence proof or a production acceptance run. Only the three synthetic
PDFs were exercised. OCR on scanned images, Office conversion, mail-server integration, AI
configuration, multi-user permission enforcement, versions/merge behavior, retention expiry, archive-size
performance and native mobile devices were not executed. Forms viewed then cancelled are marked
Inspected. No arbitrary UX score or performance percentile is assigned.

## Sources

- [Paperless-ngx repository](https://github.com/paperless-ngx/paperless-ngx)
- [Usage documentation](https://docs.paperless-ngx.com/usage/)
- [Usage source pinned to the tested v3.3.3](https://github.com/paperless-ngx/paperless-ngx/blob/v3.3.3/docs/usage.md)
- [Official Compose example](https://github.com/paperless-ngx/paperless-ngx/blob/main/docker/compose/docker-compose.sqlite-tika.yml)
- [Configuration documentation](https://docs.paperless-ngx.com/configuration/)
- [Aura publication for the tested commit](https://github.com/chetto1983/Aura/actions/runs/39648301802)

The documentation site's root initially returned HTTP 403 to the web fetcher. Official
documentation source was read through GitHub, and the actual UI was inspected on the local stack.
