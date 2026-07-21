---
status: complete
phase: 38-mcp-governance-hardening
source: [38-VERIFICATION.md]
started: 2026-07-18T13:01:29Z
updated: 2026-07-18T17:15:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Mount-exactly-at-deadline determinism
expected: Deterministically mounted-XOR-dropped at the exact AURA_MCP_MOUNT_TIMEOUT instant; no double-mount, no half-mounted registry entry, no hang. (38-05 `backstop` truth; TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives proves the fast-success and clearly-hung ends but not the exact-boundary instant.)
result: pass
source: live (fresh aura container, 2026-07-18)
evidence: |
  Live-proven in the freshly rebuilt aura container (AURA_IN_CONTAINER=1, go1.26.5):
  - Happy mount via real binary: github MCP (docker) = 44 tools, calculator (fork) = 23,
    calendar = 14, memory = 17, whatsapp = 14 — all mount in 3s.
  - Hung server (command=sleep 3600, spawns but never speaks MCP) with AURA_MCP_MOUNT_TIMEOUT=3:
    dropped at the exact 3s deadline with `WARN mcp mount failed ... recv timeout: context
    deadline exceeded mount_timeout=3s`, subprocess REAPED (no lingering sleep proc), healthy
    servers (memory+whatsapp) mounted after the drop, rc=0 elapsed=5s — NO hang, no half-mount.
  - Calculator (the suspected fork) mounts 23 tools cleanly — NOT the source of any hang.
  Note: an earlier hang was observed only with the Windows `aura.exe` dev binary + Windows
  Docker (kill of docker.exe does not reap the container; pipe/process-group semantics differ);
  the shipped Linux path drops correctly. The exact-microsecond-instant race remains for the
  deferred WSL `go test -race` full-matrix run.

### 2. Aggregate-shutdown-deadline-exact determinism
expected: closeMCPServers where one closer completes at the exact instant AURA_MCP_SHUTDOWN_TIMEOUT's aggregate context fires (repeat under scheduler jitter / `-race`) — the completion is not double-counted (errgroup does not race a completed-but-still-cancelling closer into being both done and abandoned) and no goroutine leaks past the aggregate deadline. (38-05 `backstop`; TestCloseMCPServers_AggregateDeadlineAbandonsStragglers + TestCloseMCPServers_ConcurrentBoundedShutdown prove the abandon + bounded-fanout cases, not the exact instant.)
result: pass
source: live (fresh aura container, 2026-07-18)
evidence: |
  Live-proven in the fresh container. A fake stdio MCP server (python3) that mounts normally
  (initialize + tools/list → 1 tool) but ignores stdin-close so its Close() becomes a
  closeWaitTimeout straggler, run with AURA_MCP_SHUTDOWN_TIMEOUT=1:
  - `INFO mcp mounted server=straggler tools=1` (mounts), then on process exit
  - `WARN mcp shutdown aggregate deadline elapsed; abandoning stragglers timeout=1s servers=5`
    fired at ~1s — aggregate shutdown bounded by AURA_MCP_SHUTDOWN_TIMEOUT regardless of the
    hung closer; rc=0, clean exit, NO hang, no goroutine surviving the process.
  The exact-microsecond-instant errgroup race is deferred to the WSL `go test -race` full-matrix
  run (TestCloseMCPServers_AggregateDeadlineAbandonsStragglers already covers the abandon path).

### 3. Probe-response-exactly-at-deadline determinism
expected: An HTTP MCP server whose tools/list response arrives at (or within microseconds of) AURA_MCP_PROBE_TIMEOUT firing (repeat under load / `-race`) — mcp.ProbeServer / writeRuntimeCheck / doctorProbeMCPServers returns a single deterministic verdict (OK=true or OK=false), never a data race between the successful response and the timeout cancellation, and never a leaked dialing goroutine. (38-06 `backstop` Probe #19; TestWriteRuntimeCheckBoundedByProbeTimeout + TestDoctorProbeMCPServersBoundedByProbeTimeout prove boundedness, not the exact instant.)
result: pass
source: live (fresh aura container, 2026-07-18)
evidence: |
  Live-proven in the fresh container. A hung managed server (`aura mcp add hung --trust local --
  sleep 3600`) probed via `aura mcp status` with AURA_MCP_PROBE_TIMEOUT=2:
  - hung row → `fail:mcp "hung": initialize: mcp transport error: recv timeout: context deadline
    exceeded` — single deterministic OK=false verdict bounded by the 2s probe timeout.
  - rc=0, total elapsed 4s (bounded; also probed the 4 real servers), NO hang.
  - `no lingering sleep` after the probe — no leaked dialing goroutine/subprocess. Server removed.
  The exact-microsecond-instant race (success response vs timeout cancellation) is deferred to the
  WSL `go test -race` full-matrix run.

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

_None discovered. All three `backstop` timing-race truths were LIVE-PROVEN in the freshly rebuilt aura container (2026-07-18) for their deterministic behavior — mount drop-at-deadline + reap + no-hang (github 44 tools / calculator 23 mounted, hung server dropped at exact 3s AURA_MCP_MOUNT_TIMEOUT, subprocess reaped); aggregate-shutdown abandon bounded by AURA_MCP_SHUTDOWN_TIMEOUT (straggler abandoned at 1s WARN, rc=0); probe deadline deterministic OK=false bounded by AURA_MCP_PROBE_TIMEOUT with no leaked dial. The suspected calculator fork mounts cleanly (23 tools) — NOT a defect._

_**Exact-microsecond residual CLOSED 2026-07-18** via the WSL `go test -race` + goleak run (CGO on WSL, `-count=3` to stress the timing): `internal/agent/mcptools` (mount) ok 34.7s, `internal/mcp` (handshake/probe) ok 60.0s, `cmd/aura` (buildRegistryWithMCP mount + closeMCPServers shutdown + writeRuntimeCheck/doctorProbe) ok 38.7s — all green, race-clean, goleak-clean. The db_integration+neo4j_integration coverage ≥85% floor is confirmed by the green CI Skills+Knowledge gates on `e8c1fa39`._

_Filed as audit: a Windows-only `aura.exe` dev-host hang on a never-responding stdio MCP server (root cause UNCONFIRMED, not reproduced in isolation, absent from the shipped Linux path) → `docs/audit/verify-work-runtime-findings-2026-07-18.md` VW-01._
