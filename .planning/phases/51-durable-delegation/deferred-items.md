
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
