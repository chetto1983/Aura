---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 04
subsystem: cli
tags: [mcp, status, doctor, logs, redaction]
requires:
  - phase: 16-mcp-sidecar-manager-third-party-trust
    provides: Managed config v2, catalog metadata, profiles, trust CLI
provides:
  - MCP status snapshots and JSON output
  - `aura mcp status`
  - `aura mcp doctor --all`
  - Redacted MCP diagnostic/log output
affects: [cmd/aura, internal/mcp, internal/mcp/manager]
tech-stack:
  added: []
  patterns: [status-snapshot, layered-doctor-checks, diagnostic-redaction]
key-files:
  created:
    - cmd/aura/mcp_status.go
    - internal/mcp/manager/status.go
    - internal/mcp/manager/status_test.go
    - internal/mcp/redact.go
  modified:
    - cmd/aura/mcp.go
    - cmd/aura/mcp_test.go
    - internal/mcp/client.go
    - internal/mcp/client_test.go
key-decisions:
  - "Blocked and disabled servers are visible in status without launching them."
  - "doctor --all performs non-secret config/runtime/recipe checks; single-server doctor remains compatible."
  - "MCP stderr tails are redacted before appearing in errors."
patterns-established:
  - "Status structs live in internal/mcp/manager and are rendered by the CLI as table or JSON."
  - "Doctor probes use fakeable command lookup seams for deterministic tests."
requirements-completed: [CAP-09]
duration: 9 min
completed: 2026-06-04
---

# Phase 16 Plan 04: MCP Status And Doctor Summary

**Stable MCP status snapshots, `doctor --all`, recipe diagnostics, and redacted stderr/log output**

## Performance

- **Duration:** 9 min
- **Started:** 2026-06-04T14:25:38Z
- **Completed:** 2026-06-04T14:34:22Z
- **Tasks:** 3
- **Files modified:** 8

## Accomplishments

- Added manager status snapshots with `starting|ready|failed|blocked|disabled|unknown`-style states shaped for Aura.
- Added `aura mcp status` and `aura mcp status --json`.
- Added `aura mcp doctor --all` with config/runtime/recipe checks that do not launch blocked servers.
- Added mail env, Calendar fixture, and WhatsApp bridge diagnostic lines.
- Added redaction for secret-bearing diagnostics and MCP stderr tails.
- Added `aura mcp logs <name>` placeholder output so the log surface exists without writing logs to git.

## Task Commits

1. **Task 1: Add status snapshots and JSON output** - `b7e18042` (feat)
2. **Task 2: Expand doctor to all servers and recipe-specific checks** - `b7e18042` (feat)
3. **Task 3: Add stderr/log tail capture to status/doctor** - `b7e18042` (feat)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `internal/mcp/manager/status.go` - Status snapshots, auth status, policy summaries, and redaction wrapper.
- `internal/mcp/manager/status_test.go` - Blocked/disabled/profile status coverage and redaction tests.
- `cmd/aura/mcp_status.go` - `status`, `doctor --all`, `logs`, runtime checks, and recipe checks.
- `cmd/aura/mcp.go` - Wires new subcommands and keeps single-server doctor compatible.
- `cmd/aura/mcp_test.go` - Status JSON and doctor-all redaction/recipe tests.
- `internal/mcp/redact.go` - Shared diagnostic redactor.
- `internal/mcp/client.go` - Redacts stderr tail before returning process diagnostics.
- `internal/mcp/client_test.go` - Stderr redaction coverage.

## Decisions Made

- Status does not launch blocked servers; it reports config/trust state only.
- `doctor --all` uses lightweight runtime/config/recipe checks and keeps transport startup in the existing `doctor <name>` path.
- Redaction is key-pattern based (`token`, `secret`, `pass`, `key`, `auth`, `bearer`, `credential`) and also handles `Authorization: Bearer ...`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED tests could not be committed separately**
- **Found during:** Task 1-3 TDD setup
- **Issue:** The pre-commit hook runs `go vet ./...`, so intentionally non-compiling RED tests cannot be committed.
- **Fix:** Ran the RED command, verified missing status/redaction failures, then committed once green.
- **Verification:** plan verification commands listed below.
- **Committed in:** `b7e18042`

**2. [Rule 3 - Blocking] Concurrent Phase 10 commit raced the first 16-04 commit attempt**
- **Found during:** Task commit
- **Issue:** A separate `docs(10): create scheduler phase plan` commit landed while the 16-04 commit/amend path was running, producing a ref-lock race. The unrelated Phase 10 plan files were not carried into the final 16-04 commit.
- **Fix:** Checked HEAD/status, left the untracked Phase 10 patterns file alone, and recommitted only the intended 16-04 files.
- **Verification:** `git diff --cached --name-status` before commit showed only 16-04 files; final commit `b7e18042` contains 8 intended files.
- **Committed in:** `b7e18042`

---

**Total deviations:** 2 auto-fixed (1 workflow constraint, 1 git race cleanup).
**Impact on plan:** No behavior scope change; unrelated Phase 10 artifacts were not included in the final 16-04 commit.

## Issues Encountered

- A concurrent Phase 10 planning commit appeared in history during this plan. The only remaining untracked related file is `.planning/phases/10-scheduler/10-PATTERNS.md`, which was not staged or modified.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

16-06 can use status/trust metadata to enforce runtime gates, and 16-07 can extend status/policy summaries with tool-level risk enforcement.

## Verification

- `go test ./cmd/aura/ ./internal/mcp/manager/ -run 'TestMCP.*Status|TestStatus' -count=1` - passed
- `go test ./cmd/aura/ -run 'TestMCP.*Doctor|TestProbe|TestRedact' -count=1` - passed
- `go test ./internal/mcp/ ./cmd/aura/ -run 'Test.*Stderr|Test.*Log|Test.*Redact' -count=1` - passed
- `go test ./cmd/aura/ ./internal/mcp/ ./internal/mcp/manager/ -count=1` - passed

## Self-Check: PASSED

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
