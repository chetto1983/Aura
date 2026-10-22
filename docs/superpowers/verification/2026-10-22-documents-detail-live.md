# Documents detail and renderer live checks

Date: 2026-10-22. These checks use the source-built temporary binary in the already-pulled GHCR
Aura container. The first detail build is an uncommitted Task4 snapshot based on `3621b52c6`;
it is not the final published revision. Only synthetic files are edited.

## First detail build

- Rust opened in the original-source viewer with its complete 209-byte source and22 syntax
  spans. The desktop metadata/preview panes measured422/784px in a1440px viewport. Screenshot
  `aura-detail-rust-first.png` was opened and inspected.
- The SQLite `.db` showed a binary/unsupported-encoding message and download access, with no
  source body or Copy action. A1,225,037-byte Python source showed262,144 characters, an explicit
  256KiB preview notice and the original download. It used the plain-text large-source fallback.
- HTML rendered in the existing `sandbox="allow-scripts"` iframe. Switching to Source showed
  the original markup (388 characters/78 styled spans) and no active script/style nodes inside
  the source view. Indexed passages remain a distinct Content tab.
- Native WordCraft opened the real synthetic DOC. Ctrl+S emitted a5957-byte DOCX. A deliberately
  intercepted503 storage response produced a visible failure and Retry. Clicking the Library
  link was blocked by the unsaved dialog; Stay retained the frame. Retry then stored the real
  DOCX and displayed `Saved as UX-study-office.docx` (localized in the Italian UI).
- A native Word edit inserted `UX_NATIVE_DRAFT_20261022`. Preview/Content/Preview tab changes
  retained the identical iframe, one native ready event and dirty=true. The native Save button
  then stored6001 bytes; reading the persisted DOCX ZIP confirmed the marker in `word/document.xml`.
  During that save, native dirty=false had already arrived while Aura still displayed Saving.
  The final host notice appeared only after storage completed.

## Findings and subsequent build

The mobile first build retained both resizable outer panes:182px and208px at390px viewport.
The CSS classes hid inner content while the library's outer display:flex containers still took
space. Details used half the screen and source preview was166px wide. Both screenshots were
opened: `aura-detail-code-mobile-first.png` and `aura-detail-code-mobile-preview-first.png`.
The implementer replaced this with the installed pane library's0/100% mobile constraints while
retaining the native iframe. A duplicate original-download action was consolidated through the
existing preview-action slot. The second build measured390/0px for Details and0/390px for Preview;
fields/source measured359px, document width stayed390px, and one original-download link remained.
All six primary header actions and all four mobile tabs measured44px high.

A separate source inspection identified a navigation bypass surviving a failed confirmed review.
The implementer reproduced it with a failing regression and scoped permission to the actual
synchronous navigation call. Identity-change and untouched-field refresh regressions are also
reported green. Independent review approved functional scope and quality, with one minor tab/panel
ARIA relationship correction. Fix `6a9f8a4cb` uses the existing TabsContent components; scoped
re-review approved it without further findings. A final live run found matching panel IDs and
reciprocal aria-labelledby for all four tabs. A real native Word draft retained the same iframe
and contentWindow across Preview/Details/Preview, remained guarded, and was explicitly discarded
without persisting that final test edit.

Native-app warnings seen during the Word run were the existing preload/GPU/egui/dictionary
warnings documented in the earlier Craft inventory. The one503 console error was the deliberate
storage-failure injection. A synthetic keyboard save after restoring mobile tabs emitted no save;
the native Save control succeeded. This does not establish a general native shortcut failure.

Screenshots are under `output/playwright/paperless-comparison/`; no credentials or raw native
save payloads were copied into the public comparison report.

## Additional completed live workflows

- ODS imported with GridCraft's existing data-only warning. Native Save emitted4141bytes as
  `UX-study-office (imported).xlsx`. Holding the real storage request kept Saving visible;
  choosing Review-next and then Stay sent zero document PATCH/review requests. Releasing the
  write produced the actual Saved-as notice.
- Creating `UX study office 2026` in context preserved a date draft. Save details persisted both
  operator values and their history on the synthetic ODS. Organization search found its name.
  A mixed Office/Code test exposed an All-count/default-Documents drill-down mismatch; the fix
  was rechecked with three members (one spreadsheet, two code files), and both count and results
  were three under explicit All files. Contextual command search, dialog and list now have
  localized accessible names.
- A real background refresh brought an externally assigned type into an open IEC source detail
  without clearing its date draft. The following Save PATCH contained only document_date; the
  externally assigned type remained stored.
- Previous/Next crossed the25-item boundary of an actual50-document result, with offset0→25→0
  and the correct document IDs. Reviewing the synthetic C++ file with no suggestions advanced
  to the next Code Inbox result and retained its query/destination.
- The WAV played1.266seconds of its2second duration with readyState4 and no media error. The
  synthetic H.264 MP4 played at320×180 with no error. Both were muted for testing. FilmCraft
  opened the clip with its real program preview and timeline; its imported dirty project was
  guarded and explicitly discarded without altering the original video.

This verifies the tested formats and flows in Chromium, including390px responsive layout. It
does not establish every codec, native-mobile browser behavior, or lossless Office conversion.

## Reviewed merge

Implementation `246af7770`, accessibility fix `6a9f8a4cb` and evidence `bf5cbc34f` were
fast-forward merged into master. Fresh post-merge Documents/shared-artifact/media/share
regression passed **360tests in45files**,23.68seconds. Full coverage, end-to-end isolation,
whole-change review and published-image deployment remain the final acceptance task.
