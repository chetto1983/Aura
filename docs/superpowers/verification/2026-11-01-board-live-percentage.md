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

## Browser-regression follow-up

The first CI on `cd8cfa02e` passed the frontend unit/static jobs but the progress browser
cases exceeded their 60-second budget. Their in-page finally cleanup reported a closed
page and obscured the original pending operation. No percentage-value mismatch was shown.

The exact test was then run with Chromium Linux against the new production assets and
real lab API, reusing the authenticated browser session privately: both projects passed
without retries (25.5 s), and again with CPU throttling rate 6 (52.5 s). With explicit
loading guards and bounded response waits, the old 55-key input setup exhausted its
15-second polling budget at 0.43 and 0.50 under the same throttle. This demonstrates a
timing-dependent test setup, not a reason to change the expected 0.55.

The test now clicks the native track near 55% and uses keyboard input only for any neighboring
step. It waits for the initial title and previous saved ratio before editing, since the
native editor initially mounts empty field data before its effect copies the selected card.
All label, API, reload, zero and completion assertions remain. The corrected exact desktop
and mobile tests passed at the first attempt under rate-6 throttling, 24.4 s total.

Cleanup now runs in afterEach through an independent authenticated API request context,
targeting only the ID returned by the test's own POST. It captures the existing shared
browserCookieHeader helper before the page closes. In an intentional forced-page-close
run, the original witness error remained visible and a subsequent independent DELETE
returned 404, proving the production test teardown had already removed its card.

A fake-cookie HTTP-loopback smoke returned 204 both with the current request context's
default cookie handling and with the captured header after closing the browser context.
The old helper comment's blanket claim about Secure cookies was therefore narrowed to a
version-dependent precaution. Current CI uses TLS (`scripts/web_e2e_ci.sh`); this smoke
checks the supported local HTTP configuration, not a different CI origin.

These changes fix demonstrated input-setup slowness, premature field-readiness checks and
failure masking. They do not establish the sole cause of the original CI's 60-second wait.
Typecheck/lint/format and independent review cover this test-only follow-up. No production
bundle, API behavior or expected persistence value was changed by it.

## Publication

Source `dd4b4d3cf`, embedded frontend `cd8cfa02e` and test stabilization `532143991` were
pushed to GitHub and GitLab through the prescribed WSL gates. [CI run 41341930827](https://github.com/chetto1983/Aura/actions/runs/41341930827)
passed all 29 jobs on `532143991`. All other triggered workflows also succeeded.
The final Playwright job reported 224 first-attempt passes in 6.1 minutes, no flaky cases,
and 112 conditionally skipped cases. Its API progress assertions were retained throughout.

The Linux reproductions retained the auth helper's existing completed-profile/readiness
fixtures; the board API was real. Private session snapshots, their dedicated auth cache,
temporary reproduction files and traces were removed after verification. No background
test services remain. This session did not deploy a VM update. A final read-only check of
the running VM did not reach a visible card editor within its 10-second probe and made no
board writes; it is not counted as evidence of deployment or as a successful UI acceptance.
