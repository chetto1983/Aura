# The runtime test ledger: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CI fails when a declared test did not run or skipped without a named reason, from what the CI run actually executed, and the static workflow-text check retires.

**Architecture:** Three parts, each shippable on its own:
- **Part A** records: every test step writes machine-readable results to `$AURA_TEST_LEDGER_DIR` and uploads them as `test-ledger-<job>`.
- **Part B** judges: `cmd/testledger check` compares the tests declared in the tree with the results, against `.github/test_ledger_allow.txt`; a `test-ledger` job runs it, report-only first.
- **Part C** enforces and retires: the gate fails on violations, then `cmd/aura/tier_runners*_test.go` and `.github/manual_test_tiers.txt` go.

**Tech Stack:** Go 1.27.4 (`go/parser`, `go/build/constraint`, `encoding/json`, `encoding/xml`), bash + jq, GitHub Actions artifacts, Playwright JSON reporter, Vitest JSON reporter, pytest `--junitxml`.

**Spec:** [docs/superpowers/specs/2026-11-25-test-ledger-design.md](../specs/2026-11-25-test-ledger-design.md). Measurement: [docs/superpowers/verification/2026-11-25-ci-test-inventory.md](../verification/2026-11-25-ci-test-inventory.md). PR #280 (the wiring this plan builds on) must be merged first.

## Global Constraints

- No file over 600 lines. Refactor on touch; dead code goes in the same commit.
- Go gates after each Go edit: `go vet ./...`, `go build ./...`, `go test -race ./<touched>/`; golangci-lint v2.14.0 built with go1.27.4.
- `internal/testledger` is inside the coverage floor (85%, `scripts/coverage_package_policy.json` is fail-closed: register the package). Mutation ≥70% on `check.go`.
- No new dependency: `jq` is on the runners; `gotestsum` is not in CI and stays out.
- Every `go test` CI runs goes through `scripts/go_test_ledger.sh`. One that does not is "not executed" by construction; that is the point, not a bug to route around.
- The log of a test step must read as it does today (`go test -v` output): the babysit skill reads it.
- A test changes only when it is wrong, with the reason in the commit body.
- Develop on the session branch, open a PR, drive it to green (steward skill). Merge only on the operator's word.

## Review Focus

- That the declared Go set matches what `go test` would compile for some tag set: a file under `//go:build a && !b` is declared once, with the constraint, and counts as executed by a run whose tags satisfy it.
- That a job skipped, cancelled or failed before uploading is a violation, not silence (`if: always()` on every upload; the gate `needs:` every test job).
- That the allowlist cannot excuse a test that runs (stale entry) or a test that does not exist.

## Decisions taken while writing this plan

- **Go is declared from source, pytest by node id, the JavaScript suites by file.** `pytest --collect-only -q` expands parameterised tests at collection, so its node ids are the declared set and a collected id missing from the JUnit report is a violation. `vitest list --json` does not: it names 3,744 tests where a run executes 4,275, so for Vitest and Playwright a file absent from the report is the violation and the report is the inventory inside a file (a test registered under a condition, which would reach no report, is a lint error: spec §4.1).
- **The declared-set code moves, it is not rewritten.** `tierTestFiles`, `testFuncs`, `goBuildConstraint` and `gatingTags` leave `cmd/aura/tier_runners_files_test.go` for `internal/testledger`; the rest of the static check (about 1,800 lines interpreting workflow text) is deleted in Part C, not ported.
- **Subtests are not units.** A parent that ran counts; table rows are the parent's business.
- **Report-only first.** The first `master` run produces the real violation list; the allowlist is written from it, not guessed.
- **The 59 local env-or-skip helpers are not folded here.** With the ledger, an unguarded skip is a visible violation whichever helper produced it; folding them into one `internal/testenv` is its own change.

## File structure

```
internal/testledger/
  declared.go         Go tests declared in the tree, with their build constraint
  declared_test.go
  results.go          readers: go test -json, Playwright JSON, Vitest JSON, JUnit XML, script lines
  results_test.go     fixtures cut from real outputs of 2026-11-25
  allow.go            .github/test_ledger_allow.txt parser (suite | selector | class | reason)
  check.go            declared ∖ executed ∖ allowed, skips, stale entries, missing jobs
  check_test.go
  testdata/
cmd/testledger/main.go  `testledger check --artifacts DIR --allow FILE [--report-only]`
scripts/go_test_ledger.sh       go test -json | tee <label>.jsonl | jq output; exit = go test's
scripts/go_test_ledger_test.sh
.github/test_ledger_allow.txt
.github/workflows/ci.yml        recording on every test step, uploads, the test-ledger job
```

## Part A: record

### Task A1: The Go recording helper

**Files:** `scripts/go_test_ledger.sh`, `scripts/go_test_ledger_test.sh`, `Makefile` (`evidence-contracts` runs the self-test)

- [x] Write the self-test first: a passing, a failing and a skipping fixture package under `scripts/testdata/go_test_ledger/`; assert the jsonl holds `pass`, `fail`, `skip` events, the printed log equals `go test -v`'s, and the exit status is `go test`'s (non-zero on the failing fixture).
- [x] Implement: `set -o pipefail; go test -json "$@" | tee "$AURA_TEST_LEDGER_DIR/$label.jsonl" | jq -rj 'select(.Action=="output").Output'`; refuse an unset `AURA_TEST_LEDGER_DIR` under `$CI`.
- [x] `bash scripts/go_test_ledger_test.sh` passes.

*Done 2026-11-26, with four changes from what was measured.* (1) go1.27.4 reports a compile error as
`build-output` events, not `output` ones, so the filter above dropped it from the log: the helper prints
both, and a fourth fixture that does not compile holds it there. With `-v` the same error goes to stderr,
so the log equals `-v`'s stderr then stdout, less the go command's closing bare `FAIL`, which has no event
(the exit status carries it). (2) `tee -a`: a label used twice in a job appends instead of erasing the
first run. (3) Outside `$CI` an unset `AURA_TEST_LEDGER_DIR` runs plain `go test`, so local `make` output
stays as it is. (4) `go test` takes `-C` only as its first flag (`tools/deadexport` runs that way in CI),
so the helper keeps `-C dir` ahead of `-json`. Seven mutants of the helper (each change above undone, no
`pipefail`, `-json` outside CI, any label) are each killed by the self-test.

### Task A2: Every CI go test goes through it

**Files:** `.github/workflows/ci.yml`, `.github/workflows/skills.yml`, `Makefile`, the CI-launched `scripts/*.sh` (`musr_e2e.sh`, `mcp_live_integration.sh`, `coverage_gate.sh`, `docker_coverage_gate.sh`, `ingest_*_e2e.sh`, `pim_mail_e2e.sh`, `microcompact_smoke.sh`), `scripts/agent_memory_eval.py`

- [x] Inventory first: `git grep -nE 'go test' -- .github Makefile scripts` and list each invocation CI reaches; the list goes in the commit body.
- [x] Replace each with the helper; `agent_memory_eval.py` writes the `go test -json` it already collects into the ledger dir.
- [x] Each test job sets `AURA_TEST_LEDGER_DIR` and uploads it as `test-ledger-${{ github.job }}` with `if: always()`.
- [ ] Run locally the scripts that can run here (`musr_e2e.sh`, `pim_mail_e2e.sh`) and check their jsonl.

*Done 2026-11-26.* The inventory differed from the file list above: `microcompact_smoke.sh` and
`ingest_media_e2e.sh` are launched by no workflow, `capability_eval.py` collects `go test -json` too and
writes it as `agent_memory_eval.py` does (both through `scripts/go_test_ledger.py`), and `live-outward.yml`
runs one. `scripts/tagged_tier_compile.sh` stays as it is: its `-run '^$'` compiles and executes no test.
The Makefile's own `go test` recipes are reached by no workflow and stay plain. `AURA_TEST_LEDGER_DIR` is
set once per workflow (`/tmp/aura-test-ledger`), and every job that runs a test ends with the upload
(`if-no-files-found: error`): 17 in `ci.yml`, one each in `skills.yml` and `live-outward.yml`. Measured
on the unit tier (124 packages, 15,583 passing tests), the raw stream is 18 MB and 1.3 MB compressed, so
the file keeps every event. The static check in `cmd/aura/tier_runners*_test.go` reads `go test` from
workflow text, so until Part C retires it, it reads `go_test_ledger.sh LABEL ARGS` as `go test ARGS`
(one fixture case). Run here: the no-CGO unit step (3,741 tests recorded) and `pim_mail_e2e.sh` live
(`TestPIMMailEndToEnd` recorded). `musr_e2e.sh` was not run here: CI runs it on the PR.

### Task A3: JavaScript and Python suites

**Files:** `web/playwright.config.ts`, `web/package.json`, `packages/create-aura/package.json`, `Makefile` (`ingest-test`), `.github/workflows/ci.yml`

- [ ] Playwright: `['json', { outputFile }]` beside `github` when `AURA_TEST_LEDGER_DIR` is set.
- [ ] Vitest (both packages): `--reporter=default --reporter=json --outputFile.json=…`.
- [ ] pytest: `--collect-only -q` into the ledger dir (the declared node ids), then the run with `--junitxml`.
- [ ] One local run of each, report files inspected.

### Task A4: The shell self-test runner

**Files:** `scripts/run_script_tests.sh`, its self-test, `.github/workflows/ci.yml`, `Makefile`

- [ ] Discover every tracked `scripts/**/*_test.sh` (`git ls-files`), run each, and write one result line per file (`<path> pass|fail|skip <seconds>`) into the ledger dir; a file that exits 77 is a skip.
- [ ] Replace the hand-kept lists it covers: the `security_scan` loop and the installer/updater list in `ci.yml`, the shell lines of `make evidence-contracts`, the `*-contract` recipes. A self-test that needs a prerequisite only another job has (a built image, a non-root user, `makeself`) runs there through the same runner, with a path filter argument, so each file still writes exactly one line somewhere.
- [ ] Python self-tests stay with `make evidence-contracts`' discovery; their unittest results go to the ledger as JUnit. Discovery reaches top-level `scripts/*_test.py` and subdirectories that are packages (an `__init__.py`; none today); the declared set is `scripts/**/*_test.py` (spec §4.4), so a self-test in a non-package subdirectory fails the gate as declared and not executed rather than going unseen.

## Part B: judge

### Task B1: Declared tests

**Files:** `internal/testledger/declared.go` + test; `cmd/aura/tier_runners_files_test.go` (the moved functions go)

- [ ] Move the four functions with their tests; `cmd/aura`'s static check imports them until Part C.
- [ ] Unit tests: constraint parsing (`a && !b`, `a || b`), `func TestXxx(*testing.T)` only (not helpers, not `Benchmark`/`Fuzz`), files ignored by the go tool.

### Task B2: Result readers

**Files:** `internal/testledger/results.go` + test, `testdata/` cut from the 2026-11-25 logs

- [ ] One reader per format, each returning units `{suite, id, status}`; a top-level Go test is `pkg TestName`, a subtest (`/` in the name) is ignored.
- [ ] Union across files: executed if any file has `pass` or `fail` for the unit.

### Task B3: The allowlist and the check

**Files:** `internal/testledger/allow.go`, `check.go` + tests; `.github/test_ledger_allow.txt` (empty at first)

- [ ] Violations, each with a test: declared-not-executed; unexcused skip of a unit no job executed; stale entry (matches nothing, or matches a unit some job executed); a collected pytest node id absent from the JUnit report; a Vitest or Playwright test file absent from its report; a shell self-test with no result line; a needed job with no artifact.
- [ ] Classes are exactly `paid`, `outward`, `measure`, `platform`; any other is a parse error.
- [ ] Mutation ≥70% on `check.go` (`go-mutesting`), recorded in the commit body.

### Task B4: The gate job, report-only

**Files:** `cmd/testledger/main.go`, `.github/workflows/ci.yml`

- [ ] `test-ledger` job: `needs:` every test job, `if: ${{ !cancelled() }}`, downloads `test-ledger-*`, runs `go run ./cmd/testledger check --report-only`, writes the counts, every allowlist entry and every violation to `$GITHUB_STEP_SUMMARY`.
- [ ] Merge; read the first `master` summary; write the dated verification file with the violation list.

## Part C: enforce and retire

### Task C1: The allowlist from the measured list

- [ ] Each violation from B4 either gets fixed (wire it, or fix the test with the reason) or an entry with a class the operator accepts. Expected from 2026-11-25: the paid and measurement tags, the Telegram live tier (`outward`), the desktop-only/mobile-only Playwright tests (`platform`), the 32 GB soak (`platform`), the 16 manual Playwright specs of spec §10.2 (15 `paid`, `remote-access-live` `outward`), and the Telegram live tier, which runs only in the nightly `live-outward.yml` (`outward`).

### Task C2: Enforce

- [ ] Drop `--report-only`. A fixture run in the self-test proves the gate fails on a declared-but-unexecuted test and on an unexcused skip.
- [ ] CI green on the PR; merge on the operator's word.

### Task C3: Retire the static check

- [ ] Delete `cmd/aura/tier_runners*_test.go` and `.github/manual_test_tiers.txt`; update the references (`git grep -n 'manual_test_tiers\|tier_runners'`), the babysit skill's table included.
- [ ] Verification file of the closing run.
