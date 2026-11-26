# The test ledger's closing run (2026-11-26)

Plan task C3, last item (`docs/superpowers/plans/2026-11-25-test-ledger.md`). It is the first `master` CI
run with the ledger enforcing and the static check retired: run
[45892030387](https://github.com/chetto1983/Aura/actions/runs/45892030387) on `ce16d43f3`, the merge of
#286 (C3), push of 2026-11-26 20:34 UTC, concluded `success` at 21:52 UTC with 43 checks green. The
`Test ledger` job read 21 artifacts and the results of the 21 jobs it needs, every one `success`, and
exited 0. Measured from CI, not from a container.

## Counts

| suite | declared | executed | skipped | excused |
|---|---|---|---|---|
| go | 10159 | 10115 | 0 | 44 |
| playwright | 68 | 239 | 0 | 100 |
| pytest | 273 | 273 | 0 | 0 |
| python | 18 | 242 | 0 | 0 |
| script | 43 | 43 | 0 | 0 |
| vitest | 438 | 4385 | 0 | 0 |

Go and pytest count tests. Playwright, Vitest, Python and shell self-tests are declared by file, so
`declared` counts files and the other columns count the tests their reports list.

## Violations: 0

Against the first report (`2026-11-26-test-ledger-first-master-report.md`, run 45866028711), which had
153:

| | First report | Closing run | Why |
|---|---|---|---|
| Go declared | 10169 | 10159 | net of the merges between the two runs: #284 deleted the 5 tests of `scripts/musr_live_run_assert_test.go`, #286 the 7 of `cmd/aura/tier_runners*_test.go`, and the merges added others |
| Go skipped | 3 | 0 | closed as the first report's table records: two `internal/mcp` helper processes fixed (`c7be041ac`), the rest excused |
| Go excused | 0 | 44 | the allowlist of C1 (#284) did not exist yet |
| Playwright skipped / excused | 100 / 0 | 0 / 100 | the 100 units, now excused by the allowlist instead of counted as skips |
| Script executed | 42 of 43 | 43 of 43 | `scripts/cloudflared_image_contract_test.sh` wired in `build-and-lint` (`225821603`) |

## The allowlist

77 entries, the file merged in #284 (`.github/test_ledger_allow.txt`):

| suite | paid | outward | measure | platform |
|---|---|---|---|---|
| go | 11 | 2 | 11 | 2 |
| playwright | 15 | 2 | 6 | 28 |

No entry matched an executed test: the gate reports that as a violation, and there were none.

## What this does not show

- **That an excused test runs anywhere.** `paid` and `outward` entries run with a key or against the
  operator's accounts (the nightly `live-outward.yml` for Telegram), and the gate reads neither. Their
  last green run is not part of this verdict.
- **Other workflows.** The gate reads `ci.yml`'s jobs. `skills.yml`, `codeql.yml` and the nightly
  workflows keep their own checks.
- **A pull request run.** This is a `master` push; how the gate judges a PR whose path filters skip jobs
  is not measured here.
