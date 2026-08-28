
## 51-04 (Task 2/3 — SWARM-07 memory concurrency)

- `TestStageBoxArtifact_ExtractsRegularFile` (`internal/agent/tools/send_file_sandbox_test.go:74`) fails
  pre-existing, unrelated to this plan's changes: asserts staged file mode `0600` but observes
  `-rw-rw-rw-` on this Windows host (a POSIX-permission test running on a filesystem that doesn't
  enforce POSIX bits the same way). Not touched by 51-04's edits to `internal/agent/tools/result.go`
  (delegated-dispatch context helpers only). Out of scope per CLAUDE.md scope boundary.
  **Owner: whichever phase next touches `internal/agent/tools/send_file_sandbox_test.go`** —
  confirm green on a WSL/Linux run before assuming it's still a real regression; no phase currently
  owns sandbox-artifact test hygiene specifically.

- `aura_memory_it` (the fixed database name `internal/arcadedb/memory_integration_test.go`'s
  `integrationClient` expects to already exist on the live server) was absent from the running
  `aura-arcadedb` stack, unrelated to this plan's changes: `EnsureMemorySchema` fails with
  `Database 'aura_memory_it' is not available` before any Fact-related code runs. Created it via
  `POST /api/v1/server {"command":"create database aura_memory_it"}` so this plan's
  `arcadedb_integration` verification run (WriterRole fixture fixes) could actually execute
  against a live server rather than skip. Out of scope to wire permanently (no compose init
  script or CI job currently provisions it).
  **Owner: whichever phase next touches `compose.yaml`'s ArcadeDB init or the CI `db_integration`/
  `arcadedb_integration` job definitions** — add a provisioning step (init script or job-level
  `create database` call) so this stops being a manual, ad hoc fix-up every time the stack is
  rebuilt from scratch.

## 51-06a (Task 2 — SWARM-06 fencing guard rails)

- `TestVerifyOnStopFiresOnARealTurn` (`internal/runner/runner_verification_integration_test.go:233`)
  fails pre-existing, unrelated to this plan's changes: `model ran 2 agent rounds, want 4 — the
  gate did not send the turn back` — the verify-on-stop refusal gate (`VerificationStore`/
  `VerificationDetector`) does not force the model back for a second round on this run. Confirmed
  it fails IDENTICALLY at unmodified HEAD (`730792452`) in a throwaway `git worktree` against its
  own fresh disposable database, with none of 51-06a's migration/query/store/committer changes
  present — this plan touches `aura.paused_states` fencing and the resume-claim path, neither of
  which this test's flow exercises (no ask_user pause, no resume; it is a straight multi-round
  `Turn` with a stop-gate hook). Out of scope per CLAUDE.md scope boundary.
  **Owner: whichever phase next touches the verify-on-stop gate or
  `runner_verification_integration_test.go`** — confirm green on a fresh run before assuming a new
  regression; no phase currently owns this test's health.

- `TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite` and
  `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts` (`internal/agent`, unit tier, no build
  tag) also fail pre-existing, same verify-on-stop/canonical-suite subsystem as the item above.
  Confirmed identical failure at unmodified HEAD (`730792452`) in a fresh throwaway `git worktree`.
  51-06a touches only `internal/askuser`, `internal/runner`'s resume-commit path, and
  `internal/agui/approvals_api.go`'s Source projection — none of which `internal/agent` depends
  on. Out of scope per CLAUDE.md scope boundary. Same owner as the item above.

## 51-06b (Task 1/2/3 — SWARM-06/SC#4 durable delegation resume)

- **Provenance warning on this section.** The first draft of these notes was written by a session
  that died mid-plan (pid 11464, last alive 2026-08-28T13:02Z) with ~2000 uncommitted lines and no
  commit. That draft described `internal/runner/worker_pause_sweep.go`,
  `worker_pause_sweep_test.go`, `worker_pause_sweep_db_test.go` and "12 passing
  `TestExpireWorkerPauses*` tests" — **none of which existed** in the tree, in any ref, worktree
  or stash when the plan was recovered at 16:14Z. Its Windows-vs-WSL observation (a Windows
  git-bash `go test ./...` showing `TestStageBoxArtifact_ExtractsRegularFile` and the
  `internal/agent` verify-on-stop pair failing, all green in WSL) is consistent with the 51-04/51-06a
  entries above and with the standing order that only a WSL result is a verdict, but it was not
  re-measured here and is kept only as that session's claim. Its `internal/runner` `-race` flake
  report (`TestLeftoverSteerAutoDeliversAsNextTurn` "LLM calls = 3, want 2",
  `TestAutoDeliveryChainIsBounded`, one different test on each of three runs, both green in
  isolation) is likewise unverified by this session — one full `db_integration` run of
  `internal/runner` here was green — and stays recorded as a claim, not a measurement. The plan was
  rebuilt from the orphaned tree by the recovering session (commits `2b44d8c17`, `124d3bf51`,
  `0a724ab30`, `5b4969af0`); see 51-06b-SUMMARY.md §Issues Encountered.

- **Measured here (2026-08-28, WSL, disposable `aura_cov` as `aura_app`):** in ONE of three
  full-package `go test -tags db_integration -count=1 ./internal/swarm/` runs,
  `TestNudgeSkipsDrained` (plan 51-10's absent-operator nudge test, untouched by 51-06b) failed
  once; it passed 2/2 in isolation (`-count=2`) and on the full-package re-run. Same intermittent
  class as the runner steer claim above. Not a correctness defect 51-06b introduced (Task 3 touches
  no swarm code; the sqlc regeneration is additive). Out of scope per CLAUDE.md scope boundary.
  **Owner: whichever phase next touches the steer/nudge delivery chain** (`internal/swarm/
  delegation_delivery*.go`, `internal/steer`). What this does NOT prove: whether the swarm and
  runner flakes share a cause — nobody has run them under the same instrumentation.

- **Test names vs plan `<verify>` names.** The plan's `<verify>` blocks name
  `TestWorkerOpensOwnPause`, `TestParkedRowNotClaimable`, `TestDelegationResumeContinuesWorker`,
  `TestUnparkExactlyOnce`, `TestResumeKeepsPromotedTools`, `TestSiblingPauseUnaffected`,
  `TestExpiredWorkerPauseResolvesQueueRow`. The shipped tests assert the same behaviours under
  different names (`TestOpenPauseAndParkAtomicity`, `TestDelegationPauseResumeFullLifecycle`,
  `TestDelegationResumeObserver*`, `TestDelegationResumeState*`, `TestExpireWorkerPauses*`). A
  literal `-run` with the plan's names matches nothing; 51-08's validation must use the shipped
  names or rename. Not renamed here to avoid churn on a green suite.

- **Per-plan gates deliberately deferred to phase close (51-08):** the 85% aggregate coverage gate
  (`scripts/coverage_docker.sh`; unit-only here: runner 84.1%, swarm 85.9%, documents 62.6%), the
  mutation spot-check on `delegation_resume.go` / `worker_pause_sweep.go`, and the live SC#4
  drive on the running stack.
