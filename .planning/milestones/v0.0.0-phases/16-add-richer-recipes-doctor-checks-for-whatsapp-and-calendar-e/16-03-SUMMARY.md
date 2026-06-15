---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 03
subsystem: cli
tags: [mcp, recipes, profiles, trust, calendar]
requires:
  - phase: 16-mcp-sidecar-manager-third-party-trust
    provides: Managed config v2 model and trust metadata
provides:
  - Built-in MCP recipe catalog metadata
  - `aura mcp recipes` table and JSON output
  - Calendar fixture recipe metadata
  - MCP profile management commands
  - Explicit MCP trust approval command
affects: [cmd/aura, internal/mcp/manager, internal/mcp]
tech-stack:
  added: []
  patterns: [catalog-helper, profile-membership, explicit-trust-approval]
key-files:
  created:
    - internal/mcp/manager/catalog.go
    - internal/mcp/manager/catalog_test.go
    - cmd/aura/mcp_profile.go
  modified:
    - cmd/aura/mcp.go
    - cmd/aura/mcp_test.go
    - internal/mcp/managed_config.go
key-decisions:
  - "Built-in recipe metadata lives in internal/mcp/manager instead of the top-level CLI file."
  - "Manual `aura mcp add` entries record `blocked` trust by default unless explicitly trusted."
  - "Profile membership is updated on install/add using the active profile."
patterns-established:
  - "CLI command groups can move into focused cmd/aura/mcp_*.go files to keep mcp.go below the size cap."
  - "Recipe catalog entries carry trust, runtime, env, risk, and tool policy metadata."
requirements-completed: [CAP-09]
duration: 7 min
completed: 2026-06-04
---

# Phase 16 Plan 03: MCP Manager CLI Summary

**Recipe catalog, Calendar fixture metadata, profile commands, and explicit trust approval for `aura mcp`**

## Performance

- **Duration:** 7 min
- **Started:** 2026-06-04T14:11:33Z
- **Completed:** 2026-06-04T14:18:32Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- Added manager-owned built-in catalog metadata for calculator, mail, WhatsApp, and Calendar.
- Added Calendar as a trusted recipe with deterministic fixture-mode env metadata.
- Added `aura mcp recipes` with table and `--json` output.
- Added `aura mcp profile list|create|use|add|remove`.
- Added `aura mcp trust <name>` and made manual `mcp add` entries default to `blocked`.

## Task Commits

1. **Task 1: Move recipe metadata into a catalog helper** - `97a87cab` and `e84339ce` (feat)
2. **Task 2: Add profile and trust CLI commands** - `e84339ce` (feat)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `internal/mcp/manager/catalog.go` - Built-in recipe catalog with trust/runtime/env/risk/tool-policy metadata.
- `internal/mcp/manager/catalog_test.go` - Catalog and Calendar fixture coverage.
- `cmd/aura/mcp.go` - Recipe command, catalog-backed install, and blocked-default manual add behavior.
- `cmd/aura/mcp_profile.go` - Profile and trust subcommands.
- `cmd/aura/mcp_test.go` - CLI tests for recipes, profile membership, trust approval, and blocked manual add.
- `internal/mcp/managed_config.go` - Profile-server resolution tightened so an existing empty profile means no servers.

## Decisions Made

- Recipe metadata moved out of the CLI file to avoid growing `cmd/aura/mcp.go`.
- The Calendar recipe starts fixture-first (`AURA_CALENDAR_MODE=fixture`) to keep CI deterministic.
- `aura mcp trust` currently approves local commands as `trusted_local`; richer sandbox/runtime choices are handled in 16-06.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED tests could not be committed separately**
- **Found during:** Task 1 and Task 2 TDD setup
- **Issue:** The pre-commit hook runs `go vet ./...`, so intentionally non-compiling RED tests cannot be committed.
- **Fix:** Ran the RED command, verified the expected missing command/helper failures, then committed the green implementation.
- **Verification:** `go test ./cmd/aura/ ./internal/mcp/manager/ -run 'TestMCP.*Recipe|TestCatalog|TestMCP.*Profile|TestMCP.*Trust|TestMCPAdd' -count=1`
- **Committed in:** `97a87cab`, `e84339ce`

**2. [Rule 2 - Missing Critical] Split profile/trust handlers out of `mcp.go`**
- **Found during:** Task 2 implementation
- **Issue:** Keeping all new subcommand handlers in `cmd/aura/mcp.go` would push a dense control-plane file toward the 600-line cap.
- **Fix:** Added `cmd/aura/mcp_profile.go` for profile/trust handlers while leaving the top-level switch in `mcp.go`.
- **Verification:** hook file-size check and explicit line count (`mcp.go` 510 LOC, `mcp_profile.go` 194 LOC).
- **Committed in:** `e84339ce`

---

**Total deviations:** 2 auto-fixed (1 workflow constraint, 1 maintainability guard).
**Impact on plan:** No behavior scope creep; the extra file preserves the local file-size convention.

## Issues Encountered

None beyond the documented RED-commit hook constraint.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 2 can continue with Streamable HTTP transport. Wave 3 can build status/doctor/runtime gates on top of the new catalog, profile, and trust command surface.

## Verification

- `go test ./cmd/aura/ ./internal/mcp/manager/ -run 'TestMCP.*Recipe|TestCatalog' -count=1` - passed
- `go test ./cmd/aura/ -run 'TestMCP.*Profile|TestMCP.*Trust|TestMCPAdd' -count=1` - passed
- `go test ./cmd/aura/ ./internal/mcp/manager/ -count=1` - passed

## Self-Check: PASSED

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
