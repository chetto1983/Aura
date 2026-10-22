# Documents detail and shared preview verification

Date: 2026-10-22. Task 4 on `feat/paperless-documents`, based on
`3621b52c61eebb73920d338dc3387f404fa9e07e`.

## Scope and prior measurement

Implementation reused the existing router/query model, Radix tabs/popover/command controls,
react-resizable-panels, metadata APIs, shared preview dispatcher, Craft bridge and lazy Shiki
engine. Before code changes, the PRD recorded the actual Office imports, converted save names,
early native dirty=false, source expansion and binary/indexed-content measurements from the
dated Craft/source/media evidence. Extraction and native app binaries were unchanged.

`PdfPreview`, `DocxPreview`, their DOCX link scrubber/test, and the orphan `docx-preview`, `jszip`,
`immediate`, `lie`, `pako` and `setimmediate` dependency entries were removed. Replacement tests
assert the shared PdfCraft/WordCraft dispatch and retained public-share source boundary. Images,
audio/video playback and PhotoCraft/FilmCraft editing remain on their existing paths.

## RED and GREEN

Observed failing tests before the corresponding changes:

- Verified Office and canonical source routing; bounded stream cancellation; binary fallback;
  refusal to render incomplete HTML; source copy; stale language-highlight cache.
- Native dirty=false with pending/failed host storage; real Retry; serialized writes and
  aborted pending/queued continuation on unmount.
- Detail Content/History tabs; current Inbox previous/next; failed organization-list disclosure;
  contextual label creation; organization/custom-field name search.
- Confirmed discard followed by a failed review left navigation permission enabled. A new
  failing interaction reproduced this; the fix scopes bypass to the actual navigation call.
- Live organization counts included Code while its drill-down still defaulted to Documents.
  A mixed Office/Code regression failed before explicitly preserving the All files count scope.
- The contextual command search had no accessible name. Its interaction test now selects by
  accessible name; cmdk's own Command/List label props provide localized names.

Additional regression checks cover direct links and return query, browser Back/Stay/Discard,
current-result neighbors across pages, preserving refreshed untouched fields, identity change
between PATCH and review, native iframe lifetime across tabs, original HTML escaping, theme
changes, 25 added common lazy grammars, and size/line tokenization bounds.

Final focused command (from `web`):

```text
npx vitest run src/documents src/chat/artifacts src/chat/displays/__tests__/shiki.test.ts src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx src/routes/SharePage.test.tsx src/files/filesApi src/shell/useRoutedSurface.test.tsx --maxWorkers=4
```

Result: **358 passed, 0 failed, 45 files**. This includes the full Documents regression and
relevant shared preview, media, transport and public-share tests. It is not a whole-frontend
coverage claim. The controller owns the phase's final global gates and live acceptance.

Typecheck and full type-aware lint passed. Production builds passed (4513 transformed modules;
first 19.50 s, mobile correction 17.45 s, final 18.97 s). Stable embedded distributions were handed to
the controller for Linux compilation and live testing. Duplicate scan: **0 clones / 840 files**.
Dead-code scan, full format check and diff whitespace check passed. Supplemental i18n
parity/usage and shared HTML workflow checks: 12 passed across 3 files.

## Focused mutation spot-check

Eight deliberate production mutations were applied individually, tested, then byte-for-byte
restored in a `finally` block. Every mutant produced an actual failing test. The subsequent
358-test regression ran against restored sources.

| Mutant | Observed result |
|---|---|
| Disable the streaming cap | Killed by source bound/cancellation test |
| Disable binary detection | Killed by binary preview test |
| Remove pending/failed storage from unsaved guard | Killed by delayed/failed save test |
| Allow overlapping host saves | Killed by serialized-save tests |
| Remove Retry drain | Killed by retained-payload retry test |
| Remove host-save abort on unmount | Killed by cancellation test |
| Render the untouched form instead of applying user diff | Killed by contextual draft test |
| Force dark highlighting across theme changes | Killed by live-theme regression |

Score: **8/8 killed (100%)** on this declared sample. This is a focused fault-injection check,
not the global Stryker score or exhaustive mutation coverage. The new relevant tests are also
included in the existing Stryker Vitest allow-list. Local reproducible runner/results are in
`.superpowers/sdd/2026-10-21-paperless-documents/task-4-mutation.py` and
`task-4-mutation-results.json`.

## Interfaces and limits

- `DocumentDetail.onOpen(id, offset?)` updates the existing reload-safe result query. Neighbor
  lookup preserves all filters, file family, sort and Inbox semantics; it can scan result pages
  when a copied link's document moved since the saved offset. It makes no large-corpus latency
  guarantee and returns no neighbor for documents no longer matching the result.
- Indexed Content uses the existing identity-scoped `/content?offset=&limit=25` endpoint and
  exposes refresh and paging. Original-source Preview never substitutes indexed passages.
- Metadata draft state holds only intended field changes. Native file-content Save and Save
  details are separate. AbortSignal now reaches existing JSON writes and `saveBeside`; abort
  prevents later client dispatch, but does not roll back a request already stored by the server.
- Original text reads stop at 256 KiB; incomplete HTML is not rendered through srcdoc. Syntax
  work is limited to 65,536 UTF-16 code units, 2,000 lines and 8,192 units per line. Unknown
  grammars remain escaped plain text; binary/non-UTF-8 input offers original download. Copy
  is offered only when the complete bounded original text is available.
- Native Word/Grid conversions retain their returned filenames and save beside the source.
  Failed queue entries retain bytes for Retry; a failure holds later saves until retry succeeds.
  Current-result review cancellation sends no metadata/review request.
- Organization counts cover All files. Label counts use existing facets; custom-field assigned
  counts subtract the existing unassigned query from total, one count query per configured field
  while that tab is open. Failed counts/lists are explicit, never fabricated as zero.

The controller's separate `2026-10-22-documents-detail-live.md` records actual native saves,
mobile dimensions and original-source checks. This implementation evidence does not claim
independent review, merge, publication, every native import/codec, or global accessibility.

## Independent review follow-up: tab relationships

Review approved the main implementation with no Critical/Important findings and one Minor:
the manually rendered metadata panel had no ID matching Radix trigger `aria-controls`, and
mobile Preview had no corresponding tabpanel. New desktop/mobile cases failed on that exact
missing relationship before the fix.

Detail now uses existing `TabsContent` for Details, Content, History and Preview. Preview uses
the installed Radix `forceMount` capability with explicit mobile visibility/inertness, keeping
the native iframe alive. Tests assert every trigger's referenced panel exists (including
inactive panels), reciprocal `aria-labelledby`, selected panels' accessible names, the same
iframe and contentWindow across mobile Preview/Details/Preview, and the retained dirty guard.

Follow-up verification: detail/detail-workspace/native-save tests **23 passed / 3 files**;
typecheck via production build PASS (4513 modules, **15.86 seconds**); scoped type-aware lint,
format and diff whitespace checks PASS. The stable dist was handed to the controller for its
final runtime check. This follow-up does not broaden the feature scope.
