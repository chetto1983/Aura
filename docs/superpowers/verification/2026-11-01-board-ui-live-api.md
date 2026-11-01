# Work board presentation with the live lab API — 2026-11-01

Frontend: clean source snapshot of `42568d829` plus the board UI diff, built in WSL's
native Linux filesystem. Backend: authenticated lab VM at `https://192.168.101.158`.
Playwright served local built HTML/assets through page-scoped routes; API requests reached
the real VM. This is not a deployment of the new frontend to the VM. Service-worker
registration was suppressed in the preview tab; the operator's other tab was untouched.

## Measurements

- 1440 x 960, dark: board top 176.625 px (before 253.25), three 464 px columns, 12 px
  right gutter (before 564 px). Collapsing one column gives the others 676 px; expanding
  restores three expanded columns.
- 390 x 844: document width 390 px; board scroll boundary 366 px with 744 px of content.
  The native sidebar initially measured x=-61, width=450, clipping the close button and
  labels. The board-scoped CSS fixes it to x=1, width=388; all four buttons fit (x=29
  through 366.78). Repeated on the final rebuilt bundle with its CSS import, no injection.
- Italian/dark and English/light: readable priority text, distinct colored tag chips,
  translated filters. Priority filtering hides the Medium card; All cards restores it.
- Created one disposable card through the column button; edited its long title,
  description and free-text tags `ops, billing, review`; explicit Save issued a successful
  PUT. Reload plus GET returned the exact title/tags. Closing a changed draft did not
  write it. A pointer drag to Doing issued PUT /move and GET confirmed `column=doing`.
  Delete left the card present until confirmation, then removed it successfully.
  The separate progress API probe described below was also removed.

Screenshots under the ignored `.playwright-mcp/`: desktop dark 14-36-21-766Z, mobile
editor 14-37-10-181Z, English/light 14-37-55-052Z (all 2026-11-01).

## Checks and review

- WSL board Vitest: 36/36. Scoped V8 coverage: statements 96.87%, branches 86.45%,
  functions 97.39%, lines 98.20%.
- Typecheck and scoped Oxlint: passed (0 warnings/errors). Prettier and diff check passed.
- Native Linux production build: passed. Existing Shiki C++ language chunk is 797 kB and
  triggers Vite's 500 kB warning; no board chunk triggers it.
- Independent review found the fixed mobile sidebar and a signed-gap test that accepted
  overflow. Both corrected; follow-up review reports no remaining findings.
- The added persistent E2E spec covers desktop geometry and mobile editor bounds. The
  browser-tool run above exercised these criteria on the live API; CI execution is pending.

## Scope extension

The operator subsequently requested progress and API support. A disposable live POST with
`progress:0.55` answered 200, but both response and subsequent GET omitted progress. This
proves the current API silently discards it, not how a future migration behaves.
Native SVAR 2.6.0's getEditorItems uses a progress slider from 0 to 1, step 0.1; its card
computes percent as round(progress*100). The extension is specified separately in
`docs/superpowers/specs/2026-11-01-board-progress-design.md`.
