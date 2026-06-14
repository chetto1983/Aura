---
phase: 17-packaging-distribution
plan: 03
subsystem: mcp-manager
tags: [mcp, docker-runtime, whatsapp, streamable-http, tdd]
requires: [17-01, 17-02]
provides:
  - In-container docker-runtime guard
  - WhatsApp streamable-HTTP sibling recipe
  - Port-validation tests for AURA_WHATSAPP_MCP_PORT
affects: [internal-mcp-manager, managed-mcp-catalog, docker-aura-runtime]
tech-stack:
  added: []
  patterns:
    - Sentinel error wrapping for actionable runtime failures
    - Loopback-only streamable-HTTP MCP sibling recipe
key-files:
  created:
    - internal/mcp/manager/runtime_guard_test.go
    - .planning/phases/17-packaging-distribution/17-03-SUMMARY.md
  modified:
    - internal/mcp/manager/runtime.go
    - internal/mcp/manager/catalog.go
    - internal/mcp/manager/catalog_test.go
key-decisions:
  - Docker and Docker Gateway MCP runtimes fail fast in-box when AURA_IN_CONTAINER=1.
  - WhatsApp is no longer launched via wsl.exe; it mounts as a loopback streamable-HTTP sibling on port 8092.
  - AURA_WHATSAPP_MCP_PORT is validated as a numeric TCP port before interpolation.
requirements-completed: [OPS-01]
metrics:
  duration: ~5min
  tasks: 2
  files-modified: 4
  completed: 2026-06-14
---

# Phase 17 Plan 03: MCP Boundary Summary

`runtime.kind=docker` MCP servers now fail with an actionable compose-sibling error inside the Aura container, and the WhatsApp recipe now points to a streamable-HTTP sibling endpoint instead of `wsl.exe`.

## Performance

- Started: 2026-06-14T12:38:10Z
- Completed: 2026-06-14T12:42:40Z
- Duration: ~5 min
- Tasks completed: 2
- Files changed: 4

## TDD Evidence

- RED: `go test ./internal/mcp/manager -run "TestRuntimeGuard|TestCatalogWhatsapp" -v` failed with undefined `errDockerRuntimeInContainer` and `whatsappRecipeURL`.
- GREEN: the same focused test command passed after the guard and helper were implemented.

## Accomplishments

- Added `errDockerRuntimeInContainer` and guarded `RuntimeDocker` / `RuntimeDockerGateway` before any `docker run` or gateway command line is built.
- Added table-driven guard tests covering docker runtime, docker gateway, sentinel wrapping, actionable message text, and off-box behavior.
- Rewrote the WhatsApp catalog entry to `mcp.ServerTypeStreamableHTTP` with `http://127.0.0.1:8092/mcp/`, no launch command, and no WSL summary text.
- Added `whatsappRecipeURL()` validation mirroring `memoryRecipeURL()` to block userinfo-retarget values such as `8092@evil.example`.

## Task Commits

| Task | Commit | Summary |
| --- | --- | --- |
| 1 | bc0d3694 | Guarded docker MCP runtimes when Aura is running in-container. |
| 2 | 4d9c5e02 | Pointed WhatsApp recipe at the sibling streamable-HTTP endpoint. |

## Verification Evidence

- `go test ./internal/mcp/manager -v` passed.
- `go test -race ./internal/mcp/manager` passed.
- `go test ./internal/mcp/manager -cover` reported 99.1% statement coverage.
- `go vet ./internal/mcp/manager` passed.
- `go build ./...` passed.
- `go test ./internal/mcp/ -run TestWhatsApp -v` passed with no active tests.
- `go test -tags whatsapp_integration ./internal/mcp -run TestWhatsAppServerLive -v` compiled and skipped because `AURA_MCP_WHATSAPP_SERVER_JSON` is not configured.
- `catalog.go` contains no `wsl.exe`.

## Deviations

- The live WhatsApp sibling probe remains Manual-Only until plan `17-06` adds the sibling service.

## Issues Encountered

None.

## User Setup Required

None.

## Next Phase Readiness

Plan `17-06` can add the WhatsApp sibling on the `8092` loopback port now referenced by the catalog.

## Self-Check: PASSED
