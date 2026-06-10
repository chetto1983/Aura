---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 09
subsystem: mcp
tags: [mcp, reconnect, tool-search, stderr, bounded-buffer]
requires: []
provides:
  - lazy reconnect-on-use for stdio MCP bridged tools
  - refreshed bridged tool descriptions after reconnect
  - invalidatable ToolSearch BM25 cache
  - shared bounded stderr buffer for MCP sidecars
affects: [mcp-bridge, mcp-client, knowledge-client, tool-search]
tech-stack:
  added: []
  patterns: [typed transport errors, reconnect-on-use, bounded ring buffer]
key-files:
  created:
    - internal/agent/mcptools/bridge_reconnect.go
    - internal/boundedbuffer/buffer.go
  modified:
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/mount.go
    - internal/agent/mcptools/bridge_test.go
    - internal/agent/tools/search.go
    - internal/mcp/client.go
    - internal/mcp/client_errors_test.go
    - internal/mcp/client_test.go
    - internal/knowledge/client.go
    - internal/knowledge/client_paths_test.go
key-decisions:
  - "Classify broken stdio pipes with mcp.ErrTransport and mcp.IsTransportError instead of matching error strings."
  - "Wrap only stdio MCP clients in reconnectingServer; streamable HTTP transports keep their existing direct mount path."
  - "Extract a shared boundedbuffer.Buffer so MCP and knowledge clients use one capped stderr implementation."
patterns-established:
  - "A reconnect refreshes tracked bridged tool specs and invalidates ToolSearch's BM25 snapshot."
requirements-completed: [H10, M-j]
duration: 45 min
completed: 2026-06-10
---

# Phase 19 Plan 09: MCP Reconnect and Bounded Stderr Summary

**Dead stdio MCP tools now reconnect on next use, and sidecar stderr retention is bounded.**

## Performance

- **Duration:** 45 min
- **Completed:** 2026-06-10
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Added `mcp.ErrTransport` / `mcp.IsTransportError` and wrapped send/recv/notify pipe failures with the typed sentinel.
- Added `reconnectingServer`, which closes a dead stdio client, reopens it with the same `mcp.ServerConfig`, refreshes `tools/list`, and retries once.
- Made bridged tool specs atomically refreshable and invalidated `tool_search` after a refreshed `tools/list`.
- Updated stdio MCP mounts to use the reconnect wrapper while leaving streamable HTTP mounts unchanged.
- Replaced duplicated unbounded stderr buffers with `internal/boundedbuffer.Buffer`, capped at 4096 bytes by default.
- Added regressions for reconnect retry, second-failure inline error, refreshed BM25 search, transport classification, and bounded stderr retention.

## Task Commits

1. **Task 1 and 2: Reconnect MCP tools and bound stderr buffers** - `b8f9460a` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/agent/mcptools/bridge_reconnect.go` - reconnecting stdio server wrapper.
- `internal/boundedbuffer/buffer.go` - shared mutex-guarded bounded ring writer.
- `internal/agent/mcptools/bridge.go` - refreshable bridged specs and tool_search invalidation hook.
- `internal/agent/mcptools/mount.go` - stdio mounts wrap with reconnectingServer.
- `internal/agent/tools/search.go` - invalidatable BM25 index snapshot.
- `internal/mcp/client.go` and `internal/knowledge/client.go` - typed transport errors and bounded stderr buffers.

## Decisions Made

No background supervisor, ping ticker, or busy loop was added. Recovery happens only on a tool/list call that observes a typed transport failure.

## Deviations from Plan

The composition-root reconnect wiring lives in `internal/agent/mcptools/mount.go`, which is the actual mount seam used by `cmd/aura`, rather than editing `cmd/aura/serve_channels.go` directly.

## Issues Encountered

None.

## Verification

- `go test -run 'TestReconnect|TestBridge|TestBridgedTool' ./internal/agent/mcptools/` - passed.
- `go test -run 'TestSafeBuffer|TestStderrTail|TestRing|TestBounded|TestStdioRoundtripSendErrorIncludesStderrTail|TestStdioReadResponseRecvEOF' ./internal/mcp/ ./internal/knowledge/` - passed.
- `go build ./internal/agent/mcptools/ ./internal/mcp/ ./internal/knowledge/ ./cmd/aura` - passed.
- `go vet ./internal/agent/mcptools/ ./internal/mcp/ ./internal/knowledge/ ./cmd/aura` - passed.
- `go test ./internal/agent/mcptools/ ./internal/mcp/ ./internal/knowledge/` - passed.
- `go test -race ./internal/agent/mcptools/ ./internal/mcp/ ./internal/knowledge/` - passed.
- `go test ./internal/agent/tools/` and `go test -race ./internal/agent/tools/` - passed.

## User Setup Required

None - no new configuration or external package required.

## Next Phase Readiness

Wave 2 plan 19-05 can rely on MCP self-send tools recovering from dead stdio transports, and all MCP/knowledge sidecars now retain bounded stderr context.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
