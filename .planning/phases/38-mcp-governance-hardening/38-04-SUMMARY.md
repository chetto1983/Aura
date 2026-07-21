---
phase: 38-mcp-governance-hardening
plan: 04
subsystem: mcp
tags: [mcp, classify, trust, transport, governance, identity, elevation-of-privilege]

# Dependency graph
requires:
  - phase: 38-mcp-governance-hardening (plan 01)
    provides: "internal/mcp/classify.go — Classify(ManagedServer) (serverType, trust string, err error): the canonical transport+trust classifier"
provides:
  - "internal/mcp/manager/runtime.go's normalizedTrustForServer/isStreamableHTTPServer and internal/mcp/manager/status.go's runtimeName migrated onto mcp.Classify (call sites #5/#6/#7); the manager's OWN independently-duplicated F-013 auto-promote branch deleted"
  - "D-04 remote-trust elevation guard: MountForIdentity silently ignores a per-identity trust override for a REMOTE (streamable_http) server; SetTrustForIdentity/mutateIdentityPref fails closed with ErrRemoteElevationForbidden and persists no overlay"
affects: [38-05, 38-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Classify-first dispatch extended to the manager package's eligibility gate and the per-identity overlay/write path — no new decision bodies, only thin callers"
    - "isTrustMutation flag on a shared write-path helper (mutateIdentityPref) lets one mutate-and-persist function host a trust-only fail-closed guard without duplicating the shared-catalog lookup"

key-files:
  created: []
  modified:
    - internal/mcp/manager/runtime.go
    - internal/mcp/manager/status.go
    - internal/mcp/manager/runtime_test.go
    - internal/mcp/manager/coverage_extra_test.go
    - internal/mcp/managed_config_identity.go
    - internal/mcp/managed_config_identity_test.go

key-decisions:
  - "The manager's normalizedTrustForServer/isStreamableHTTPServer and status.go's runtimeName now delegate to mcp.Classify; a Classify error is treated conservatively (TrustBlocked / not-streamable / \"local\"), matching 38-01's legacy-wrapper convention — the manager path gates run-eligibility so ambiguity must fail closed."
  - "D-04 guard shape (per planner_assumptions): MountForIdentity SILENTLY IGNORES a remote's trust override (mirroring the existing silent IsSharedAdminGoverned ignore), while SetTrustForIdentity/mutateIdentityPref returns the new ErrRemoteElevationForbidden sentinel so a deliberate CLI/web elevation attempt fails loudly rather than silently no-op'ing. Both shapes leave the enable-toggle path completely unaffected."
  - "isRemoteTransport(s ManagedServer) bool is the new single classification point for the D-04 boundary — it wraps Classify and treats a Classify error as \"not remote\" (conservative default: apply the override, since the shared catalog's own shape is admin-authored/validated at write time, not something this per-identity guard needs to re-litigate)."

patterns-established:
  - "A boolean flag on a shared mutate-and-persist helper (mutateIdentityPref's isTrustMutation) scopes a fail-closed guard to only the write paths that need it, without splitting the function or duplicating the shared-catalog lookup + admin-governed check."

requirements-completed: [MCPH-01, MCPH-02]

coverage:
  - id: D1
    description: "manager/runtime.go's normalizedTrustForServer and isStreamableHTTPServer route through mcp.Classify; the manager's own independently-duplicated F-013 auto-promote branch is deleted, closing the bug at this second copy."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "internal/mcp/manager/runtime_test.go#TestNormalizedTrustForServer (http_type_with_no_explicit_trust_blocked, url_implies_http_with_no_explicit_trust_blocked subtests)"
        status: pass
      - kind: unit
        ref: "internal/mcp/manager/runtime_test.go#TestRunnableManagedServersBareRemoteTrustBlocked"
        status: pass
      - kind: unit
        ref: "internal/mcp/manager/coverage_extra_test.go#TestRuntimeErrorAndClassificationBranches"
        status: pass
    human_judgment: false
  - id: D2
    description: "manager/status.go's runtimeName routes through mcp.Classify instead of an inline URL/Type reimplementation; existing display-label behavior (docker/gateway kind, remote_http label, local default) is unchanged."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "internal/mcp/manager/status_test.go#TestRuntimeName"
        status: pass
    human_judgment: false
  - id: D3
    description: "MountForIdentity: a per-identity trust override for a class-(a) REMOTE (streamable_http, non-admin-governed) server is NOT applied — the effective config keeps the shared-catalog trust. The enable-toggle branch is unaffected; a LOCAL stdio override still applies unchanged."
    requirement: "MCPH-02"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestMountForIdentityRemoteTrustOverrideIgnored"
        status: pass
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestMountForIdentityRemoteEnableToggleApplies"
        status: pass
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestSetTrustForIdentityOverlaysClassA (pre-existing, unchanged — LOCAL stdio override regression guard)"
        status: pass
    human_judgment: false
  - id: D4
    description: "SetTrustForIdentity (via mutateIdentityPref) on a class-(a) REMOTE server returns the new ErrRemoteElevationForbidden sentinel and persists no overlay — a deliberate remote-elevation write fails closed and loudly."
    requirement: "MCPH-02"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestSetTrustForIdentityRemoteElevationForbidden"
        status: pass
    human_judgment: false

# Metrics
duration: ~20min
completed: 2026-07-18
status: complete
---

# Phase 38 · Plan 04 — Manager classifier collapse + D-04 remote-trust elevation guard

**The manager package's eligibility resolver (runtime.go/status.go) now routes through the canonical `Classify` — deleting its own independent copy of the F-013 auto-promote bug — and a per-identity overlay can no longer elevate a REMOTE (streamable_http) server's trust, only the admin shared catalog can.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-18T10:35:00Z (approx.)
- **Completed:** 2026-07-18T10:43:06Z
- **Tasks:** 2 (Task 2 is TDD: RED + GREEN)
- **Files modified:** 6

## Accomplishments

- `manager/runtime.go`'s `normalizedTrustForServer`/`isStreamableHTTPServer` and `manager/status.go`'s `runtimeName` migrated onto `mcp.Classify` (call sites #5/#6/#7 of the classifier collapse) — the manager's own independently-duplicated F-013 auto-promote-to-`TrustRemoteHTTP` branch is deleted; a bare-URL/bare-type remote with no explicit trust now resolves `TrustBlocked` at this copy too.
- New `TestRunnableManagedServersBareRemoteTrustBlocked` proves the fixed behavior end-to-end: a bare-URL remote with unset trust is excluded from the runnable set.
- D-04 remote-trust elevation guard added: `isRemoteTransport` (a thin `Classify` wrapper) gates both `MountForIdentity` (silent ignore of a remote's trust override) and `mutateIdentityPref`/`SetTrustForIdentity` (fail-closed `ErrRemoteElevationForbidden` sentinel, no overlay persisted).
- Full TDD cycle for the D-04 guard: RED commit added a class-(a) REMOTE `weather` fixture + 3 tests (2 initially failing, 1 already-passing enable-toggle regression guard) with the sentinel declared as a compiling stub; GREEN commit wired the actual guard logic and all tests pass.

## Task Commits

Each task committed atomically:

1. **Task 1: Migrate manager/runtime.go + manager/status.go onto Classify** — `0541a75f` (refactor)
2. **Task 2 — RED**: add failing tests for D-04 remote-trust elevation guard — `85441315` (test)
3. **Task 2 — GREEN**: enforce D-04 remote-trust elevation guard — `965e4307` (feat)

_Note: Task 2 had no separate REFACTOR commit — the GREEN implementation was already minimal (one classification helper + two call-site guards); no cleanup was needed._

## Files Created/Modified

- `internal/mcp/manager/runtime.go` — `normalizedTrustForServer`/`isStreamableHTTPServer` now delegate to `mcp.Classify`; duplicate F-013 branch removed.
- `internal/mcp/manager/status.go` — `runtimeName` now delegates to `mcp.Classify` instead of an inline `URL != "" || Type == ...` check.
- `internal/mcp/manager/runtime_test.go` — rewrote the two `TestNormalizedTrustForServer` cases that encoded the manager's copy of the F-013 bug; added `TestRunnableManagedServersBareRemoteTrustBlocked`.
- `internal/mcp/manager/coverage_extra_test.go` — rewrote the equivalent bug-encoding assertion in `TestRuntimeErrorAndClassificationBranches`.
- `internal/mcp/managed_config_identity.go` — added `ErrRemoteElevationForbidden` sentinel, `isRemoteTransport` helper, D-04 guard in `MountForIdentity`'s overlay block, and `isTrustMutation` parameter on `mutateIdentityPref` wired from `SetTrustForIdentity`.
- `internal/mcp/managed_config_identity_test.go` — added a class-(a) REMOTE `weather` fixture to `writeSharedCatalog` and 3 new tests covering the D-04 `<behavior>` rows.

## Decisions Made

See `key-decisions` frontmatter. Notably: the D-04 guard's dual shape (silent ignore in the merge path, loud sentinel error in the write path) follows the planner's explicit `planner_assumptions` spec, not an executor invention.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `-race` unavailable in this worktree (no CGO toolchain)**
- **Found during:** Task 1 verification (`go test ./internal/mcp/manager/ -race`)
- **Issue:** `-race requires cgo; enable cgo by setting CGO_ENABLED=1`; no `gcc`/w64devkit toolchain is on PATH in this Windows worktree, and no `~/.aura-toolchain.sh` BASH_ENV shim exists here.
- **Fix:** Ran the full test suites without `-race` per the plan's own `<project_validation>` fallback instruction ("if `-race` fails to link on Windows fall back to non-race and note it"). All targeted and full-package tests pass non-race. `-race` re-verification is deferred to the phase-close full-matrix run (WSL), consistent with 38-01-SUMMARY.md's precedent ("`-race` re-run belongs to the phase-close full-matrix verification (WSL)").
- **Files modified:** none (verification-only).
- **Verification:** `go test ./internal/mcp/ ./internal/mcp/manager/` (non-race) — both packages green.
- **Committed in:** N/A (no code change; documented here per deviation discipline).

**2. [Rule 3 - Blocking] Stale golangci-lint cache referenced a deleted sibling worktree**
- **Found during:** Task 1's first commit attempt
- **Issue:** The pre-commit `lint` hook failed with `gosec` findings in files this commit never touched (`internal/mcp/client.go`, `managed_config.go`, `managed_config_identity.go`, `internal/procgroup/procgroup_windows.go`), all reported against the path of a sibling worktree (`agent-ac8401d6521d1a711`) that no longer exists on disk — a stale shared `golangci-lint` build/result cache from a parallel worktree run.
- **Fix:** Ran `golangci-lint cache clean`, then re-ran `golangci-lint run $(bash scripts/go_packages.sh)` directly to confirm 0 issues before retrying the commit. The retried commit's lint hook passed cleanly.
- **Files modified:** none (cache-only; no source changes).
- **Verification:** `golangci-lint run ./internal/mcp/...` and the full owned-package invocation both reported "0 issues" after the cache clean; the retried `git commit` passed the lint hook.
- **Committed in:** N/A (tooling-cache issue, not a code change).

---

**Total deviations:** 2 auto-fixed (both Rule 3 — blocking, tooling/environment only; no source-code deviations from the plan's specified actions).
**Impact on plan:** Neither deviation touched implementation scope. All plan-specified source changes were implemented exactly as written; both issues were pre-existing environment/tooling conditions blocking verification, resolved without altering the deliverable.

## Issues Encountered

None beyond the two tooling deviations documented above.

## Next Phase Readiness

- **38-05** (mount.go path) and **38-06** (mcp_status.go/doctor.go) remain to migrate their call sites onto the `Classify` contract; this plan's fixes (manager copy of F-013, D-04 guard) do not touch those files.
- `internal/mcp` and `internal/mcp/manager` are both green under `go vet`, `go build ./...`, and `go test` (non-race). `-race` re-verification and the phase's full-matrix coverage gate belong to phase-close (WSL), per the established 38-01 precedent.
- No new env vars, CLI flags, or migrations were introduced (matches `<artifacts_this_phase_produces>`).

---
*Phase: 38-mcp-governance-hardening*
*Completed: 2026-07-18*

## Self-Check: PASSED

- All 6 key-files (created/modified) verified present on disk with `[ -f ]`.
- All 3 task commit hashes (`0541a75f`, `85441315`, `965e4307`) verified present in `git log --oneline --all`.
- `go vet ./internal/mcp/ ./internal/mcp/manager/` — clean.
- `go build ./...` — clean.
- `go test ./internal/mcp/ ./internal/mcp/manager/` (non-race, see Deviations) — both green.
- Plan `<verification>` greps (no residual `TrustRemoteHTTP` auto-promote in `manager/runtime.go`; no inline `URL != ""` classification in `manager/*.go`) — both clean.
- `bash scripts/check-file-size.sh` — all 2009 tracked source files within the 600-LOC cap.
