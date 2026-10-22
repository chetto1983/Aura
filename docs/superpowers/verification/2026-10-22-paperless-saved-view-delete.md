# Saved-view deletion from Documents navigation — 2026-10-22

## Measured gap and implementation

The user reported a missing delete action beside the pinned saved view in the Documents
sidebar. The controller reproduced the missing action on the real stack and committed the
measured PRD/spec/plan amendment as `ec4cf56e2` before these production changes. See
`2026-10-22-saved-view-delete-gap.md` for that initial measurement.

Each pinned view now has a visible 44-by-44-pixel trash button with the accessible name
`Delete: <view name>` (localized through the existing Delete label). The button appears in
both the desktop sidebar and the mobile navigation drawer. Confirmation names the saved
view and defaults focus to Cancel. A failed delete leaves the view in place, displays the
server error inside the confirmation, and offers an enabled Retry action.

The shared `DeleteSavedViewButton` in `SavedViews.tsx` replaces the management page's previous
delete JSX and reuses its existing `useViewWrite`, `deleteView` and `ConfirmDialog`. Successful
deletion invalidates only `['documents-views', identity]`, so every consumer of those views
updates. No backend route, deletion protocol or library-search navigation was added. The
existing management page's visible Delete label remains unchanged.

Keyboard verification also exposed two existing composition gaps. Confirmation was opened
without a registered Radix trigger, so Cancel could not restore focus. The shared dialog now
accepts an optional trigger and composes the existing `DialogTrigger asChild`; its previous
callers retain their existing behavior. This uses Radix's documented trigger/focus behavior:
[Dialog documentation](https://www.radix-ui.com/primitives/docs/components/dialog).
On mobile, the outer drawer also handled an Escape already consumed by the inner modal.
`Drawer` now ignores `defaultPrevented` keyboard events. The controller recorded these live
measurements before implementation in amendments `ea6e8ea0f` and `4d3ceb1c0` respectively.

## TDD

```sh
cd web
npx vitest run src/documents/DocumentsNavigation.test.tsx --maxWorkers=2
```

RED: **3/3 failed** because `Delete: Cedar contracts` was absent from the navigation.

After implementation:

```sh
npx vitest run src/documents/DocumentsNavigation.test.tsx \
  src/documents/SavedViews.test.tsx src/documents/DocumentsSurface.test.tsx \
  src/documents/useDocumentWorkspace.test.tsx --maxWorkers=2
```

GREEN: **4 files / 13 tests passed**, exit 0. The new tests exercise the real React tree,
query client and existing API wrapper, stubbing only HTTP responses. They prove cancellation
sends zero writes; confirmation sends only the selected view's DELETE; the sibling view and
foreign identity cache remain unchanged; current URL/search and document rendering survive;
the management page sees the refreshed remaining view; and a refused delete preserves both
views, exposes its error inside the modal and succeeds on Retry. The original Saved Views
CRUD test passes without changing its assertions.

The first production build caught a test-only typing mistake: React Testing Library's role
query does not accept Playwright's `exact` option. Removing that unsupported option preserved
the same exact string accessible-name assertion. Lint also required explicit `String(index)`
in a new test fixture's template literal. Both corrections preceded the successful final checks.

## Live verification

The final live run used the controller's rebuilt `4d3ceb1c0+dirty` Linux binary with the
frozen production assets on the real stack at `http://127.0.0.1:9080`:

```sh
cd web
AURA_E2E_ORIGIN=http://127.0.0.1:9080 AURA_E2E_LIVE_DOCUMENTS=1 \
  npx playwright test e2e/paperless-saved-view-delete-live.spec.ts --project=chrome \
  --workers=1 --output=../output/playwright/paperless-final-sidebar
```

**2/2 passed**, exit 0, **6.4 seconds** (1440 px: 2.9 s; 390 px: 2.7 s). Each case created
two uniquely named pinned views through the real API, then used the actual navigation Delete
control. It measured a visible 44×44 target, a confirmation identifying that view, safe Cancel
focus, Tab to Confirm and back to Cancel, return focus after Cancel and Escape, and zero DELETE
requests for either cancellation. On mobile the inner Escape left the drawer open and usable.
Confirm then returned HTTP 200 for exactly the selected saved-view ID; its sibling remained
visible, the current URL/search stayed unchanged, and the document total stayed unchanged.
Every original saved view, including the user's reference view, was checked unchanged.
A subsequent ordinary outer Escape still closed the mobile drawer.

All synthetic views still present were removed in `finally` by exact ID, including after
failed exploratory runs. No original view or document was a cleanup target. No login bypass,
new session issuance or operator logout was used. The existing authenticated operator session
and normal same-origin API were used.

The explicit screenshots `pinned-view-delete-action.png`, `named-delete-confirmation.png` and
`sidebar-after-delete.png` remain in the two test directories under
`output/playwright/paperless-final-sidebar/`. The desktop action and mobile modal images were
opened for inspection. The mobile modal image catches its CSS fade transition; the keyboard
and deletion assertions exercise the fully functioning dialog. F1's screenshot/JSON directory
was preserved separately at `output/playwright/paperless-final-fix/`.

The initial live checks and focused unit tests genuinely failed on the two keyboard defects
before each production fix: Cancel did not restore the trigger at either viewport, and the
390 px drawer disappeared along with the inner modal on Escape. Assertions were retained.
The new shared-trigger regression failed while its three existing default-dialog tests passed;
the nested drawer regression failed while its nine existing shell tests passed.

After both fixes, the focused command covering navigation, Saved Views, Documents Surface,
ingestion, shared ConfirmDialog and shell utilities passed **6 files / 27 tests**, exit 0,
6.85 seconds. It includes the existing standalone drawer Escape and Tab behavior plus a real
nested confirmation proving inner Escape leaves the drawer usable, returns focus to Delete,
and a subsequent outer Escape still dismisses the drawer.

## Final quality gates

The final consolidated run after the F1 polling fix, sidebar deletion and both keyboard fixes:

```sh
cd web
npm test -- --maxWorkers=2
```

**381 files / 3,274 tests passed**, exit 0, **398.20 seconds**. No coverage exclusion or
threshold was changed; all four exact 85% floors passed.

| Metric | Exact result |
|---|---|
| Statements | 13,730/14,883 = 92.25% |
| Branches | 10,606/12,223 = 86.77% |
| Functions | 4,025/4,424 = 90.98% |
| Lines | 12,384/13,146 = 94.20% |

The earlier F1-only run is recorded separately in
`2026-10-22-paperless-documents-first-import.md`.

`npm run lint`, `npm run typecheck`, `npm run format:check`, `npm run dup`, `npm run deadcode`
and `npm run build` passed. Duplication reported zero clones; all touched source files are
below 600 LOC. Vite retained its existing advisory for chunks larger than 500 kB. The earlier
consolidated coverage attempt was explicitly stopped as incomplete when the live focus defect
required a source fix; it is not passing evidence. The final run started only after both
viewport checks passed and production source/assets were frozen.

The source, tests and embedded assets were committed as `564aa3bc4` with normal pre-commit
hooks passing (file-size and zero-clone duplication). The controller's fix-scoped review
approved that commit without findings. This evidence update changes documentation only.

The completed base global Stryker measurement was **81.22%**, above the unchanged 70% break
threshold, for its configured source scope. None of this fix's production files are in that
configured mutation scope. The shared dialog support affects callers used by that suite,
so the controller will run the default incremental Stryker command with its untouched warm
cache after final coverage and the source commit. The base number is retained evidence,
not the fresh final incremental result.
