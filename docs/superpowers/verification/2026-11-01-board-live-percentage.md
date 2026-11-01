# Live percentage beside the board editor slider — 2026-11-01

## Defect and reuse

The operator's screenshot showed the progress slider without a numeric readout.
Playwright reproduced the installed lab UI in Italian: an unsaved ArrowRight changed
input value 0 to 0.01, while the label remained `Avanzamento`. No board write occurred.

The installed react-editor public items type exposes `labelTemplate(value)`. Its Sections
renderer calls that template with current field data instead of the static label.
BoardWorkspace now uses that native option to show the existing localized progress word
and the rounded percentage. Native slider, 1% step, explicit Save and ratio API are retained.
No new component, mirrored state, dependency, database or backend change was introduced.

## Real browser acceptance

The native Linux production bundle was page-routed as static assets on the authenticated
lab origin. All APIs used the real lab service; no board API was mocked. Each scenario
created one uniquely named card and deleted only its returned ID in cleanup.
The label was checked while the pointer was still held down during dragging.

| Scenario | Italian desktop, 1440×960 | English mobile, 390×844 |
| --- | --- | --- |
| Native pointer drag to 0.35 | `Avanzamento · 35%`, visible | `Progress · 35%`, visible |
| Home / End | visible 0% / 100% | visible 0% / 100% |
| Unsaved 0.15 | visible 15%, zero PUT requests | visible 15%, zero PUT requests |
| Explicit Save | HTTP 200; request and response progress 0.15 | HTTP 200; request and response progress 0.15 |
| Reload and reopen | input 0.15, visible 15% | input 0.15, visible 15% |
| Document width | 1440, no overflow | 390, no overflow |
| Own fixture deletion | successful | successful |

Ignored local screenshots: `.playwright-mcp/board-editor-live-percent-it.png` and
`.playwright-mcp/board-editor-live-percent-en.png`. This proves the new bundle with the
live API, not deployment of that bundle to the VM. Closing the isolated contexts discarded
their local UI preferences; existing operator cards were not edited.

## Checks

- TDD: both localized real-widget tests failed on the absent percentage label before the
  change, then passed. All 43 board tests across five files passed in WSL.
- Scoped V8 board coverage: statements 96.92%, branches 86.86%, functions 97.45%,
  lines 98.23%. This is the board scope, not a new whole-frontend coverage measurement.
- Typecheck, scoped type-aware lint and Prettier passed. Production build passed; the
  existing Shiki C++ 797 kB warning remains unrelated to the board change.
- Manual presentation mutation spot-check replaces only the percent multiplier 100 with
  0, 1 and 101: all three mutants killed by the localized live-label tests; native snapshot
  source restored afterward. This is not whole-file or whole-repository mutation coverage.
- Independent source review reported no actionable findings.
- The existing real-API Playwright regression now recognizes the changing accessible name
  and asserts its 55%, 0% and 100% labels before saving, preserving its persistence checks.

No agent tool or API behavior changed, so the earlier persistence and real-runner evidence
remains applicable. This follow-up measures the editor's visible feedback while editing.
