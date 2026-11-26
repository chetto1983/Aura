# Every test runs in CI, or is named: the runtime test ledger

- **Date:** 2026-11-25
- **Status:** Draft, for the operator's approval
- **Measurement this rests on:** [`docs/superpowers/verification/2026-11-25-ci-test-inventory.md`](../verification/2026-11-25-ci-test-inventory.md)
- **Supersedes:** `.github/manual_test_tiers.txt` and the static check in `cmd/aura/tier_runners*_test.go`
  (`3e83b15`, `cb5a571`, `35ee3d2`, `442c205`), once the ledger is green on `master` (§8).
- **Operator decisions already taken (2026-11-25):** the 15 `unwired — owner decision pending` entries all
  run in CI; the asset round-trip uses a synthetic file, not a corpus; the static check is retired when the
  ledger works; no test may go unnoticed. For JavaScript that rests on a lint rule as well as on the
  ledger: a test registered only under a condition that was false in CI would never reach a report, so
  registering one under a condition is a lint error (§4.1, operator, 2026-11-26).

## 1. Why

On 2026-11-25 the operator found a test that had been broken since September with nobody noticing. The
measurement of the same day (verification file above) shows it is the shape, not the exception:

- **Tests that exist and never run fail when run.** Of the 13 unwired Go entries run on the live stack,
  `TestServeSmoke`, `TestProvisioningSagaResumable`, `TestAssetObjectStoreCrossDenyLive` and two
  `TestMemoryCaptureLive_*` tests fail, and `TestMemoryCaptureLive_TerminalBarrier` hangs. In every one
  the code moved on (RLS migration 0087, the F-007 fail-closed resolver, the strict boot profile, the
  auto-title worker) and the test, which nothing ran, kept asserting the old world (§6).
- **A skip passes green under `CI=true`.** `TestAssetRoundTripKeepsTheOriginal` read its fixture from
  `/mnt/d/tmp/baseline-corpus` and `t.Skipf`'d without it: exit 0. Sixteen tagged-tier `t.Skip` calls have no
  `CI` guard, and every tagged job runs `go test` without `-v`, so nothing in a log shows a skip.
- **Jobs go green without running their tests.** Web E2E sits behind `dorny/paths-filter`: in the last 40
  `master` CI runs it ran 13 times and skipped 14 (13 unreadable), although it boots `aura serve` and
  exercises the backend a Go-only push changes. The Telegram live job has had `HAS_BOT_TOKEN: false` on every
  run: it runs `go vet` and reports success. Of 338 Playwright tests, 114 skip on every run.
- **The current gate reads text.** `tier_runners_test.go` interprets workflows, make recipes, shell scripts
  and Python drivers to infer which tests run. Its own header names the limit ("it reads text, not control
  flow"), and by construction it cannot see a runtime skip, a path filter, a conditional step or a job
  that exits early. It found the 15 unwired entries; it could not find anything above.

What runs is a fact of the CI run, so it is measured there.

## 2. Goals, non-goals, anti-goals

**Goals**
- Every declared test, in every suite, executes in CI on every push that runs the workflow, and its result
  is recorded.
- A declared test that no job executed, or that skipped in every job that ran it, fails CI unless an
  allowlist entry names it with a reason class. The allowlist is printed in every run's summary, so an excused test is still seen.
- The 15 unwired entries and the other holes in the verification file run, or carry an allowlist entry with
  a reason the operator accepted.
- One mechanism. The static check and `manual_test_tiers.txt` go once the ledger is proven (§8).

**Non-goals**
- Writing tests for code that has none (`internal/approvalgrants`, `cmd/aura-ingest-supervisor`). The
  ledger is about declared tests; coverage is `scripts/coverage_gate.sh`'s job.
- Making paid runs free. A test that spends real model credit or messages a real third party stays
  operator-run; the ledger lists it every run instead of forgetting it.
- Judging assertion strength. A test that runs and asserts nothing is the mutation gates' problem (§9).

**Anti-goals: failures even if the ledger works**
- A ledger that is green because a job was skipped, cancelled, or never uploaded its results.
- An allowlist entry without a reason, or one that excuses a test that now runs.
- A second source of truth kept alongside the ledger.
- A change to what a test asserts made to get it green. Each stale test in §6 is changed to assert the
  current, intended behaviour, with the commit naming the change that moved it.

## 3. Approaches considered

| | Approach | Trade-off | Verdict |
|---|---|---|---|
| A | Extend the static check (follow `if:`, path filters, `exit 0`, Python control flow) | Re-implements a shell and YAML interpreter; still blind to a runtime `t.Skip` and to a job the runner skipped | Rejected |
| B | **Runtime ledger**: every test job uploads machine-readable results; one gate job compares declared tests with executed ones | Needs every test step to write results; a step that does not is reported as not executed, so the rule enforces itself | **Chosen** |
| C | Infer from coverage profiles | Coverage counts lines, not tests; a skipped test leaves no trace | Rejected |

## 4. Design

### 4.1 Units

| Suite | Unit | Declared from | Executed from |
|---|---|---|---|
| Go (root module + `tools/deadexport`) | top-level `TestXxx` per package | source: tracked `*_test.go`, `func TestXxx(*testing.T)`, any build constraint | `go test -json` events, `Action` `pass`/`fail`/`skip` with a `Test` and no `/` |
| Playwright | file + title path + project | every tracked `web/e2e/*.spec.ts` must appear in the report; its tests come from the report, which lists skipped ones too | the JSON reporter, `status` per result |
| Vitest (`web`, `packages/create-aura`) | file + full name | every tracked `*.test.ts(x)` must appear in the report; its tests come from the report (skipped and todo included) | the JSON reporter |
| pytest (`services/ingest`) | node id | `pytest --collect-only -q` | `--junitxml` |
| Script self-tests | file `scripts/**/*_test.{sh,py}` | `git ls-files` glob | shell: the glob runner's result line; Python: unittest's JUnit (§4.4) |

Subtests are not units: a parent that runs counts, and table rows stay the parent's business.

The JavaScript suites are declared by file, not by a listing command: measured on `web`, `vitest list --json`
names 3,744 tests while the CI run executes 4,275, because parameterised and generated tests exist only at
run time. A file that never reaches the report is the violation; inside a file the report lists every test
the file registered, skipped ones included. A test registered only under a condition (`if (flag) test(…)`)
is not registered when the condition is false, so it is in no report. The lint forbids it:
`aura/no-conditional-test-registration` (`web/lint/aura.mjs`, an oxlint JS plugin) rejects `test`, `it` or
`describe` registered under an `if`, a ternary, `&&` or `||` outside a test's own body, in Playwright and
Vitest alike, and oxlint's `vitest/no-conditional-tests` backs it on Vitest. `test.skip(condition, reason)`
registers nothing itself: the tests declared around it stay registered and the report lists them as skipped,
which the ledger sees. Measured before the rule (2026-11-26,
validated on a fixture): 0 of 504 test files under `web/` and `packages/create-aura/` registered a test
under a condition. `web/scripts/lint-contract.mjs` proves both rules still fire.

### 4.2 Recording

- **Go.** One helper, `scripts/go_test_ledger.sh <label> <go test args…>`, runs
  `go test -json "$@" | tee -a "$AURA_TEST_LEDGER_DIR/<label>.jsonl"` into a `jq` that prints the `output`
  and `build-output` events, under `pipefail`. The log reads as `go test -v` does today (the babysit skill
  keeps working), compile errors included: go1.27.4 reports them as `build-output`, not `output` (measured
  2026-11-26), so a filter on `output` alone hid them. The only line missing is the go command's closing
  bare `FAIL`, which has no event; the exit status is `go test`'s. No new dependency: `jq` is on the runners, `gotestsum` is not
  (`Makefile:76` installs it locally only). Every `go test` in `ci.yml`, in the make targets it calls and in
  the scripts those launch goes through the helper. One that does not is not in the ledger and fails it.
  `scripts/agent_memory_eval.py` already collects `go test -json`; it writes the same file.
- **Playwright** adds `['json', { outputFile }]` to the CI reporter list beside `github`.
- **Vitest** adds `--reporter=default --reporter=json --outputFile.json=…`.
- **pytest** adds `--junitxml`.
- Each test job uploads `$AURA_TEST_LEDGER_DIR` as artifact `test-ledger-<job>` with `if: always()`, so a
  red job still reports what ran.

### 4.3 The gate

A `test-ledger` job in `ci.yml`, `needs:` every test job, `if: ${{ !cancelled() }}`, downloads the
`test-ledger-*` artifacts and runs `go run ./cmd/testledger check`. It fails on any of:

1. a declared unit that no job executed (`pass` or `fail`) and no allowlist entry excuses;
2. a unit that no job executed and that skipped, with no allowlist entry: a skip is judged only when no job
   ran the unit, so a test skipped in one job and run in another is executed, not excused;
3. an allowlist entry that matches no declared unit, or matches a unit some job executed (stale);
4. a needed test job whose result is not `success`, or one that uploaded no artifact.

It writes to `$GITHUB_STEP_SUMMARY`: declared / executed / skipped / excused counts per suite, every
allowlist entry with its class and reason, and every violation. The declared-set code reuses what
`cmd/aura/tier_runners_files_test.go` already has (`tierTestFiles`, `testFuncs`, `goBuildConstraint`),
moved into `internal/testledger` so it is tested and inside the coverage floor; `cmd/testledger` is a thin
main.

Units are matched across jobs by union: a test that runs in two jobs is executed if either ran it. A
Playwright unit carries its project, so a desktop-only test is executed in `chrome` and, in `mobile-chrome`,
a separate unit that never runs and needs a `platform` entry.

### 4.4 Script self-tests

`scripts/run_script_tests.sh` runs every tracked `scripts/**/*_test.sh` found by glob, writes one result
line per file, and replaces today's hand-kept lists (`ci.yml:99`, the make `*-contract` recipes). A new
self-test runs because it exists. Tests that need a built image (`cloudflare_caddy_origin_test.sh` needs
`aura-caddy`) get it from the same job.

Python self-tests run through `make evidence-contracts`' `unittest discover -s scripts`, which reaches the
top level and any subdirectory that is a package (has an `__init__.py`; none does today), and report to
the ledger as JUnit. Their declared set is wider on purpose, `scripts/**/*_test.py`: a self-test in a
subdirectory that is not a package is then declared and not executed, a violation, instead of a file
nothing sees.

### 4.5 The allowlist

`.github/test_ledger_allow.txt`, one entry per line:

```
<suite> | <selector> | <class> | <reason>
```

`selector` names a package and test (`internal/llm/openai_compat TestOllamaLiveE2E`), a Playwright file
with optional title and project (`e2e/studio-live.spec.ts`), or a script file. Classes, closed set:

- `paid` — spends real model credit or a metered service;
- `outward` — acts on a real third-party account (a Telegram chat, an OpenRouter management key);
- `measure` — a measurement harness that asserts nothing (`measure`, `retrieval_eval` today);
- `platform` — the unit cannot run on the CI platform by design (a desktop-only test in the mobile project,
  a symlink test where symlinks are unsupported, the 32 GB soak).

There is no "pending" class. A test that should run and does not is a red ledger until it runs.

### 4.6 Path filters

`web-e2e` and `web-test` lose their `paths-filter` guard. Measured cost (run `45642678128`): 767 s and
335 s of runner time, in parallel inside a workflow that already takes about 66 minutes of wall time.
`web-lint` keeps its filter: it runs no tests.

`packages/create-aura`'s 114 Vitest tests run only in `create-aura-appliance.yml`, a separate workflow the
gate cannot see. `web-test` runs them too (seconds); the appliance workflow keeps its own run as the
publish gate.

### 4.7 Skips

A test may skip in a job only when another job executes it, or through an allowlist entry (§4.5,
rules 2 and 3). The 59 local "env var or skip" helpers
(`envOrSkip`, `smokeEnvOrSkip`, `calendarEndpointOrGate`, … defined in 40 packages) fold into one
`internal/testenv.Require(t, keys...)`: skip locally, `t.Fatal` under `$CI`. The 16 unguarded tagged-tier
skips either go through it or become `platform` entries.

## 5. Where each measured hole landed (PR #280)

| Hole | Landing |
|---|---|
| `TestTwoRolesTracer`, `TestEnsureForIdentityRestoresAgainstALiveGarage`, `TestAssetRoundTripKeepsTheOriginal` | `scripts/musr_e2e.sh`'s five-tag `-run` |
| `TestProvisioningSagaResumable`, `TestAssetObjectStoreCrossDenyLive`, `internal/objectstore/garageadmin` | a `garage_integration` block in `scripts/musr_e2e.sh` |
| `TestForgetDetachesOneActorsSourceAfterD10SplitLeavesTheOther` | the `./cmd/arcadedb-mcp/` `-run` in `scripts/musr_e2e.sh` |
| `internal/runner` memory capture and projection | `scripts/musr_e2e.sh`, the stack they were measured on |
| `serve_smoke` | `scripts/musr_e2e.sh`, last: booting `aura serve` retires the legacy `local` identity |
| `TestAdaptiveReasoningTierLive` | the Reasoning tier job, next to `TestReasoningClassifierLive` |
| `backup_live` | the Integration job, with `postgresql-client-18` from PGDG first on PATH |
| `smoke` (`TestPipCacheIsolation`) | the Sandbox docker_integration job, on the image it builds |
| `pim_mail_e2e`, `pim_contract_live` | a new `pim-mail-e2e` job; the contract runs inside `TestPIMMailEndToEnd` on its token, and the `pim_contract_live` tag is gone |
| two Python self-tests | `make evidence-contracts` discovers every `scripts/*_test.py`; CI calls the target |
| `cloudflare_caddy_origin_test.sh` | the build job builds `aura-caddy` from the commit and runs it |
| `scripts/run_{runner,askuser,conversations,identity}_integration.sh` + `run_runner_integration_test.sh` | deleted: WSL helpers hard-coded to `/mnt/d/Aura` |
| Web E2E and Vitest path filters | removed (§4.6); create-aura's Vitest runs in `web-test` |
| Calm Prism `--update-snapshots` | removed; baselines are master's render on the runner (operator, 2026-11-25) |
| multimodal audio skip-as-green | the job fails when a sidecar never comes up |
| seven unguarded tagged-tier skips | fail under `$CI` through each package's existing guard |
| `TestEgress_FQDNAllowlist` | removed with the OpenSandbox FQDN mode no image could run (operator, 2026-11-25); a set `AURA_SANDBOX_EGRESS_ALLOWLIST` fails boot |
| Telegram live tier | nightly in `live-outward.yml`, where a missing secret fails it; `ci.yml` compiles it (operator, 2026-11-26) |
| 27 gated Playwright spec files (§10.2) | 11 wired in Web E2E; 16 stay manual as `paid` or `outward` |

The glob runner of §4.4 was not needed for the Python self-tests (discovery covers them); the
shell self-tests are still listed by hand, and the ledger is what catches one left out.

## 6. Stale tests found by running them (fixed 2026-11-25)

Each now asserts the current intended behaviour; none was a code bug. Each fix was run live on the
stack and landed as its own commit on `ccr-d742c0b3-jvadt4`.

| Test | Why it failed | Fix |
|---|---|---|
| `TestAssetObjectStoreCrossDenyLive` | asserted the shared-bucket fallback the F-007 fix removed | asserts the fail-closed `pgx.ErrNoRows` (`2608dcd`) |
| `TestProvisioningSagaResumable` | raw count hidden by migration 0087's RLS; then goleak on the admin client's keep-alive | counts through `db.WithIdentityTxRaw`; closes idle conns (`5c1be89`) |
| `TestServeSmoke` | strict default profile refuses the CI's sample keys; 45 s wait on a dead daemon | boots `AURA_PROFILE=dev` like `web_e2e_ci.sh`; fails when the process exits (`f45fd37`) |
| `TestAssetRoundTripKeepsTheOriginal` | Windows-host fixture, skip-as-green; the retired `local` identity; a 3-minute wait for processing that moved to the ingest sidecar | synthetic payload, a provisioned identity torn down by the production port, `accepted` + sha256 asserted, no skips (`044af43`) |
| `TestMemoryCaptureLive_*` | the auto-title worker took the scripted tool turn from the shared fake client; unbounded waits hung 10 minutes | `agenttest.TitleClient`, waits bounded by `withinFor` (`954d17a`) |
| `pim_mail_e2e` A1 | vacuous: no provider here can write a calendar | asserts the calendar-write refusal (`c837967`) |

Not a test defect: `backup_live` needs a `pg_dump` matching the server (18), as `docker/aura/Dockerfile`
installs; it passes with one. `pim_contract_live` passes against the GreenMail-backed PIM given a token
from the test issuer; its landing needs that token in CI (§5).

## 7. Rollout

1. Done in PR #280: wire the holes of §5 and fix the stale tests of §6, one commit per landing.
2. The ledger, recording only: the gate reports and does not fail. One push to `master` proves every
   test job uploads.
3. The gate fails on violations. From here a test that stops running turns CI red on the push that did it.
4. Retire the static check (§8).

Plan: [`2026-11-25-test-ledger.md`](../plans/2026-11-25-test-ledger.md).

## 8. Retiring the static check

Once step 3 is green on `master` and a fixture proves the gate fails on a declared-but-unexecuted test and
on an unexcused skip, delete `cmd/aura/tier_runners*_test.go` (about 1,960 lines) and
`.github/manual_test_tiers.txt`, keeping only the declared-set code that moved into `internal/testledger`.

## 9. What this does not prove

- That a test asserts anything worth asserting. `pim_mail_e2e`'s A1 ran, passed, and proved nothing. The
  critical mutation gates (≥70% killed) remain the check on assertion strength, and only on their scopes.
- That a test excused as `paid` or `outward` still works. The ledger keeps it visible, not green.
- Anything about pushes that do not run the workflow (documentation-only changes under `paths-ignore`).
- A JavaScript test whose registration the lint of §4.1 cannot see: one registered in a loop over a
  conditional list (`for (const x of flag ? xs : [])`), or by a helper called under a condition; a
  `test.skip`, `test.fixme` or `test.fail` declaration whose title and body are both variables
  (`test.skip(title, body)`), which has the shape of an annotation; and any test in `packages/create-aura`,
  which no linter runs (0 conditional registrations there, 2026-11-26).
  Go tests are declared from source and have no such gap.

## 10. Open questions for the operator

1. *(answered 2026-11-26)* **Conditionally registered JavaScript tests.** The report stays the inventory
   and a lint rule forbids registering a test under a condition (§4.1). Declaring from source was set
   aside: tests generated in a loop (`for (const width of [1440, 390])`) take names computed at run time.
2. *(answered, see below)* The gated Playwright specs, re-measured the same day from the `--list` JSON
   and each file's guard: 24 `*-live` files and 3 `*-real` files. The first count, 22, held only the files
   skipped at collection (19 `*-live`, 3 `*-real`); four `*-live` files gate inside the test
   (`mcp-cockpit`, `paperless-first-import`, `paperless-isolation`, `paperless-saved-view-delete`) and
   `remote-access-live` has a project of its own. 8 need only the stack and fixtures and are wired in Web
   E2E: graph, artifact origin, skill catalogue, documents ×3, paperless workflow, paperless saved-view
   delete. 16 drive a real agent or model (`paid`; `model-settings-hot-reload` and `native-media` run a
   local model, not a paid one, but still a real one) and 3 need an outward account: `remote-access`
   (Cloudflare) and the two paperless specs that create a second identity, because provisioning resolves
   the bot's name with a live Telegram `getMe` (`internal/agui/onboarding_provision.go`), which no CI
   token can answer.
   *Updated 2026-11-26 (operator):* the two paperless identity specs run in Web E2E against a local
   `getMe` fake and a strict, isolation-on serve; `native-media-live` runs there with a local vision
   model (Qwen2.5-VL-3B, the only candidate at ten turns out of ten); `remote-access-live` stays
   manual, an attended acceptance; the Telegram live tier runs nightly in `live-outward.yml`. That makes
   11 wired, 15 `paid` and 1 `outward` (measurements:
   `docs/superpowers/verification/2026-11-26-test-ledger-open-points.md`).

Answered 2026-11-25: the 15 entries all run in CI; the asset round trip uses a synthetic file; the static
check retires once the ledger works; the Telegram live tier stays manual; the Calm Prism baselines are
master's render; the FQDN egress mode is removed; the paid live Playwright specs stay manual ("il resto
cablalo"). Of the 11 that are not paid, the 8 stack-only specs are wired; the 3 that need an outward account
no CI holds (`remote-access`: Cloudflare; `paperless-first-import` and `paperless-isolation`: a live
Telegram `getMe`) stay manual with the Telegram tier as `outward`, a classification the operator has still
to confirm (§10.2).

Answered 2026-11-26: a test registered under a condition is a lint error (§4.1); the paperless identity
specs run against a `getMe` fake, with no product change; the Telegram live tier runs nightly where a
missing secret fails it; `remote-access-live` stays manual; `native-media-live` runs on Qwen2.5-VL-3B
despite its research-only licence, as a CI fixture (§10.2).
