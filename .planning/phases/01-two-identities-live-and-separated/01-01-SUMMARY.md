---
phase: 01-two-identities-live-and-separated
plan: 01
subsystem: identity-provisioning
tags: [onboarding-saga, sandbox, cli, arcadedb, garage, docker, musr-isolation]

# Dependency graph
requires:
  - phase: 01-two-identities-live-and-separated (plan 07)
    provides: "the measured Authula TOTP enrollment contract (informational for this plan's saga read, not consumed directly — this plan's saga path stops at Provision, before first login)"
provides:
  - "aura identity create — a CLI verb provisioning a second identity through the SAME StartSession + Provision pair the cockpit wizard uses"
  - "agui.SandboxProvisioner — the eager, idempotent, compensated per-identity sandbox box leg, symmetric with the existing SandboxPurger deprovision leg"
  - "usersandbox.SandboxRouter.EnsureBox — the explicit-identity get-or-create seam the sandbox leg calls, refusing a blank identity rather than falling back to `local`"
  - "TestIdentityCreateProvisionsEveryPlane — a tagged live acceptance test proving all four E2E-02 resources land from one Provision call"
affects: ["01-02 (usersandbox coverage authority also covers router_provision.go)", "01-04/01-05 (data/execution isolation proofs build on a second identity now provisionable end to end)", "01-06 (the phase-closing live run needs a real second identity — this CLI verb is how one gets created outside the cockpit)"]

# Actuals (#2632)
actuals:
  tokens: 16000
  tasks: 3
  commits: 4
  plan_head_before: c86ffcb24a375ea4128f548fe6dcc11e2d765cd9

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Explicit-identity get-or-create seam alongside a context-derived one, sharing one resolve-and-track body (EnsureBox/Route on SandboxRouter) — the get-or-create logic exists once, only identity resolution differs"
    - "Test composition root substitutes ONE port (TelegramMint) while reusing every other production adapter verbatim, so a tagged acceptance test proves the real saga rather than a parallel implementation"
    - "RLS-owner-scoped tables (capability_grants, identity_object_store) require db.WithIdentityTxRaw for a test's own verification SELECTs — a bare pool.QueryRow sees zero rows even when the write succeeded"

key-files:
  created:
    - internal/sandbox/usersandbox/router_provision.go
    - internal/sandbox/usersandbox/router_provision_test.go
    - internal/agui/onboarding_provision_sandbox_test.go
    - cmd/aura/identity_create.go
    - cmd/aura/identity_create_test.go
    - cmd/aura/two_identity_provision_harness_test.go
    - cmd/aura/two_identity_provision_e2e_test.go
  modified:
    - internal/sandbox/usersandbox/router.go
    - internal/agui/onboarding_provision_resources.go
    - internal/agui/onboarding_session.go
    - internal/agui/onboarding_provision_fakes_test.go
    - cmd/aura/serve_provisioning.go
    - cmd/aura/serve_onboarding.go
    - cmd/aura/identity.go

key-decisions:
  - "Local verification of the tagged acceptance test used a DISPOSABLE Postgres container (127.0.0.1:5434, torn down on exit), never the live `aura` database — internal/dbtest's live-target guard (commit 0fa214648) fails-closed on any db_integration DSN named `aura` outside GITHUB_ACTIONS, and this is a repo-wide, pre-existing safety net, not something this plan introduced or worked around. Garage and ArcadeDB stayed the existing shared live services (matching D-13's actual design — only Postgres is disposable); every resource this run provisioned there was torn down by the test's own t.Cleanup and confirmed absent by direct inspection afterward."
  - "The sandbox leg's own-failure handling self-destroys only (mirrors memory/objectStore), not filesystem's full compResources() call — the caller compensates the earlier legs via the returned closure. This matches the plan's explicit action text and Task 2's behavior spec, and is safe because compResources() is idempotent regardless of leg order (it checks each s.X != nil, not whether that leg actually ran)."
  - "identityCreateErrorMessage matches only the two EXPORTED sentinels (ErrOnboardingDuplicate, ErrOnboardingEscalation) with errors.Is; the isolation-disabled sentinel is unexported in agui and its message is passed through unchanged rather than reconstructed or duplicated in cmd/aura."

requirements-completed: []  # ISO-01/E2E-02 are shared with 01-06 (no SUMMARY yet) — the shared-ID gate (#2388) defers marking a multi-plan requirement complete until every declaring plan finishes.

coverage:
  - id: D1
    description: "aura identity create CLI verb provisions a second identity through onboardingService.StartSession + Provision — no raw SQL INSERT, no relaxed validation"
    requirement: "E2E-02"
    verification:
      - kind: unit
        ref: "cmd/aura/identity_create_test.go#TestParseIdentityCreateFlags"
        status: pass
      - kind: unit
        ref: "cmd/aura/identity_create_test.go#TestIdentityCreateErrors"
        status: pass
      - kind: other
        ref: "manual CLI invocation: go run ./cmd/aura identity create (no flags) refuses before prompting; go run ./cmd/aura identity create -bogus names the flag"
        status: pass
    human_judgment: false
  - id: D2
    description: "All four E2E-02 resources (ArcadeDB database+credential, Garage bucket+key, skills root by name, sandbox box) land from one Provision call"
    requirement: "E2E-02"
    verification:
      - kind: integration
        ref: "cmd/aura/two_identity_provision_e2e_test.go#TestIdentityCreateProvisionsEveryPlane (tagged db_integration,garage_integration,authula_integration,musr_e2e; run locally against a disposable Postgres + the live Garage/ArcadeDB/Docker services, 5 subtests, ~5s runtime)"
        status: pass
    human_judgment: false
  - id: D3
    description: "SandboxRouter.EnsureBox fails closed on a blank identity rather than falling back to `local` (T-01-02), and is idempotent"
    requirement: "ISO-01"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/router_provision_test.go#TestEnsureBoxRefusesEmptyIdentity"
        status: pass
      - kind: unit
        ref: "internal/sandbox/usersandbox/router_provision_test.go#TestEnsureBoxIsIdempotent"
        status: pass
    human_judgment: false
  - id: D4
    description: "The sandbox leg's compensation is symmetric with its creation: reverse-order teardown, idempotent, survives a cancelled parent context, and a nil port skips cleanly"
    requirement: "ISO-01"
    verification:
      - kind: unit
        ref: "internal/agui/onboarding_provision_sandbox_test.go#TestProvisionSandboxOwnFailureDestroysNothingElse"
        status: pass
      - kind: unit
        ref: "internal/agui/onboarding_provision_sandbox_test.go#TestProvisionSandboxLaterLegFailureReversesInStrictOrder"
        status: pass
      - kind: unit
        ref: "internal/agui/onboarding_provision_sandbox_test.go#TestProvisionSandboxCompensationIsIdempotent"
        status: pass
      - kind: unit
        ref: "internal/agui/onboarding_provision_sandbox_test.go#TestProvisionSandboxCompensationSurvivesCancelledContext"
        status: pass
      - kind: unit
        ref: "internal/agui/onboarding_provision_sandbox_test.go#TestProvisionSandboxNilPortSkipsLegAndCompensation"
        status: pass
    human_judgment: false

duration: 155min
completed: 2026-09-08
status: complete
---

# Phase 01 Plan 01: Aura Identity Create — Eager Sandbox Leg + CLI Tracer Summary

**`aura identity create` now provisions a second identity through the real onboarding saga, and the per-identity Docker sandbox box lands eagerly at provisioning time (not lazily at first tool call) — proven by a live tagged test against real ArcadeDB, Garage, and Docker.**

## Performance

- **Duration:** ~155 min
- **Started:** 2026-09-08 (session start)
- **Completed:** 2026-09-08
- **Tasks:** 3 completed (Task 1 tracer, Task 2 compensation tests, Task 3 CLI surface)
- **Files modified:** 14 (7 created, 7 modified)

## Accomplishments

- `internal/sandbox/usersandbox/router_provision.go`: `SandboxRouter.EnsureBox(ctx, identityID)` — the explicit-identity get-or-create seam that fails closed on a blank identity rather than falling back to the seeded `local` identity the way `Route`'s context-derived path does (T-01-02). `Route` now delegates to the same shared `resolveAndTrack` body.
- `internal/agui/onboarding_provision_resources.go`: a new `SandboxProvisioner` interface (embeds `SandboxPurger`) and the sandbox leg added as the LAST resource leg in `provisionResourceLegs`, journaled through the already-existing `sagaStepSandbox` constant, with symmetric compensation at the FRONT of the reverse-order `compResources` closure.
- `cmd/aura/identity_create.go`: the CLI verb. Flags parsed via a pure, testable `parseIdentityCreateFlags`; password and security answer read via the existing `readHiddenFromStdin` (reused, never a flag); calls exactly `StartSession` then `Provision` through a small `onboardingCreator` test seam (D-07: no new saga entry point).
- `cmd/aura/two_identity_provision_harness_test.go` + `two_identity_provision_e2e_test.go`: a D-08 fake `TelegramMint` plus a composition helper that builds the REAL onboarding service (every other production adapter unmodified), and `TestIdentityCreateProvisionsEveryPlane`, which drives one `Provision` call and positively asserts all four E2E-02 resources — including a non-empty-root-set check before the skills-directory check, so a blank `AURA_SKILLS_IDENTITY_DIR` would fail loud rather than pass vacuously.
- `internal/agui/onboarding_provision_sandbox_test.go`: six tests proving the sandbox leg's compensation is symmetric — reverse-order teardown (sandbox, filesystem, objectStore, memory), idempotent double-invocation, survives a cancelled parent context, and a nil port skips cleanly.

## Task Commits

Each task was committed atomically (TDD RED/GREEN split where the file was genuinely test-first; see Deviations for where it was not):

1. **Task 1 (RED): failing tests for `SandboxRouter.EnsureBox`** - `e6b9fde64` (test)
2. **Task 1 (GREEN): `SandboxRouter.EnsureBox` + `Route` refactor** - `246e4eff5` (feat)
3. **Task 1 + Task 3: the sandbox provisioning leg, the CLI verb, and the tagged acceptance test** - `133e06bd0` (feat)
4. **Task 2: sandbox compensation symmetry tests** - `a976cce4b` (test)

**Plan metadata:** committed separately below (docs).

## Files Created/Modified

- `internal/sandbox/usersandbox/router_provision.go` - `EnsureBox` + the shared `resolveAndTrack` body
- `internal/sandbox/usersandbox/router_provision_test.go` - 4 unit tests, no Docker daemon required
- `internal/sandbox/usersandbox/router.go` - `Route` refactored to delegate to `resolveAndTrack`
- `internal/agui/onboarding_provision_resources.go` - `SandboxProvisioner` interface + the sandbox leg
- `internal/agui/onboarding_session.go` - `OnboardingDeps.Sandbox` + `onboardingService.sandbox`
- `internal/agui/onboarding_provision_sandbox_test.go` - 6 compensation-symmetry tests
- `internal/agui/onboarding_provision_fakes_test.go` - added `fakeSandboxProvisioner`
- `cmd/aura/serve_provisioning.go` - `sandboxProvisionAdapter` + `sandboxProvisionerFor`
- `cmd/aura/serve_onboarding.go` - wires `deps.Sandbox`
- `cmd/aura/identity.go` - `create` case, returns before the DB-only pool opens
- `cmd/aura/identity_create.go` - the CLI verb, flag parsing, error mapping
- `cmd/aura/identity_create_test.go` - 10 subtests over flag parsing + error mapping
- `cmd/aura/two_identity_provision_harness_test.go` - fake TelegramMint + service composition helper
- `cmd/aura/two_identity_provision_e2e_test.go` - `TestIdentityCreateProvisionsEveryPlane`

## Decisions Made

- **Local verification used a disposable Postgres database, never the live `aura` DB.** `internal/dbtest.MigrateURL` (commit `0fa214648`) refuses to migrate a database literally named `aura` outside `GITHUB_ACTIONS` — a pre-existing, repo-wide safety net (the 2026-07-10 incident it documents), not something this plan added or needed to bypass. A one-off local runner (not committed — this is `scripts/lib/disposable_stack.sh`/`scripts/musr_e2e.sh`'s job, owned by D-12/D-13 in a later plan of this phase) started a throwaway Postgres container on `127.0.0.1:5434`, migrated it, and pointed `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` at a non-`aura`-named database inside it, while Garage/ArcadeDB/Docker stayed the existing shared live services per D-13's actual design. Every resource the test run provisioned on those shared services was torn down by the test's own `t.Cleanup` and confirmed absent afterward by direct inspection (`list databases`, `ListBuckets`, `docker ps`).
- **RLS-scoped verification queries.** `aura.capability_grants` and `aura.identity_object_store` are owner-scoped under RLS (migration 0087, fail-closed): a bare `pool.QueryRow` with no `app.current_identity` bound sees zero rows even though the row exists. The test's own verification queries had to be wrapped in `db.WithIdentityTxRaw`, mirroring the existing `assertRLSCount` pattern in `two_identity_e2e_harness_test.go` — this was found and fixed during the first live run (see Deviations, Rule 1).
- **Sandbox leg's own-failure handling self-destroys only**, matching memory/objectStore rather than filesystem's full-`compResources()` shape, per the plan's explicit action text and Task 2's behavior spec.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Test verification queries against RLS-owner-scoped tables saw zero rows**
- **Found during:** Task 1 (first live run of the tagged acceptance test)
- **Issue:** `TestIdentityCreateProvisionsEveryPlane`'s raw `pool.QueryRow` checks against `aura.capability_grants` and `aura.identity_object_store` reported 0 rows, even though the saga had written them — because both tables are RLS-owner-scoped as of migration 0087 (fail-closed) and the test's connection carried no `app.current_identity`.
- **Fix:** Wrapped both checks in `db.WithIdentityTxRaw(ctx, pool, identityID, ...)`, the same kernel-RLS-backstop pattern `two_identity_e2e_harness_test.go`'s `assertRLSCount` already establishes.
- **Files modified:** cmd/aura/two_identity_provision_e2e_test.go
- **Verification:** Re-ran the tagged test; both subtests passed.
- **Committed in:** 133e06bd0 (part of the Task 1 commit — found and fixed before that commit, not as a separate patch)

**2. [Rule 3 - Blocking] `dbtest.MigrateURL` refuses to migrate the live `aura` database locally**
- **Found during:** Task 1 (first attempt to run the tagged test locally)
- **Issue:** The tagged test's `musrMigratedPool` helper calls `dbtest.MigrateURL`, which fails closed (`t.Fatalf`) on any `AURA_DB_MIGRATE_URL` naming the database `aura` unless `GITHUB_ACTIONS` is set — a pre-existing, deliberate safety net (commit `0fa214648`), not a bug and not something to bypass by faking CI.
- **Fix:** Ran the tagged test locally against a disposable Postgres container (see Decisions above) instead of the live stack's `aura` database. This is exactly the shape D-12 (a later plan in this phase) is scheduled to formalize into `scripts/lib/disposable_stack.sh`/`scripts/musr_e2e.sh`; nothing here duplicates or pre-empts that work — the local verification runner used for this plan was not committed to the repo.
- **Files modified:** none (verification-only)
- **Verification:** The tagged test passed against the disposable database; Garage/ArcadeDB/Docker cleanup confirmed by direct inspection.
- **Committed in:** n/a (verification-only, not a code change)

---

**Total deviations:** 2 (1 auto-fixed bug, 1 auto-resolved blocking issue). **Impact on plan:** Both were necessary to produce genuine evidence the tagged test passes for real; neither touched production behavior beyond the RLS-scoping fix in the test's own verification code.

## TDD Gate Compliance

This plan's three tasks all carry `tdd="true"`, but the tracer's cross-layer coupling (CLI → saga → resource legs → sandbox router, all committed together) made a strict per-file RED-before-GREEN impractical beyond the most isolated unit:

- **`internal/sandbox/usersandbox/router_provision_test.go` (Task 1's `EnsureBox`)**: genuine RED-GREEN. `test(01-01)` commit `e6b9fde64` was written and confirmed to fail (compile error: `EnsureBox undefined`) before `feat(01-01)` commit `246e4eff5` implemented it.
- **`internal/agui/onboarding_provision_sandbox_test.go` (Task 2)** and **`cmd/aura/identity_create_test.go` / the tagged acceptance test (Task 3 + the rest of Task 1)**: these were written and passed against production code that was implemented in the SAME commit or a prior one — not a literal RED-first sequence for those specific files. The behavior each test proves was still design-driven (each new type/port — `SandboxProvisioner`, `EnsureBox`, `onboardingCreator` — exists because a test needed to exercise it in isolation), but the git history does not show a failing commit preceding each of these specific test files. Recorded here per the Fail-Fast Rules' "Missing RED commit" clause rather than presented as compliant.

No test was weakened to make this pass; where an assertion failed on first run (the RLS-scoping issue above), the test's own query was fixed to correctly observe production behavior, and production code was not touched to accommodate a wrong test.

## Issues Encountered

- **RLS visibility on verification queries** — see Deviations #1. Resolved.
- **`dbtest.MigrateURL`'s live-database refusal** — see Deviations #2. Resolved via a disposable local Postgres, not a code change.

## User Setup Required

None - no external service configuration required. (The CLI verb itself requires `AURA_MUSR_ISOLATION=true` under a strict `AURA_PROFILE` and a configured `TELEGRAM_BOT_TOKEN` to succeed against a live deployment — this is documented in the verb's own `-h`-equivalent usage text, not a setup step this plan performs.)

## Next Phase Readiness

- `aura identity create` is a real, tested path an operator (or the phase-closing live-run harness, plan 01-06) can use to provision a second identity outside the cockpit wizard.
- The eager sandbox leg means a freshly created identity's box exists at provisioning time — plan 01-06's forced-first-login harness and any later isolation-proof plan can assume the box is already live rather than waiting for a first tool call to create it.
- `ISO-01`/`E2E-02` are NOT yet marked complete in `REQUIREMENTS.md` — they are shared with plan `01-06`, which has no SUMMARY yet (the shared-ID gate, #2388, defers marking a multi-plan requirement complete until every declaring plan finishes).
- No blockers for the next plan in this phase's wave.

---
*Phase: 01-two-identities-live-and-separated*
*Completed: 2026-09-08*

## Self-Check: PASSED

- FOUND: `internal/sandbox/usersandbox/router_provision.go`
- FOUND: `internal/sandbox/usersandbox/router_provision_test.go`
- FOUND: `internal/agui/onboarding_provision_sandbox_test.go`
- FOUND: `cmd/aura/identity_create.go`
- FOUND: `cmd/aura/identity_create_test.go`
- FOUND: `cmd/aura/two_identity_provision_harness_test.go`
- FOUND: `cmd/aura/two_identity_provision_e2e_test.go`
- FOUND: commit `e6b9fde64`
- FOUND: commit `246e4eff5`
- FOUND: commit `133e06bd0`
- FOUND: commit `a976cce4b`
- Plan-level `<verification>` re-run: `go vet ./... && go build ./...` clean; `go test -race -count=1 ./internal/agui/ ./internal/sandbox/usersandbox/ ./cmd/aura/` green; the tagged acceptance run passed (5 subtests, ~5s runtime, against a disposable Postgres + the live Garage/ArcadeDB/Docker services); `bash scripts/check-file-size.sh` exits 0; `git diff --exit-code -- scripts/coverage_package_policy.json` exits 0 (unchanged).
