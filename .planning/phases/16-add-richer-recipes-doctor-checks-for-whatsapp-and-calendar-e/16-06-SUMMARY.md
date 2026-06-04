---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 06
subsystem: mcp
tags: [mcp, runtime, docker, trust, isolation]
requires:
  - phase: 16-mcp-sidecar-manager-third-party-trust
    provides: Managed config v2, profiles, trust classes, status snapshots
provides:
  - MCP runtime policy helpers
  - Docker and Docker MCP Gateway launch config generation
  - Trust-gated chat boot server filtering
  - Trust-gated single-server doctor launch
affects: [cmd/aura, internal/config, internal/mcp, internal/mcp/manager]
tech-stack:
  added: []
  patterns: [runtime-policy, trust-gate, docker-stdio-wrapper]
key-files:
  created:
    - internal/mcp/manager/runtime.go
    - internal/mcp/manager/runtime_test.go
  modified:
    - internal/mcp/managed_config.go
    - internal/mcp/managed_config_test.go
    - internal/config/config.go
    - cmd/aura/mcp.go
    - cmd/aura/mcp_test.go
    - cmd/aura/main_test.go
key-decisions:
  - "Manual local MCP servers default to blocked unless explicitly trusted or configured with sandboxed/container runtime metadata."
  - "Dockerized stdio servers use docker run -i --rm with no host mounts and no network by default."
  - "Blocked servers are filtered before chat boot and reported by doctor without launching their command."
patterns-established:
  - "Runtime launch policy lives in internal/mcp/manager and is consumed by config loading."
  - "Managed config validates docker/docker_gateway runtime metadata without requiring a local command."
requirements-completed: [CAP-09]
duration: 10 min
completed: 2026-06-04
---

# Phase 16 Plan 06: Runtime Isolation And Trust Gates Summary

**Trust-gated MCP launch policy with deterministic Docker/Gateway runtime generation**

## Performance

- **Duration:** 10 min
- **Started:** 2026-06-04T14:34:22Z
- **Completed:** 2026-06-04T14:44:24Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Added runtime helpers that convert managed MCP server metadata into launch configs.
- Added Docker stdio launch generation with `docker run -i --rm`, default `--network none`, explicit mounts only, CPU/memory flags, env forwarding, and network allow metadata.
- Added Docker MCP Gateway launch generation through `docker mcp gateway run --profile <profile>`.
- Added runtime validation so Docker/Gateway managed servers can be saved without a local command.
- Wired chat boot config loading through the runtime policy, filtering blocked servers before startup.
- Updated single-server `aura mcp doctor <name>` to report blocked/trust-needed without launching the configured command.
- Added regression tests proving blocked fake commands are not executed by chat boot or doctor.

## Task Commits

1. **Task 1: Add runtime policy and Docker command generation** - `7b85f582` (feat)
2. **Task 2: Enforce trust gates before chat boot and doctor launch** - `7b85f582` (feat)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `internal/mcp/manager/runtime.go` - Runtime policy, Docker/Gateway command generation, blocked-server filtering.
- `internal/mcp/manager/runtime_test.go` - Docker defaults, explicit mounts/resources, Windows mount pass-through, Gateway, and blocked policy coverage.
- `internal/mcp/managed_config.go` - Runtime kind constants and validation for local/Docker/Gateway stdio servers.
- `internal/mcp/managed_config_test.go` - Docker runtime config round-trip coverage.
- `internal/config/config.go` - Loads managed MCP servers through runtime policy.
- `cmd/aura/mcp.go` - Blocks `doctor <name>` before startup for blocked managed servers.
- `cmd/aura/mcp_test.go` - Doctor blocked-launch regression and trusted fixture updates.
- `cmd/aura/main_test.go` - Chat boot blocked-launch regression.

## Decisions Made

- Kept Docker network domain allowlists as explicit metadata via `AURA_MCP_NETWORK_ALLOW`; the Docker invocation stays least-privilege by default.
- Preserved existing fail-soft boot behavior for broken trusted servers while skipping blocked servers before transport startup.
- Used a typed blocked sentinel internally so only trust-gate errors are swallowed during boot filtering.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED tests could not be committed separately**
- **Found during:** Task 1-2 TDD setup
- **Issue:** The pre-commit hook runs `go vet ./...`, so intentionally non-compiling RED tests cannot be committed.
- **Fix:** Ran the RED command, verified missing runtime/trust-gate failures, then committed once green.
- **Verification:** plan verification commands listed below.
- **Committed in:** `7b85f582`

---

**Total deviations:** 1 auto-fixed (1 workflow constraint).
**Impact on plan:** No implementation scope change; TDD evidence was recorded without committing a hook-rejected red tree.

## Issues Encountered

- Existing doctor tests used manual fixtures that are now correctly blocked by default. The fixtures were updated to explicit trusted recipe metadata where launch behavior, not trust gating, was under test.

## User Setup Required

None - Docker/Gateway commands are generated from config metadata but no Docker daemon is required for tests.

## Next Phase Readiness

16-07 can layer risk labels and mount-time tool policy onto the managed config/runtime path now used by chat boot.

## Verification

- `go test ./internal/mcp/manager/ -run 'TestDockerRuntime|TestGatewayRuntime|TestRuntimePolicy' -count=1` - passed
- `go test ./cmd/aura/ ./internal/mcp/manager/ -run 'TestTrustGate|TestBlocked|TestBuildRegistry' -count=1` - passed
- `go test ./internal/mcp/ -run 'TestManagedConfigDockerRuntime|TestManagedConfigTrust|TestManagedConfigProfile|TestManagedConfigRoundTrip' -count=1` - passed
- `go test ./cmd/aura/ ./internal/config/ ./internal/mcp/ ./internal/mcp/manager/ -count=1` - passed
- pre-commit hook for `7b85f582`: `gofmt`, `go vet ./...`, file-size guard - passed

## Self-Check: PASSED

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
