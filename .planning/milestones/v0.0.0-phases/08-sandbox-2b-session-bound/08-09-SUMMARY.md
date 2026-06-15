---
phase: 08-sandbox-2b-session-bound
plan: 09
subsystem: testing
tags: [sandbox, integration-test, security, ci, gvisor, egress-proxy, mutation-testing, coverage]

# Dependency graph
requires:
  - phase: 08-05
    provides: SessionManager (per-conv container lifecycle, TTL reaper, boot recovery) + WorkspaceManager os.Root cascade
  - phase: 08-06
    provides: host-side CONNECT egress proxy (deny-wins glob + resolve-then-pin via internal/web export)
  - phase: 08-07
    provides: sidecar /session/{id}/exec endpoints + RunPythonSession/RunShellSession
  - phase: 08-08
    provides: composition-root wiring (egress bridge + proxy env + connect-allowing session seccomp) + Conversations.Delete cascade
  - phase: 05
    provides: 05-SECURITY.md / AR-05-01 + the sandbox.yml 2a gating DinD job + docker_integration_test.go scaffolding
provides:
  - "Live sandbox_integration && db_integration tier proving the 4 ROADMAP CAP-02 criteria (authored + compile-green; live PASS is the Task-3 Gate-3 sign-off)"
  - "0008_sandbox_sessions migration round-trip test (table + index + uuid FK + CHECK + ON DELETE CASCADE)"
  - "08-SECURITY.md — the consolidated T-08-* register (40 threats) extending 05-SECURITY/AR-05-01 for the 2b connect-allowed egress posture"
  - "sandbox-2b-session-gate gating CI job (session tiers + live egress + mutation + 85% coverage fold)"
  - "aura-quality-snapshot.md Phase-8 2b rows"
affects: [08-verify-work, 08-code-review, 08-complete-milestone, phase-11-skills-7e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Live session tier driving a real per-conv container via a live SessionManager (real dockerCLI + real sqlc store) + the sidecar exec path, gated behind BOTH sandbox_integration && db_integration tags"
    - "No-skip-as-green extended to the session/egress env (sessionImage/runDirOrSkip/egressEnvOrSkip t.Fatal under $CI)"
    - "_Live test-name suffix to disambiguate the live integration leg from the untagged unit-tier counterpart (TestSessions_ConcurrentSerialized_Live, TestWorkspace_SymlinkEscapeCascade_Live)"
    - "Threat register consolidated from per-plan threat_models with implementing-file citations; AR extension documented before the live confirmation (anti-silent-ship discipline)"

key-files:
  created:
    - internal/sandbox/sessions_live_integration_test.go
    - internal/sandbox/workspace_integration_test.go
    - internal/sandbox/network_integration_test.go
    - .planning/phases/08-sandbox-2b-session-bound/08-SECURITY.md
  modified:
    - internal/sandbox/docker_integration_test.go
    - internal/sandbox/sessions_integration_test.go
    - .github/workflows/sandbox.yml
    - docs/aura-quality-snapshot.md
    - .planning/phases/08-sandbox-2b-session-bound/08-VALIDATION.md

key-decisions:
  - "Renamed the two live tests whose RESEARCH-mapped names collided with the untagged unit-tier tests (sessions_test.go/workspace_test.go are compiled under EVERY tag set) to _Live suffixes — the unit names stay, the live leg disambiguates."
  - "The live session-persistence/TTL/concurrent/boot tests need BOTH a live container AND the registry, so they carry `sandbox_integration && db_integration` and reuse sidecarURL (sandbox tier) + migratedPool/envOrSkip (db tier) — both helper files compile under the combined tag set."
  - "The 0008 round-trip is asserted as a schema-contract + cascade test (table/index/uuid-FK/CHECK + ON DELETE CASCADE) rather than a step-down via the migrate API, since the public db API exposes Up (and Reset=Down+Up) but not a single-step down; the cascade is the FK the down DROP relies on."
  - "threats_open held at 5 (not zeroed): the block-on-high mitigate threats whose proof is the live tier remain open until the Task-3 human Gate-3 live green — mirrors 05-SECURITY's no-assumption discipline."

patterns-established:
  - "Live egress reachability spike as a load-bearing test: TestNetwork_PyPIAllowed IS the landmine-3 probe — a red proves the bridge/seccomp posture wrong, fix the posture not the test."
  - "Per-arch live-env resolution (sessionRuntime defaults to runc for dev, CI sets runsc) so a plain Docker Desktop host runs the persistence tier without gVisor installed."

requirements-completed: []  # CAP-02 is NOT completed — it closes on the Task-3 human Gate-3 sign-off.

# Metrics
duration: ~40min
completed: 2026-06-03
---

# Phase 08 Plan 09: Gate-3 Evidence — Live Integration Tier + 08-SECURITY Summary

**Authored the live `sandbox_integration && db_integration` tier proving the 4 ROADMAP CAP-02 criteria against real containers, consolidated the T-08-* threat register into 08-SECURITY (extending AR-05-01 for the connect-allowed 2b egress posture), and wired the gating 2b DinD CI job — all compile-green; CAP-02 + threats_open=0 remain held for the Task-3 human Gate-3 live sign-off.**

## Performance

- **Duration:** ~40 min
- **Tasks:** 2 of 3 (Task 3 is a blocking human-verify checkpoint, NOT executed)
- **Files modified:** 9 (4 created, 5 modified)

## Accomplishments

### Task 1 — Live integration tier (commit f99d9cb4)
- **sessions_live_integration_test.go** (`sandbox_integration && db_integration`): `TestSessions_PythonStatePersists` (crit 1a + cross-conv isolation), `TestSessions_WorkspacePersists` (crit 1b), `TestSessions_ConcurrentSerialized_Live` (crit 1c, -race, asserts one container), `TestReaper_LiveContainerRemoved` (crit 3 live — real docker rm + registry terminated + liveCount 0), `TestSessions_BootRecovery_LazyRecreate` (D-06).
- **workspace_integration_test.go**: `TestWorkspace_SymlinkEscapeCascade_Live` (crit 2) — plants `ln -s /etc /workspace/escape` INSIDE the container then runs the host os.Root no-follow `PurgeConversationDir` cascade; asserts host `/etc/hostname` survives (os.SameFile identity) + the symlink + conv dir are removed.
- **network_integration_test.go**: `TestNetwork_PyPIAllowed` (crit 4a + the landmine-3 bridge-gateway reachability spike — a failing pip-to-pypi proves the posture wrong) + `TestNetwork_NonAllowlistRefused` (crit 4b deny-wins).
- **sessions_integration_test.go**: added `TestMigration0008_SchemaRoundTrip` (table + status/last_used index + uuid FK landmine #1 + status CHECK + ON DELETE CASCADE).
- **docker_integration_test.go**: added `sessionRuntime`/`sessionImage`/`sessionSeccomp` live-env helpers (no-skip-as-green).
- **08-VALIDATION.md**: Per-Task Verification Map populated (12 rows), `nyquist_compliant`+`wave_0_complete` set true, Wave-0 reqs ticked.

### Task 2 — 08-SECURITY + CI + coverage + snapshot (commit d4851c59)
- **08-SECURITY.md**: 40-threat T-08-* register consolidated from the eight PLAN threat_models, every block-on-high threat citing an implementing file; AR-05-01 EXTENDED for the 2b connect-allowing session posture (host-proxy-contained, bridge-gateway-reachable, empty-allowlist-egressless — documented before live confirmation); the Claude-Code allowlist-bypass caveat recorded as AR-08-01. `threats_open: 5` (NOT zeroed).
- **.github/workflows/sandbox.yml**: new `sandbox-2b-session-gate` job — Postgres-through-0008 + sidecar + egress bridge/host proxy at the bridge gateway; 2b session/workspace/migration tier (race) + the live egress leg + go-mutesting >=70% on network.go/scoring.go/sessions.go + 85% coverage fold; `CI=true` + composed DSNs + `AURA_SANDBOX_*` exported (no-skip-as-green). Path triggers add `internal/scoring/**` + the 0008 migration.
- **docs/aura-quality-snapshot.md**: Phase-8 2b detail rows (live tier, egress spike, 3-file mutation, folded coverage) + the AR-05-01 extension note.

## Verification

Task 1 (`<automated>` COMPILE-ONLY per the plan — live execution is Task 3):
- `go vet -tags 'sandbox_integration db_integration' ./internal/sandbox/` → **exit 0**
- `go build -tags 'sandbox_integration db_integration' ./internal/sandbox/` → **exit 0**
- `go test -tags 'sandbox_integration db_integration' -run xxxNONExxx ./internal/sandbox/` (test-binary compile) → **exit 0**
- `go test -run xxxNONExxx ./internal/sandbox/ ./internal/scoring/` (unit tier still compiles) → **exit 0**
- All 8 named criterion tests + the 0008 round-trip + boot lazy-recreate are discoverable under the tags (`go test -list`).

Task 2 (`<automated>`):
- `test -f .planning/phases/08-sandbox-2b-session-bound/08-SECURITY.md` → **yes**
- `grep -c AR-05-01 08-SECURITY.md` → **11**
- `grep -c sandbox_integration .github/workflows/sandbox.yml` → **10**

Pre-commit hooks (gofmt + vet + file-size ≤600 LOC) passed on both commits.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Test-name collision with the untagged unit tier**
- **Found during:** Task 1 (first `go vet` under the tags)
- **Issue:** The RESEARCH Test Map named `TestSessions_ConcurrentSerialized` and `TestWorkspace_SymlinkEscapeCascade` for the integration tier, but those exact names already exist in the untagged unit-tier files (`sessions_test.go`, `workspace_test.go`), which compile under EVERY tag set → `redeclared in this block`.
- **Fix:** Renamed the live tests to `TestSessions_ConcurrentSerialized_Live` / `TestWorkspace_SymlinkEscapeCascade_Live` (matching the established `TestReaper_LiveContainerRemoved` `_Live` convention); the unit names stay, the VALIDATION map records both legs.
- **Files modified:** sessions_live_integration_test.go, workspace_integration_test.go
- **Commit:** f99d9cb4

**2. [Rule 2 - Critical] 0008 round-trip as a schema-contract + cascade test**
- **Found during:** Task 1
- **Issue:** The plan asks for a "0008 up/down round-trip"; the public `internal/db` API exposes `Migrate` (Up) and `Reset` (Down+Up) but not a single-step 0008-only down.
- **Fix:** Authored `TestMigration0008_SchemaRoundTrip` asserting the up-side schema contract (table + index + uuid FK + CHECK) AND the ON DELETE CASCADE the down migration's DROP depends on — proving the round-trip contract without a private single-step-down hook.
- **Files modified:** sessions_integration_test.go
- **Commit:** f99d9cb4

## Known Stubs

None. The live tests are not stubs — they are full assertions gated behind build tags + no-skip-as-green env discipline; their live execution is the Task-3 human Gate-3 (a deliberate human-verify boundary, not a stub).

## Checkpoint Pending

**Task 3 is a `checkpoint:human-verify gate="blocking-human"` and was NOT executed.** CAP-02 is NOT marked complete and `08-SECURITY.threats_open` is NOT zeroed — both close only on the operator's live Gate-3 sign-off (running the real gVisor containers + live pip→pypi + symlink cascade + TTL reap in WSL with the stack up). See the orchestrator checkpoint payload.

## Self-Check: PASSED
