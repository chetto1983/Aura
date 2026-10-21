# A Paperless-style document workspace for Aura

Date: 2026-10-21. Status: Documents-area scope confirmed by the user; implementation underway.

Evidence: [live comparison](../../paperless-ngx-live-comparison-2026-10-21.md).
Reference: locally running Paperless-ngx 3.3.3. Aura baseline:
`9ef3a2e73128a4dba6016c719a7bcc931e04452d`, deployed from GHCR.

## Intent

Make importing, finding, organizing, reading and reviewing documents as connected and discoverable
as they are in Paperless. The user asked to study both stacks and duplicate Paperless's UI/UX and
functionality because Aura is hard to use. The working assumption is that this redesign belongs
inside Aura's Documents area. Replacing navigation for Chat, Board, Studio, Graph and Governance
is outside the confirmed scope. The user chose: "Rebuild the Documents area using Paperless’s
workflow."

The implementation should reproduce useful behavior and information architecture in Aura's React
application. Do not embed Paperless or move tenant data into its database. The user explicitly
permits copying Paperless code; any reused code must retain its provenance and applicable license
terms. Reuse existing Aura components where they already provide the behavior.

## Calls made by the designer

These are proposed choices, not statements that the user already approved them:

- Keep Aura branding, existing theme tokens and light/dark support. Adopt Paperless's archive
  layout and behavior rather than its logo or exact green palette.
- Keep the existing global app destinations available. Put document navigation inside Documents.
- User correction: reuse the existing Craft applications for PDF and all supported Office file
  families, including Word, spreadsheets and presentations. Do not build a separate renderer.
- User correction: default the Documents library to PDFs, Office and text documents. Pictures,
  video, audio and code each have an explicit file-type filter; All files remains available.
  Inspect ingestion and reuse its actual status/classification mechanisms.
- User added "video too": include video thumbnails through native FilmCraft as well as the
  explicit Video filter, existing playback and FilmCraft editing. Keep video extraction's
  existing metadata-only behavior; thumbnail rendering is independent of ingestion.
- User added "look also code file and render": Code files use Aura's existing highlighted
  source viewer with copy/download, language-appropriate highlighting and a readable text
  fallback. Original source is distinct from indexed passages. Preserve the existing sandboxed
  HTML Source/Preview behavior; large or binary inputs need an explicit bounded fallback.
- User clarified: "Keep extraction; replace UI/rendering paths". Existing CocoIndex,
  Tika/LibreOffice normalization and media/code extraction remain. Superseded UI/rendering code
  is removed; no parallel legacy viewer or default-all compatibility branch is introduced.
- Use the existing backend, index, object store, auth, query library, route library and UI
  primitives. Existing components are the starting point; a new dependency needs an inventory.
- The proposed implementation boundary is increments A and B: the connected daily document
  workflow. C and D describe optional capability extensions for a later explicit scope decision.
  Completion of A/B must never be reported as full Paperless feature parity.

The prior document-fields spec (2026-10-18) explicitly excluded saved views, nested tags,
per-document rights and a full-text query language, and records the operator dropping versioning
on 2026-10-17. This proposal reopens saved views as a concrete usability improvement. It does not
silently reinstate versioning or promise all other exclusions are now required.

### Explicit feature boundary

Included in proposed A/B: navigation, upload/status, unreviewed inbox, content search without a
new query language, filters, URL state, display modes/thumbnails, saved views, contextual label
creation, metadata review, searchable lists and the existing five custom field types with
filter/bulk support. Existing viewers, editors, file actions and tenant isolation remain.

Outside A/B: ASN, storage-path templating, tag hierarchy, new custom-field types, advanced search
syntax, per-document grants, new PDF transformation tools, notes, versions, trash, document share
links, intake workflows and mail rules. C/D are options for the last six areas and related rights;
the rest require a separate scope decision. No new learned classifier or AI provider is proposed.

## Target information architecture

On desktop, Documents has a persistent approximately 224–240 px sidebar and a main pane. A compact
sidebar mode is available. On a narrow screen it becomes a drawer, and the current destination is
always named. Preserve one obvious way to return to the document list.

Sidebar destinations:

1. Overview: upload, document counts, inbox work and pinned saved views.
2. All documents: complete library with filters and three display modes.
3. Inbox: documents that have not been reviewed, including newly uploaded documents with no
   extracted suggestions. A separate Suggestions filter identifies pending machine proposals.
4. Processing: uploading, waiting, indexing, ready, failed and duplicate states when the actual
   pipeline can distinguish them; explain failures and expose supported retry actions.
5. Saved views: named searches and filters, pinned in the sidebar and optionally Overview.
6. Organization: Types, Counterparties, Tags, Custom fields.
7. Files/folders: preserve the file manager as an advanced organization tool, with a clear
   label; do not make it the implicit default archive.
8. Later lifecycle destinations: Trash, Sharing, Workflows and Mail intake, added as their
   underlying functionality is delivered.

No inactive placeholders should imply those later functions already work.

## Core interaction contracts

### Import and processing

Upload is visible from Overview, the library and Inbox. Accept multiple files and drag/drop within
the document workspace. Each file has a stable entry that reports the real upload/ingest state.
A completed upload is not called searchable until indexing confirms it. The status entry links
to the completed document. The library and counts update after ingestion without requiring a new
search. Retry must not duplicate a successful upload.

Keep using the existing upload endpoint and ingestion pipeline. First measure the available
status/identity linkage; if a field is absent, specify and test a backend contract instead of
guessing progress from a timer. Existing objects remain visible through Files while indexing.

### Library, search and filters

The file-type filter includes Documents, PDF, Word documents, Spreadsheets, Presentations, Text,
Pictures, Video, Audio, Code and All files. Documents is the UI default and excludes pictures,
video, audio and code. The API uses the same Documents default; All files is explicit. Type inference is
explicitly filename-extension based because IndexedDocument does not store a MIME type.

Provide table, compact list and thumbnail-card modes. Cards use actual generated previews, with
file-type fallbacks. They show readable title/filename, date, type, counterparty, tags and review
state. Clicking a title opens the document; selection has its own explicit target.

Provide clearly named filename and content search modes. Content results must include the Cedar
sample when searching `Cedar`. Reuse the existing content index, with a documented library-search
contract for stable paging, total count, deduplication and filter composition. Agent retrieval's
top-k cap must not silently truncate archive results.

Keep current include/exclude/unassigned and any/all tag semantics. Retain explicit active-filter
chips and Clear filters. Expose sort direction, indexed/added time versus document date, and
page-size choice. Empty/loading/error states explain what happened and offer a useful next step.
Background refresh must preserve the current user's search and edits.

Search, filters, sort, page and display mode are serializable in URL state. Open documents have
stable routes. Browser Back, refresh, a copied authenticated URL and closing detail preserve the
appropriate library context. Unknown IDs produce a useful not-found state without leaking a
different tenant's document.

Saved views persist per identity on the server and contain a versioned, validated query/display
definition. They are reachable on another signed-in device. Rename/delete/pin operations have
clear outcomes. Reuse the Board saved-view pattern if its ownership model fits.

### Review and document detail

Desktop detail places editable metadata beside the preview, with a resizable division using the
existing pane library. Keep Save, Discard, Reviewed/save-and-next, and explicit previous/next
navigation within the current result. Never replace operator values silently with suggestions.
Keep evidence, provenance, rejection history and an unsaved-changes guard.

Organize Details, Content and History into clearly labeled tabs; add Notes, Versions and
Permissions only if the optional increment is accepted and its contracts exist. File information distinguishes a display title
from the stored filename. Type/counterparty/tag selectors support searching and creating a label
in context without leaving the document. Existing custom field types remain supported.

Retain every currently supported preview and Craft editor. PDF download/print/edit actions must
have understandable labels outside any browser-specific viewer toolbar where needed. Text content
is readable and searchable; editing extracted/OCR content requires an explicit reindex contract.

Card/page previews reuse native Craft export/render capabilities in an isolated request process:
WordCraft for Word-family files, GridCraft for spreadsheet-family files, DeckCraft for supported
presentations, PdfCraft for PDF, PhotoCraft for images. Keep existing audio/video viewers and
Craft editors. Unsupported legacy formats must retain download access and honest labeling.

Mobile uses Details and Preview tabs rather than forcing the entire desktop PDF above the form.
Primary actions remain reachable. Names wrap or truncate with a way to read the full name.
Document work does not reserve a persistent footer for tokens/cache/cost/context telemetry.

### Bulk work

Keep selection scope explicit: this page versus all filtered results. A new filter clears or
explicitly re-scopes selection, following the current safe behavior. Retain the existing change
summary and confirmation. Type/counterparty/tag bulk edits remain functional throughout.

Add custom-field edits, download/export, review and lifecycle actions only when real APIs support
them. Failed or partially completed operations must identify the affected documents and be
retryable without reapplying successful writes. Do not imply arbitrary all-results operations
with a UI-only selection list.

### Archive lifecycle and automation

Optional parity extensions include notes, file version history, recoverable deletion, document
sharing, document-specific rights, and document automation/intake rules. These are backend
features, not cosmetic menu additions, and are not part of the proposed A/B completion boundary.

- Notes retain author/time and stable document identity.
- Versions keep immutable file history with explicit preview/download selection; metadata stays
  attached to the root record. Test rename/move/edit survival with the existing document identity.
- Trash hides recoverable documents from ordinary search while retaining bytes and fields until
  a documented retention/purge operation. Restore must return the same identity and metadata.
- Sharing must define the exported document/version and permissions; existing conversation shares
  must not be repurposed without a contract and isolation tests.
- Document workflows define trigger, condition, action, order, enabled state and execution history.
  Inventory the existing rules, scheduler and MCP/PIM capabilities before implementing missing
  pieces. Email intake must distinguish scanning mail from sending mail and must not modify a
  real mailbox during tests.

## Proposed delivery order and optional extensions

| Increment | Scope | Required live exit evidence |
|---|---|---|
| A — Coherent daily workflow | Document navigation, upload/processing visibility, live refresh, content search, stable routes, mobile list/detail, meaningful empty/error states | Upload → indexed → searchable → open → edit/review → return; refresh/back/deep-link preservation; desktop and mobile; real index, not mocked results |
| B — Repeat work and organization | Server saved views/dashboard, thumbnails, contextual labels, searchable organization screens, custom-field filters/bulk editing | Save a query and reopen after a fresh browser context; assign metadata to exactly the selected sample set; thumbnail permissions; no edit loss |
| C — Optional archive lifecycle | Notes, document download/export/share, trash/restore and explicit document rights; file versions only if the operator reopens the prior decision | Save/reload notes; delete/restore synthetic file with identity/metadata retained; second-identity isolation and revoked-link checks; version tests only if included |
| D — Optional intake and automation | Workflow trigger/actions/history, mail intake UI, duplicate handling and format-specific gaps | Disposable mail server and synthetic attachments; deterministic workflow effects; retry/idempotency; PDF/image/Office corpus; no external messages |

An executable plan for each increment is written after its backend contracts and prerequisites
are measured. Reserve migration numbers at execution time from the current migration directory.
Record a PRD amendment only for decisions supported by those measurements.

## Acceptance and test boundaries

- All existing document field/review/bulk and file-manager contracts keep working.
- Newly uploaded unreviewed files cannot disappear behind a stale zero-result table or a
  misleading zero-suggestions count.
- `Cedar` finds the synthetic contract through UI content search; filename mode behaves as named.
- Metadata edits and review state survive navigation, reload, rename and move.
- On 390 px width, core list tasks do not require horizontal table scrolling; document controls
  have accessible names and keyboard focus order is usable.
- Tenant-scoped reads/writes and saved views have real isolation tests. No data moves into the
  comparison Paperless instance except synthetic fixtures.
- Use realistic regression/integration tests plus live E2E. Follow repository Go/web gates for
  touched code, mutation/coverage requirements and dated verification records. Screenshot
  inspection does not establish full accessibility compliance or native-mobile behavior.
- Ship no placeholder buttons and make no full-parity claim until all accepted increments pass.

## Remaining decisions

The user confirmed Paperless-style navigation inside Documents, with the proposed A/B boundary
forming this implementation plan.
Any request for literal full feature parity must also reopen the named exclusions explicitly.
Detailed
database changes, shared-query interfaces, thumbnail implementation and lifecycle policy defaults
are left to measured design/planning within the existing architecture, not invented in this spec.

Native-mobile behavior, OCR/Office conversion quality, permission enforcement, large-corpus search
performance, Aura restore/version operations and mail automation are not proven by the current
comparison. Paperless's synthetic trash/restore was executed successfully. Each unproven area is
a required later measurement for its accepted increment.
