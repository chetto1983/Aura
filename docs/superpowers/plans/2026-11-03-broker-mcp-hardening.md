# Broker MCP hardening implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or
> superpowers:subagent-driven-development task by task. User has authorized execution.

**Goal:** Fix the observed broker integration defects and make cached startup reliable and measured.

**Architecture:** Retain existing tools, uv cache, MCP SDK sessions and invocation ledger. Repair
metadata, validation and lifecycle propagation at their existing ownership boundaries.

**Tech Stack:** Go/MCP SDK/pgx, Python/MCP/Pydantic/uv, WSL, real lab VM.

**Spec:** `docs/superpowers/specs/2026-11-03-broker-mcp-hardening-design.md`

## Global Constraints

- Existing main branches; preserve concurrent work and stage only owned paths.
- No new cache manager/protocol/migration, no cache deletion or weakened identity isolation.
- TDD for behavioral fixes; WSL for Aura tests; <=600 LOC on touched Go files.
- Record measured live evidence and explicit limits; push and check CI after verified commits.

## Review Focus

- Invalid finite/nonfinite numeric inputs and short history before expensive provider I/O.
- Local audit persistence annotations without misrepresenting hook subprocess effects.
- Worker failure/cancellation timing and argument identity despite first-writer ledger conflicts.
- Box redial budget vs connection lifetime, user cancellation and mutation replay.
- Cached uv launch with unavailable network and separate worker/identity process sessions.

### Task 1: Broker contract

**Files:** Existing broker server, analysis/fundamentals/model helpers and tests in the broker repo.
**Interfaces:** Existing public tools and result fields remain; additive diagnostics/metadata.

- [ ] Add/run failing tests for local-write annotations, invalid parameters, insufficient data,
  missing-metric explanations and consistent metadata.
- [ ] Implement with existing MCP/Pydantic/catalog APIs; run full offline Python suite and lint.
- [ ] Commit scoped broker changes; prepare release metadata using existing packaging pattern.

### Task 2: Worker invocation evidence

**Files:** `internal/gateway/decide.go`, `delegated_reservation.go`, reservation context and tests.
**Interfaces:** Existing ledger rows preserve actual args, start/end and duration; no new schema.

- [ ] Reproduce absent read args and zero delegated duration in failing tests.
- [ ] Preserve existing context metadata through gateway start/end; test errors and cancellations.
- [ ] Run scoped race/integration plus repository-required vet/build checks; commit only owned paths.

### Task 3: Cached startup and reconnect

**Files:** Existing MCP mount/supervisor and box launcher code/tests, only as measurement requires.
**Interfaces:** Existing session-open seam honors configured box init budget and cache lifecycle.

- [ ] Measure uv cache directories/volume mounts, cached launch and Python imports on the VM.
- [ ] Add/run failing regression tests for the measured cause and slow reconnect budget.
- [ ] Apply minimal existing-runtime fixes; run WSL unit/race/vet/build and scope gates.
- [ ] Review combined changes, commit, push and check CI; validate real agent calls on VM.
- [ ] Record before/after measurements in dated verification and amend PRD from evidence.

## Execution decisions

- The operator's "procedi" approves the reviewed improvements; no repeated design approval.
- Task 1 and Task 2 have independent file ownership; Task 3 owns deployment and cache probes.
- Baseline: Aura `9af034136`, broker `9886eb3` (remote additionally changes CI only).
- Ruling: preserve `openWorld=true` and non-idempotence for fresh Yahoo-backed audit records;
  a false closed-world hint would misstate behavior. Separate local-audit metadata and retain
  conservative fallback/standing approval rather than add a broker-name trust exception.
  Cost if wrong: the operator still sees the conservative tier in a new unapproved installation.
- Cache measurement: existing uv/Python volumes are present. Tag resolution adds network cost;
  immutable commit works offline. Eager numerical/chart imports account for initial server time.
  Use native lazy imports in broker tool functions and full-commit packaging pins.
