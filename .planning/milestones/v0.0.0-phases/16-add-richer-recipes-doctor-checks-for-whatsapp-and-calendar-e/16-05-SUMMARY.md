---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 05
subsystem: mcp
tags: [mcp, streamable-http, transport, json-rpc, httptest]
requires:
  - phase: 16-mcp-sidecar-manager-third-party-trust
    provides: Managed config v2 with server type and URL fields
provides:
  - Common MCP Transport interface
  - Stdio client Ping support
  - OpenServer routing for stdio vs Streamable HTTP
  - Streamable HTTP MCP client with session/protocol headers
affects: [internal/mcp, internal/agent/mcptools, cmd/aura]
tech-stack:
  added: []
  patterns: [httptest-transport-fixture, json-rpc-http, session-header]
key-files:
  created:
    - internal/mcp/transport.go
    - internal/mcp/transport_test.go
    - internal/mcp/http_client.go
    - internal/mcp/http_client_test.go
  modified:
    - internal/mcp/client.go
    - internal/mcp/client_test.go
key-decisions:
  - "Stdio remains the compatibility baseline through Open and ServerConfig."
  - "Streamable HTTP uses protocol version 2025-06-18 and stores the server-provided Mcp-Session-Id."
  - "OpenServer chooses stdio or HTTP from ManagedServer.Type without changing existing stdio callers."
patterns-established:
  - "HTTP transport tests use httptest and assert headers instead of live remote services."
  - "HTTP auth headers can be supplied through explicit config or MCP_HEADER_*/MCP_BEARER_TOKEN env entries."
requirements-completed: [CAP-09]
duration: 7 min
completed: 2026-06-04
---

# Phase 16 Plan 05: Streamable HTTP MCP Summary

**Common MCP transport contract with stdio compatibility and a tested Streamable HTTP client**

## Performance

- **Duration:** 7 min
- **Started:** 2026-06-04T14:18:32Z
- **Completed:** 2026-06-04T14:25:38Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- Added a shared `Transport` interface for `ListTools`, `CallTool`, `Ping`, and `Close`.
- Added stdio `Ping` while preserving the existing `Open` entry point and fake-server tests.
- Added `OpenServer` routing from managed server metadata to stdio or Streamable HTTP.
- Implemented `OpenHTTP` with initialize, initialized notification, `tools/list`, `tools/call`, `ping`, session reset, auth headers, timeout/cancel behavior, JSON-RPC error handling, and minimal SSE response parsing.
- Covered the 2025-06-18 HTTP requirements with `httptest`: POST requests, `Accept: application/json, text/event-stream`, `Mcp-Session-Id`, and `MCP-Protocol-Version` after initialization.

## Task Commits

1. **Task 1: Introduce a transport interface and keep stdio green** - `405e84a1` (feat)
2. **Task 2: Implement minimal Streamable HTTP client** - `e256e5b1` (feat)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `internal/mcp/transport.go` - Common transport interface and `OpenServer` routing.
- `internal/mcp/transport_test.go` - Stdio transport interface and ping coverage.
- `internal/mcp/client.go` - Adds stdio `Ping`.
- `internal/mcp/client_test.go` - Adds fake-server `ping`.
- `internal/mcp/http_client.go` - Streamable HTTP client.
- `internal/mcp/http_client_test.go` - Session/protocol/auth/error/timeout coverage.

## Decisions Made

- Kept the existing stdio `protocolVersion` unchanged for compatibility, while HTTP negotiates `2025-06-18`.
- Implemented HTTP+SSE response parsing only for response delivery; long-lived GET streams and resumability stay out of this minimal client until needed.
- A 404 with an active session clears the local session and returns a session-expired error; callers can reopen to initialize a fresh session.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED tests could not be committed separately**
- **Found during:** Task 1 and Task 2 TDD setup
- **Issue:** The pre-commit hook runs `go vet ./...`, so intentionally non-compiling RED tests cannot be committed.
- **Fix:** Ran the RED command, verified missing `Transport`/`OpenHTTP` failures, then committed once green.
- **Verification:** `go test ./internal/mcp/ -run 'Test.*Stdio|Test.*Transport|TestOpen|TestHTTP|TestStreamable|TestSession|TestProtocol' -count=1`
- **Committed in:** `405e84a1`, `e256e5b1`

---

**Total deviations:** 1 auto-fixed (1 workflow constraint).
**Impact on plan:** No implementation scope change; TDD evidence was recorded without committing a hook-rejected red tree.

## Issues Encountered

- The HTTP cleanup path sends DELETE for sessions; the first test fixture initially treated every non-POST as a failure. The fixture now returns 405 for DELETE, matching the MCP allowance that servers may reject client session termination.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 3 can use the config/runtime metadata and `OpenServer` transport routing for status/doctor and trust/runtime gates.

## Verification

- `go test ./internal/mcp/ -run 'Test.*Stdio|Test.*Transport|TestOpen' -count=1` - passed
- `go test ./internal/mcp/ -run 'TestHTTP|TestStreamable|TestSession|TestProtocol' -count=1` - passed
- `go test ./internal/mcp/ -count=1` - passed

## Self-Check: PASSED

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
