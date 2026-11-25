# Tests that never ran in CI: inventory, live run, and wiring

Date: 2026-11-25. Branch `ccr-d742c0b3-jvadt4`, PR #280. Measured in a cloud container (Linux,
Go 1.27.4, Docker 29.9.3, Node 24.22.3) and from the GitHub Actions logs and API of the `master`
CI runs. Spec: [`2026-11-25-test-ledger-design.md`](../specs/2026-11-25-test-ledger-design.md).

## Why

The day began with master red: the Calendar PIM tier failed on `f74a932` because the PIM pin
moved to `8f97396` (writes need `accountId`) and the test's expected surface did not
(`68ddb22`). The operator had found a test broken since September the same day. Both point at
the same gap: nothing checks that a test runs, only that the jobs which run tests pass.

## Inventory, suite by suite

| Suite | Measured | Result |
|---|---|---|
| Go unit tier, 124 packages | local `go test -race -json` with `CI=true` | 115 pass, 9 have no tests, 0 fail; 2 skips, both re-exec helper processes |
| Go tagged tiers | `manual_test_tiers.txt` and the static check | 15 entries unwired: 5 tags, 10 files |
| Go tagged tiers, runtime skips | `t.Skip` calls without a `CI` guard | 16 sites; the tagged jobs run without `-v`, so a skip prints nothing |
| Vitest (`web`) | CI log, run `45642678128` | 426 files, 4,275 tests, 0 skipped |
| Vitest (`packages/create-aura`) | `create-aura-appliance.yml` logs | 12 files, 114 tests; only in that workflow |
| Playwright (`web/e2e`) | CI log, same run; `--list --reporter=json` | 338 tests, **114 skipped**: 86 at collection (80 in 22 `*-live` specs gated on env, 6 visual capture modes) and 28 at run time (desktop-only or mobile-only, by design) |
| Playwright, gating | 40 `master` CI runs, jobs API | the job ran 13 times, was skipped by `paths-filter` 14, 13 undeterminable |
| Calm Prism visual tests | `git log -S` on the workflow | `--update-snapshots` since `75ad77e53` (2026-07-23): they could not fail |
| pytest (`services/ingest`) | CI log | 273 passed, 0 skipped |
| Script self-tests | references in workflows, Makefile, scripts | 2 Python and 3 shell tests in no invocation |
| Telegram live tier | CI logs | `HAS_BOT_TOKEN: false` on every run: the job runs only `go vet` |
| Multimodal audio tier | the live step's conclusion, 40 runs | ran in all 26 runs that reached it, 1 cancelled; it could skip green |

## The unwired entries, run live

On the MUSR stack (`scripts/musr_e2e.sh` steps 1 to 7, the CI job's env), plus GreenMail for the
PIM tiers:

| Entry | First result | Cause | Now |
|---|---|---|---|
| `TestTwoRolesTracer` | PASS | — | wired |
| `TestEnsureForIdentityRestoresAgainstALiveGarage` | PASS | — | wired |
| `internal/objectstore/garageadmin` (3 live tests) | PASS | — | wired |
| `TestForgetDetaches…` (arcadedb-mcp) | PASS | — | wired |
| `TestConversationProjectionLiveCrashRecovery` | PASS | — | wired |
| `TestAdaptiveReasoningTierLive` | PASS | — | wired |
| `TestPipCacheIsolation` (smoke) | PASS | — | wired |
| `pim_mail_e2e` | PASS | case A1 vacuous: no provider here writes a calendar | asserts the refusal, wired |
| `pim_contract_live` | assertions hold | no way to mint the static token it wanted | runs inside `pim_mail_e2e` on its token |
| `TestAssetRoundTripKeepsTheOriginal` | **SKIP, exit 0, `CI=true`** | fixture on `/mnt/d/tmp/baseline-corpus` | synthetic file, provisioned identity |
| `TestServeSmoke` | **FAIL** | strict profile refuses the CI's sample keys; 45 s wait on a dead daemon | dev posture, fails on exit |
| `TestProvisioningSagaResumable` | **FAIL** | raw count hidden by 0087's RLS; then goleak | counts as the identity |
| `TestAssetObjectStoreCrossDenyLive` | **FAIL** | asserted the fallback F-007 removed | asserts fail-closed |
| `TestMemoryCaptureLive_*` (3) | **FAIL, one hung 607 s** | auto-title worker took the scripted tool turn | own title client, bounded waits |
| `TestBackupNetworkPostgresLive` | **FAIL** | host pg_dump 16 against postgres:18 | CI installs client 18 |

After the fixes, `scripts/musr_e2e.sh` with every new block ran green in 94 s.

## Environment workarounds, none in the repo

- No IPv6 in the container: Garage panicked binding `[::]`; measured with a scratch overlay
  binding `0.0.0.0`.
- Builds inside containers do not trust the session's TLS proxy: the published `:edge` sandbox,
  egress and Caddy images stood in for local builds.
- `tempo_blocks_repair_test.sh` refuses root, which this container is; it runs unchanged on the
  CI runner.

## What this does not prove

- That the wiring is green on GitHub Actions. PR #280's CI is the first run of the new steps, of
  the Caddy build and of the PGDG install on a runner.
- That a test asserts something worth asserting: A1 ran, passed, and proved nothing.
- Anything about the 80 `*-live` Playwright tests, which still skip on every run, or whether the
  Authula multi-user test's clean-schema skip fires in CI: no CI job runs `-v`, which is what the
  runtime ledger is for.
- The Calm Prism baselines are master's render on `f74a932`, chosen by the operator; whether that
  UI is the intended one is the operator's judgement, not a measurement.
