# Work board: use the space and make cards readable

The operator approved the five UI improvements proposed after a Playwright comparison of
the SVAR demo and the authenticated lab VM on 2026-11-01. This is a presentation change
to the existing board, implemented with its installed SVAR 2.6.0 and Aura primitives.

## Measured baseline

Both pages were inspected at 1440 x 960 in dark mode. Aura's board starts at y=253;
its three columns are 280 px wide and end at x=876, leaving about 560 px unused.
The demo starts at y=56 and distributes four 345 px columns across the viewport.
Aura renders priority as a dot and tags as neutral text chips. Its editor is a modal.
Description, due date, card menus, WIP limits, source and conversation/task marks already
exist. The live board has one sparse card, so the number of cards is not a UI defect.

These observations do not prove mobile behavior, persistence after a redesigned edit,
or availability of attachments, assignees and progress in Aura's data model.

## Inventory and approach

- `@svar-ui/react-kanban/types/index.d.ts` exports `RenderConfig.fixedColumnWidth` and
  `Editor` forwards `placement: 'sidebar' | 'modal' | 'inline'` from react-editor.
  Its shipped runtime defaults both column scrolling and fixed widths to true, and
  defaults the editor to sidebar. Use `render={{ fixedColumnWidth: false }}` and
  `placement="sidebar"`; preserve its minimum column width and native scrolling.
- The installed export surface includes Kanban, Editor, ContextMenu, Toolbar, theme
  wrappers, getCardShape/getEditorItems/getPriorityOptions and RestDataProvider. There
  is no exported card body that adds Aura's source and task marks. Retain the existing
  `BoardCardContent`, using Aura's existing `Badge` for visible priority and tag chips.
- Native tag editing selects predefined options. Aura must still create free text tags;
  retain `TagsEditorItem` and its existing comma-separated serialization.
- Retain BoardViewsBar, Select, Button and saved views. Compact their arrangement; do
  not build a new toolbar, provider, editor or responsive JavaScript adapter.

## Intended UI

1. Expanded columns share the available width, including after another column collapses.
   Narrow displays keep horizontally scrolling columns with readable card widths.
2. The title/actions remain in the header. Saved views and labelled filters share one
   wrapping desktop control row. Each filter has a small inline label and a compact
   select. On phones the views/filters remain horizontally reachable without widening
   the document. At 1440 x 960 with defaults the board starts no lower than y=185.
3. Cards show a readable localized priority badge, due date and menu above the title.
   Description and tags remain below; source, conversation and task marks remain visible.
   Tags use stable colors from existing Aura semantic tokens, independent of card order.
   Long titles/tags wrap inside the card. Color supplements the text.
4. The native sidebar editor preserves explicit Save, delete confirmation and Discuss
   in chat. On narrow displays it must fit the viewport and keep actions reachable.
5. Keep existing light/dark themes, self-hosted fonts, translations and keyboard focus.

No schema, API, permission, dependency, progress, attachment, assignee or global shell
changes are required. Runtime telemetry is outside this board-only change.

## Acceptance and validation

- Playwright: three expanded columns consume the desktop board width (right edge within
  24 px), toolbar height meets the bound above, and collapsed columns release space.
- Playwright at 390 x 844: the document does not overflow horizontally; the board scrolls;
  an existing card opens in an editor that fits and whose Save is reachable.
- Existing board tests continue to cover save, filter, views, delete confirmation and
  provider failures. Update the priority assertion to require visible localized text.
- Run board Vitest, typecheck, scoped lint/format, build and the applicable web gates in
  WSL. Verify the built frontend with Playwright against the live board API, including
  an edit/cancel and a disposable add/save/reload/delete scenario. Record exactly which
  frontend was served and which backend was used; the old VM bundle is not new evidence.
- Obtain an independent code review and record evidence in the dated verification file.

Risk: low, board presentation only. No migration or environment variables. Keep changed
files below 600 lines and preserve unrelated working-tree changes.
