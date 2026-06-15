---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 02
subsystem: mcp
tags: [mcp, config, profiles, trust, redaction]
requires:
  - phase: 16-mcp-sidecar-manager-third-party-trust
    provides: CAP-09 / MCP-V2-01 amendment and design contract
provides:
  - Backwards-compatible managed config v2 model
  - Profile membership and active-profile helpers
  - Trust normalization for recipes, remote HTTP, and blocked local commands
  - Redacted profile export/import helpers
affects: [internal/mcp, internal/mcp/manager, cmd/aura, config]
tech-stack:
  added: []
  patterns: [backwards-compatible-json, secret-redaction, trust-normalization]
key-files:
  created:
    - internal/mcp/manager/config.go
    - internal/mcp/manager/config_test.go
  modified:
    - internal/mcp/managed_config.go
    - internal/mcp/managed_config_test.go
key-decisions:
  - "Managed config v2 defaults legacy files to version 2 and the default profile without changing the existing mcpServers shape."
  - "Trust normalization keeps recipe sources trusted and makes manual local commands blocked until explicit trust metadata exists."
  - "Profile export replaces secret env values with ${KEY} placeholders, and import preserves existing credentials unless explicitly overridden."
patterns-established:
  - "Config control-plane helpers live in internal/mcp/manager while the runtime MCP data plane stays in internal/mcp."
  - "Exports preserve server/source metadata but never include secret-bearing values by default."
requirements-completed: [CAP-09]
duration: 6 min
completed: 2026-06-04
---

# Phase 16 Plan 02: Managed Config V2 Summary

**Backwards-compatible MCP managed config v2 with profiles, trust metadata, runtime fields, and redacted profile sharing**

## Performance

- **Duration:** 6 min
- **Started:** 2026-06-04T14:05:28Z
- **Completed:** 2026-06-04T14:11:33Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- Extended `ManagedConfig` with versioning, profiles, server type, trust metadata, runtime metadata, tool policy, risk labels, and Streamable HTTP URL support.
- Added compatibility normalization so legacy `mcpServers` JSON loads with a v2 default version/profile.
- Added trust helpers proving `recipe:*` sources normalize to `trusted_recipe` and manual local commands normalize to `blocked`.
- Added `internal/mcp/manager` export/import helpers with secret env redaction and credential-preserving imports.

## Task Commits

Each task was committed atomically:

1. **Task 1: Extend managed config with profiles and trust metadata** - `4b304df4` (feat)
2. **Task 2: Add config export/import redaction helpers** - `ced56c50` (feat)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `internal/mcp/managed_config.go` - Adds config v2 structs, defaults, profile membership, trust normalization, and HTTP-aware validation.
- `internal/mcp/managed_config_test.go` - Covers legacy migration, trust defaults, and profile membership.
- `internal/mcp/manager/config.go` - Adds redacted profile export and non-overwriting profile import helpers.
- `internal/mcp/manager/config_test.go` - Covers secret redaction and credential-preserving import behavior.

## Decisions Made

- `EnabledServers` remains the stdio compatibility path and skips Streamable HTTP entries until the HTTP transport wave wires the new data plane.
- Manual/local servers can still load from legacy files, but `NormalizedTrust` exposes the safer `blocked` default for later boot gating.
- Secret export redaction uses deterministic `${KEY}` placeholders, preserving setup instructions without leaking values.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED tests could not be committed separately**
- **Found during:** Task 1 and Task 2 TDD setup
- **Issue:** The repository pre-commit hook runs `go vet ./...`; intentionally non-compiling RED tests are rejected by the hook.
- **Fix:** Ran the RED command and recorded the intended failures, then committed the green implementation once tests and hooks passed.
- **Files modified:** `internal/mcp/managed_config_test.go`, `internal/mcp/manager/config_test.go`
- **Verification:** `go test ./internal/mcp/ ./internal/mcp/manager/ -count=1`
- **Committed in:** `4b304df4`, `ced56c50`

---

**Total deviations:** 1 auto-fixed (1 blocking workflow constraint).
**Impact on plan:** TDD evidence was collected, but the RED state was not committed because project hooks enforce compiling tests.

## Issues Encountered

None in implementation. The only workflow constraint was the non-committable RED state described above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 2 can add CLI/catalog/profile commands and Streamable HTTP transport on top of the config v2 model. Later boot/runtime gates can use `NormalizedTrust`, `ProfileServerNames`, server `Type`, `Runtime`, and `ToolPolicy`.

## Verification

- `go test ./internal/mcp/ -run 'TestManagedConfig|TestProfile|TestTrust' -count=1` - passed
- `go test ./internal/mcp/ ./internal/mcp/manager/ -run 'Test.*Export|Test.*Import|Test.*Redact' -count=1` - passed
- `go test ./internal/mcp/ ./internal/mcp/manager/ -count=1` - passed

## Self-Check: PASSED

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
