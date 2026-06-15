---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 07
subsystem: mcp
tags: [mcp, policy, risk-labels, tool-registry, safety]
requires:
  - phase: 16-mcp-sidecar-manager-third-party-trust
    provides: Managed config v2, recipe metadata, status snapshots, runtime trust gates
provides:
  - MCP tool risk-label inference
  - Managed tool policy decisions with block reasons
  - Policy-aware MCP bridge/mount path
  - Risk/status visibility in `aura mcp tools` and `aura mcp status`
affects: [cmd/aura, internal/config, internal/agent/mcptools, internal/mcp/manager]
tech-stack:
  added: []
  patterns: [conservative-risk-inference, policy-aware-mount, blocked-tool-reasons]
key-files:
  created:
    - internal/mcp/manager/policy.go
    - internal/mcp/manager/policy_test.go
  modified:
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/mount.go
    - internal/agent/mcptools/mount_test.go
    - internal/config/config.go
    - cmd/aura/main.go
    - cmd/aura/mcp.go
    - cmd/aura/mcp_status.go
    - cmd/aura/mcp_test.go
    - cmd/aura/registry_test.go
    - internal/mcp/manager/status.go
key-decisions:
  - "Risk inference is conservative: unknown and destructive risks are denied by default in managed policy decisions."
  - "The raw Bridge/Mount helpers remain backward-compatible; Aura's managed boot path uses the policy-aware mount wrapper."
  - "Recipe/server risk labels add to inferred tool labels instead of hiding unknown tool risk."
patterns-established:
  - "Policy decisions carry risk labels, allowed/blocked state, and block reasons for both runtime enforcement and CLI/status output."
  - "Config loading preserves managed server metadata beside runtime launch configs so chat boot can enforce the same policy users see."
requirements-completed: [CAP-09]
duration: 13 min
completed: 2026-06-04
---

# Phase 16 Plan 07: Risk Labels And Tool Policy Summary

**Conservative risk labels with mount-time blocking before MCP tools enter Aura's registry**

## Performance

- **Duration:** 13 min
- **Started:** 2026-06-04T14:46:57Z
- **Completed:** 2026-06-04T15:00:06Z
- **Tasks:** 3
- **Files modified:** 12

## Accomplishments

- Added risk labels for `read`, `write`, `network`, `filesystem`, `destructive`, `private_data`, `external_send`, and `unknown`.
- Added conservative heuristics for read/list/search/fetch/get, create/update/delete/send/post/upload, filesystem, private-data, and destructive tool surfaces.
- Added managed policy decisions with allow, deny, deny-risk, risk labels, and block reasons.
- Added policy-aware bridge/mount functions that drop blocked tools before registry registration while retaining block reasons.
- Preserved the older raw `Bridge`/`Mount` helpers for low-level bridge tests and non-managed callers.
- Carried managed server policy metadata through config loading for chat boot enforcement.
- Updated `aura mcp tools <name>` to show risk labels and mounted/blocked status per advertised tool.
- Updated `aura mcp status` text and JSON output with mounted/blocked counts, risk labels, and block reasons.

## Task Commits

1. **Task 1: Implement risk-label inference and recipe overrides** - `9df0a3e8` (feat)
2. **Task 2: Enforce policy at mount time** - `9df0a3e8` (feat)
3. **Task 3: Show risk labels and block reasons in CLI** - `9df0a3e8` (feat)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `internal/mcp/manager/policy.go` - Risk inference, policy decisions, configured block summaries.
- `internal/mcp/manager/policy_test.go` - Risk inference, overrides, allow/deny/deny-risk coverage.
- `internal/agent/mcptools/bridge.go` - Adds policy-aware bridge and shared registration helper.
- `internal/agent/mcptools/mount.go` - Adds policy-aware process mount wrapper.
- `internal/agent/mcptools/mount_test.go` - Proves risky tools are not registered and reasons are retained.
- `internal/config/config.go` - Carries managed policy metadata alongside runtime configs.
- `cmd/aura/main.go` - Enforces policy at chat boot and logs blocked tools.
- `cmd/aura/mcp.go` - Shows risk labels and block reasons in `mcp tools`.
- `cmd/aura/mcp_status.go` - Adds mounted/blocked columns.
- `cmd/aura/mcp_test.go` - CLI policy visibility coverage.
- `cmd/aura/registry_test.go` - Adds fake MCP policy tool fixture mode.
- `internal/mcp/manager/status.go` - Adds risk labels, counts, and configured block reasons.

## Decisions Made

- Denied risks always include `destructive` and `unknown` by default for managed policy decisions, even if a server does not explicitly set `denyRisk`.
- Server/recipe risk labels add context such as `private_data`, but they do not suppress `unknown` when the tool itself cannot be classified.
- `aura mcp tools` evaluates live advertised tools, while `aura mcp status` reports configured policy counts without launching servers.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED tests could not be committed separately**
- **Found during:** Task 1-3 TDD setup
- **Issue:** The pre-commit hook runs `go vet ./...`, so intentionally non-compiling RED tests cannot be committed.
- **Fix:** Ran the RED command, verified missing risk/policy APIs and mount wrapper failures, then committed once green.
- **Verification:** plan verification commands listed below.
- **Committed in:** `9df0a3e8`

**2. [Rule 1 - Bug] Email read tools were initially over-labeled as external sends**
- **Found during:** CLI policy test run
- **Issue:** The first heuristic treated any `email` token as `external_send`, causing `fetch_emails` to show send risk.
- **Fix:** Narrowed `external_send` to action verbs/protocol surfaces such as `send`, `post`, `upload`, `reply`, `smtp`, and `whatsapp`.
- **Verification:** `go test ./cmd/aura/ -run 'TestMCP.*Tools|TestMCP.*Status' -count=1`
- **Committed in:** `9df0a3e8`

---

**Total deviations:** 2 auto-fixed (1 workflow constraint, 1 heuristic bug).
**Impact on plan:** No scope change; final behavior is more precise while staying conservative.

## Issues Encountered

- None remaining.

## User Setup Required

None.

## Next Phase Readiness

16-08 can run mock E2E and documentation/quality-snapshot checks against the full manager path: recipes, profiles, status/doctor, Streamable HTTP, trust gates, runtime isolation, and policy enforcement.

## Verification

- `go test ./internal/mcp/manager/ -run 'TestRisk|TestPolicy' -count=1` - passed
- `go test ./internal/agent/mcptools/ ./internal/mcp/manager/ -run 'TestMount|TestPolicy|TestRisk' -count=1` - passed
- `go test ./cmd/aura/ -run 'TestMCP.*Tools|TestMCP.*Status' -count=1` - passed
- `go test ./cmd/aura/ ./internal/config/ ./internal/mcp/ ./internal/mcp/manager/ ./internal/agent/mcptools/ -count=1` - passed
- pre-commit hook for `9df0a3e8`: `gofmt`, `go vet ./...`, file-size guard - passed

## Self-Check: PASSED

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
