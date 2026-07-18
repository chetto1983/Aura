---
status: testing
phase: 38-mcp-governance-hardening
source: [38-VERIFICATION.md]
started: 2026-07-18T13:01:29Z
updated: 2026-07-18T13:01:29Z
---

## Current Test

number: 1
name: Mount-exactly-at-deadline determinism (AURA_MCP_MOUNT_TIMEOUT)
expected: |
  Mount an MCP server whose handshake completes at exactly the AURA_MCP_MOUNT_TIMEOUT
  deadline (inject a controllable delay == mountTimeout, repeated under load/jitter, ideally
  under `go test -race`). Result must be deterministically mounted-XOR-dropped, never both:
  exactly one of (a) mounted and usable, or (b) dropped with a WARN and its subprocess reaped.
  Never a registry holding a handle to a server whose subprocess was also killed, and never a hang.
awaiting: user response

## Tests

### 1. Mount-exactly-at-deadline determinism
expected: Deterministically mounted-XOR-dropped at the exact AURA_MCP_MOUNT_TIMEOUT instant; no double-mount, no half-mounted registry entry, no hang. (38-05 `backstop` truth; TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives proves the fast-success and clearly-hung ends but not the exact-boundary instant.)
result: [pending]

### 2. Aggregate-shutdown-deadline-exact determinism
expected: closeMCPServers where one closer completes at the exact instant AURA_MCP_SHUTDOWN_TIMEOUT's aggregate context fires (repeat under scheduler jitter / `-race`) — the completion is not double-counted (errgroup does not race a completed-but-still-cancelling closer into being both done and abandoned) and no goroutine leaks past the aggregate deadline. (38-05 `backstop`; TestCloseMCPServers_AggregateDeadlineAbandonsStragglers + TestCloseMCPServers_ConcurrentBoundedShutdown prove the abandon + bounded-fanout cases, not the exact instant.)
result: [pending]

### 3. Probe-response-exactly-at-deadline determinism
expected: An HTTP MCP server whose tools/list response arrives at (or within microseconds of) AURA_MCP_PROBE_TIMEOUT firing (repeat under load / `-race`) — mcp.ProbeServer / writeRuntimeCheck / doctorProbeMCPServers returns a single deterministic verdict (OK=true or OK=false), never a data race between the successful response and the timeout cancellation, and never a leaked dialing goroutine. (38-06 `backstop` Probe #19; TestWriteRuntimeCheckBoundedByProbeTimeout + TestDoctorProbeMCPServersBoundedByProbeTimeout prove boundedness, not the exact instant.)
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps

_None discovered. All three items are planner-authored `backstop` timing-race truths the verifier abstained on (no counter-evidence found), not defects. The natural way to exercise them is the deferred WSL `go test -race` full-matrix run (goleak-clean) — 38-05's shutdown path was already run under `-race`+goleak in WSL during execution; a phase-close `-race` re-run over the mount + probe deadline paths closes the remaining two by exercising the same race-detector coverage._
