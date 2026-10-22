# Saved-view navigation deletion gap

Measured 2026-10-22 before implementation, on the running Aura workspace at
`http://127.0.0.1:9080`. The user supplied the pinned-view screenshot and reported
"miss delete view".

The desktop navigation named `Navigazione documenti` contains the synthetic pinned link
`UX study — Cedar archive`; its nearby button count and the whole navigation's button count
are both **zero**. The before capture is
`output/playwright/paperless-comparison/aura-saved-view-delete-before.png`.

`DocumentsNavigation.tsx` renders pinned views as links. `SavedViews.tsx` already provides
deletion in the management page using `deleteView`, identity-scoped query invalidation and
the existing `ConfirmDialog`. This proves the sidebar lacks the requested action; it does
not prove that server deletion is missing or broken.

The correction exposes that same operation beside pinned links, with an accessible name
identifying the view and confirmation before deletion. It must retain the current search
and all documents, show failures without removing the view, and refresh the identity's
saved-view consumers after success. Desktop and mobile drawer checks will use separately
created synthetic views, leaving this reference view intact.
