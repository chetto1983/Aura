# P2 Boundary and Lifecycle Validation - 2026-06-11

## Scope

Closed this phase:

- R-18: `send_file` workspace fence.
- R-19: destructive shell pattern gate through `ask_user` resume approval.
- R-20/R-21/R-22/R-25: MCP mutating defaults, untrusted description/summary framing, body caps, and no replay after transport failure.
- R-23/R-24/R-43: sidecar bounded paging, strict id grammar, opaque spill ids, and owner-only sidecar permissions.
- R-35: background shell cap, pruning, and shutdown.

## Validation

Passed:

- `go test ./internal/agent/tools ./internal/agent/mcptools ./internal/mcp ./cmd/aura -count=1`
- `go test -race ./internal/agent/tools ./internal/agent/mcptools ./internal/mcp ./cmd/aura -count=1`
- `go test ./internal/agent ./internal/llm ./internal/runner ./internal/conversations ./internal/agui ./internal/cron ./internal/agent/workflow ./internal/agent/tools ./internal/skills ./internal/mcp ./cmd/aura -count=1`

Coverage gate:

- `D:\tmp\w64devkit\bin\bash.exe scripts/coverage_gate.sh`
- Result: FAIL, `total: (statements) 77.2%`; `FAIL: owned coverage 77.2% < 85%`.

The coverage-gate test run itself completed, but the repository-wide owned coverage floor is currently below the configured threshold. The focused package tests and race tests for this phase passed.

## Residual Risk

Operations/observability work remains for the next phase: structured logs, disk TTL sweep, SIGTERM conversational drain, Windows CI, OTel exporter honesty, and Prometheus metrics.

The global coverage floor also needs a separate remediation pass before the coverage gate can be reported green again.
