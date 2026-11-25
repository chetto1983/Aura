# Dead code removal and the production reachability gate

Date: 2026-11-24. Range `ecc3d78..HEAD` on branch `ccr-f5c5d64d-2oo6ah`: 23 removal commits,
one per package, then two gate commits. Measured in a cloud container: Go 1.27.4,
deadcode and golang.org/x/tools v0.48.0, staticcheck v0.8.2, golangci-lint v2.14.0, and a
disposable PostgreSQL 16.15 cluster for the `db_integration` tier. On 2026-11-25
`tools/deadexport` moved to golang.org/x/tools v0.51.0 to take x/mod v0.42.0 (CVE-2026-56864
and CVE-2026-56865 in v0.38.0); its findings on this tree are unchanged.

## Why the existing gate was blind

`make deadcode` ran `deadcode -test`, and printed nothing on `ecc3d78`. Two causes, measured:

- **`-test` makes every test a root.** Run without `-test`, deadcode reported 36 functions;
  29 of them sat in test-support packages (`agenttest`, `steertest`, `dbtest`, `agenteval`).
- **RTA keeps every exported method of a type that reaches an interface.** This one hid more.
  `go/callgraph/rta`'s rule is "all exported methods of any runtime type, since they may be
  called via reflection", and `deadcode -whylive` on an unused sqlc query answered "reachable
  only through reflection". A scratch analyzer counting type-checked references from non-test
  code found 50 exported functions and methods no production code reached. One of them was a
  false positive: `chatgptplan.Service.Callback`, promoted into `browserlogin.Authorizer`
  through the embedding in `browserAuthorizer`.

Build tags and `-generated` added nothing. GOOS did: `retention.ratioFromCapacity` is dead on
linux and live on windows.

## What was removed

Every candidate was read before it went, in three classes:

- **Removed.** Functions superseded by another path (for example `DelegationClaimLoop.Run`,
  replaced by the runtime ticker driving `ProcessOnce`) or with no caller at all.
- **Moved into `_test.go` files.** Same-package fixtures: `cron.Store.InsertRun`,
  `board.Store.Card`, `MemoryExportDestination.Count`, and the two challenge-free approve
  seeds.
- **Kept and named in the allowlist.** Cross-package test seams.

The second pass found two more: `steer.PostgresStore.PushDelegationResult`, and
`DelegationClaimLoop.Wait`, which was kept as a seam. The third pass found nothing new.

| Category | Lines added | Lines removed |
|---|---:|---:|
| Production Go, sqlc excluded | 111 | 917 |
| sqlc queries and generated code | 0 | 177 |
| Tests | 345 | 1,088 |

Four sqlc queries went with the methods that alone ran them: `ReleaseReservedConversationDelete`,
`UpdateHeartbeat`, `ListIdentityLLMKeys` and `ListAlwaysApplySkills`. One env var went too:
`AURA_LOOP_BRANCH_SOFT_FRACTION`, which fed a soft cap nothing read.

Where a deleted test pinned an invariant of code that stays, the test moved to the live path
instead of going:

- the operator-pinned price is now tested on `ResolveModelProfile`;
- the remote-tunnel member invariants run through `Reconcile`;
- the skills ownership near-misses run through `DirWithin(WritableRoot(), dir)`;
- finding F's in-flight claim runs through two `ProcessOnce` passes;
- `archivePreparedReport` gained the direct tests it never had.

The moved tests were checked against a mutant of the code they pin, and each one fails on it.

## Gates run on the final tree

| Check | Result |
|---|---|
| `go build`, linux, windows, darwin | pass |
| `go vet` on the root module's packages | pass |
| `golangci-lint run` | 0 issues |
| `scripts/tagged_tier_compile.sh` | pass |
| `go test -race` on every package | pass |
| `make deadcode` (old gate, deadexport tests, self-test, production gate) | pass |
| `scripts/check-file-size.sh`, `workflow_pin_gate.sh`, `check_ci_go_packages.sh` | pass |

The `db_integration` tier of every package touched also passed against the disposable cluster:
`board`, `conversations`, `cron`, `documents`, `identitykey`, `share`, `skills`, `steer`,
`swarm`, the runner's worker-pause tests and the cmd/aura delegation outbox test.

## Coverage, measured before and after

This is each package's own unit plus `db_integration` tests, with `-coverpkg` limited to that
package, on `ecc3d78` and on HEAD, against the same cluster. Neither tree had a test failure.

| Package | Before | After | Change |
|---|---:|---:|---:|
| agent | 2491/2779, 89.64% | 2471/2758, 89.59% | −0.05 |
| agent/tools | 4120/4579, 89.98% | 4110/4569, 89.95% | −0.03 |
| agui | 7895/8856, 89.15% | 7893/8853, 89.16% | +0.01 |
| askuser | 213/301, 70.76% | 213/287, 74.22% | +3.46 |
| board | 345/379, 91.03% | 337/371, 90.84% | −0.19 |
| config | 298/327, 91.13% | 293/321, 91.28% | +0.15 |
| conversations | 1785/2134, 83.65% | 1773/2120, 83.63% | −0.02 |
| cron | 1082/1211, 89.35% | 1057/1183, 89.35% | 0.00 |
| documents | 870/1176, 73.98% | 870/1160, 75.00% | +1.02 |
| identitykey | 89/99, 89.90% | 74/82, 90.24% | +0.34 |
| llm | 894/980, 91.22% | 867/950, 91.26% | +0.04 |
| readiness | 64/68, 94.12% | 61/65, 93.85% | −0.27 |
| remotetunnel | 794/874, 90.85% | 766/840, 91.19% | +0.34 |
| retention | 566/597, 94.81% | 565/596, 94.80% | −0.01 |
| runner | 2074/2399, 86.45% | 2074/2395, 86.60% | +0.15 |
| semindex | 110/115, 95.65% | 84/86, 97.67% | +2.02 |
| share | 351/409, 85.82% | 333/385, 86.49% | +0.67 |
| skills | 1123/1238, 90.71% | 1117/1232, 90.67% | −0.04 |
| steer | 203/242, 83.88% | 186/223, 83.41% | −0.47 |
| swarm | 871/1006, 86.58% | 843/976, 86.37% | −0.21 |

`agent/prompt`, `examplebank` and `openrouterprovision` did not change.

`objectstore/garageadmin` is a baseline package, and its exact denominator fails closed.
`DenyBucketKey` was 6 statements, 4 of them covered, by its own unit test only. The package's
own tests went from 126/159 to 122/153. The pin in `scripts/coverage_package_policy.json`
moves from 129/159 to 125/153.

## What this does not show

- **The release coverage gate.** `scripts/coverage_gate.sh` needs ArcadeDB and the embed
  sidecar, and this container had no Docker daemon. The per-package numbers above count only
  each package's own tests, so they sit below what the gate measures with cross-package
  contribution. The changes are small, but a target package that was within 0.5 points of 85%
  in the gate's own measurement could now be under it. Only CI shows that.
- **The arcadedb live profile.** It owns arcadedb's coverage, and it did not run: the tagged
  tests were compiled and vetted only.
- **No E2E and no lab VM.** This range changes no behaviour a scenario exercises. That rests on
  the removed code having no production caller, which is what the analysis shows, not on a run.
- **Reflection.** deadexport sees type-checked references only. A method reached only through
  reflection (a template, `MethodByName`) would read as dead. None was found in this tree,
  which has no Go templates.

## Open gaps the gate now names

Two production functions were built and never scheduled. They stay, listed under UNWIRED in
`scripts/deadcode_production_allowlist.txt`:

- **`telegram.Store.CleanupExpired`.** Nothing removes expired onboarding tokens, so
  `aura.telegram_setup_pending` grows by one row per abandoned setup.
- **`idempotency.Store.ExpireReplayBodies`.** No sweep clears replay payloads, so they outlive
  `ReplayRetention`. Readers ignore expired ones, but the bytes stay in
  `aura.idempotency_operations`.

`aura.skill_catalog.always_apply` is still written, and `skill_catalog_always_apply_idx`
(migration 0118) now indexes a column no query filters on. Dropping both is a migration.
