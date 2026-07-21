---
phase: 39-idempotency-observability-pack
plan: 02
subsystem: runtime
tags: [go, idempotency, gateway, http, cli, scheduler, mcp, replay]

# Dependency graph
requires:
  - phase: 39-idempotency-observability-pack
    plan: 01
    provides: "Identity-scoped durable operation registry, typed decisions, replay records, and deterministic fingerprints"
  - phase: 35-toolgateway-policy-engine
    provides: "Mutating-tool policy gateway and append-only execution reservation/audit tuple"
provides:
  - "Immutable trusted operation context plus fail-closed HTTP, CLI, scheduler, approval/resume, and mutating-tool metadata inventories"
  - "Gateway ownership acquisition before effects, typed completed replay, conflict/progress/indeterminate denial, and terminal ambiguity handling"
  - "Aura-namespaced MCP operation metadata with read-only reconnect/reissue and zero automatic mutating replay after transport ambiguity"
affects: ["39-04 runtime telemetry", "39-06 operation retention"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Caller-stable operation identity is immutable context state; request, JSON-RPC, and tool-call IDs remain audit correlation only"
    - "Only a durable acquired decision authorizes a mutating effect; completed work decodes a bounded typed replay result"
    - "Transport retry policy is classification-aware: reads may reconnect and reissue, mutations become terminally ambiguous"

key-files:
  created:
    - internal/idempotency/context.go
    - internal/agui/idempotency_http.go
    - cmd/aura/idempotency.go
    - internal/gateway/idempotency_test.go
    - internal/agent/mcptools/bridge_reconnect_mutation_test.go
    - internal/runner/runner_resume_idempotency_test.go
  modified:
    - internal/gateway/reserve.go
    - internal/agent/llm_agent_retry.go
    - internal/agent/tools/spec.go
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/bridge_reconnect.go
    - internal/mcp/tool_methods.go
    - internal/mcp/http_client.go
    - internal/cron/dispatch.go
    - cmd/aura/chat_boot.go
    - cmd/aura/main.go

key-decisions:
  - "Operation context validates against the trusted identity principal and returns values, never mutable pointers; correlation is audit-only."
  - "The process-wide Plan 01 registry is wired at the gateway composition root and remains ahead of the existing internal reservation/effect boundary."
  - "Legacy scheduler owners that are not UUIDs map to the seeded local registry identity while the task's original delivery identity remains unchanged."
  - "MCP `_meta.aura` contains only operation key, finite scope, and fingerprint; remote/model metadata cannot inject trusted identity or override local ownership."

patterns-established:
  - "A mutating tools.Spec must declare a finite operation scope, canonical normalizer, and typed replay policy; owner coverage fails on omissions."
  - "Completion failures after a possible effect mark the acquired operation indeterminate before returning."

requirements-completed: []

coverage:
  - id: D1
    description: "Trusted operation propagation and explicit HTTP/CLI/scheduler/approval mutation contracts"
    verification:
      - kind: unit
        ref: "focused ingress suite across internal/idempotency, internal/agui, internal/runner, internal/cron, and cmd/aura"
        status: pass
    human_judgment: false
  - id: D2
    description: "Gateway registry decisions execute a same-operation effect once, replay the durable result, and fail closed on non-acquired or ambiguous outcomes"
    verification:
      - kind: unit
        ref: "internal/gateway/idempotency_test.go and internal/agent/llm_agent_retry_gateway_test.go"
        status: pass
      - kind: integration
        ref: "internal/gateway db_integration suite under Linux -race"
        status: pass
    human_judgment: false
  - id: D3
    description: "MCP envelopes preserve Aura operation metadata and ambiguous mutations perform zero reconnect/replay while reads retain reconnect behavior"
    verification:
      - kind: unit
        ref: "internal/mcp/client_test.go and internal/agent/mcptools/bridge_reconnect_mutation_test.go"
        status: pass
    human_judgment: false
  - id: D4
    description: "Operation changes preserve the existing multi-user isolation contract across live Postgres, Neo4j, Garage, and Authula configuration"
    verification:
      - kind: integration
        ref: "TestTwoIdentityCrossDeny and TestProvisionLoginIsolatedRun with all five integration tags"
        status: pass
    human_judgment: false

duration: 57min
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 02: End-to-End Operation Propagation Summary

**Trusted caller-stable operations now cross public mutation adapters, scheduler and resume paths, the tool gateway, and MCP transport so durable registry ownership—not per-attempt IDs—controls effect execution and replay.**

## Performance

- **Duration:** 57 min
- **Started:** 2026-07-21T11:01:20Z
- **Completed:** 2026-07-21T11:58:00Z
- **Tasks:** 3 TDD tasks
- **Files touched:** 41 (37 in the final tree; four temporary RED API tests were removed by GREEN)

## Accomplishments

- Added immutable operation context with trusted-principal validation, strict bounded key parsing, typed intent fingerprints, exhaustive HTTP/CLI mutation metadata inventories, approval/resume forwarding, generated CLI retry keys, and stable scheduler run operations.
- Extended every built-in mutating tool descriptor with Aura-owned scope/normalizer/replay metadata, wired the process-wide registry into the gateway, and made acquired/replay/conflict/in-progress/indeterminate/outage decisions govern policy reservation and execution.
- Added bounded durable tool-result completion and terminal indeterminate handling for post-effect failures, proving one effect across retries even when request and tool-call audit identifiers differ.
- Namespaced immutable operation metadata into both stdio and HTTP MCP envelopes, prevented model `_meta` override, and disabled reconnect/reissue after ambiguous mutating sends while preserving classified read-only recovery.
- Closed owned-package coverage above 85% and passed the live two-identity cross-deny acceptance test against Postgres, Neo4j, Garage, and Authula configuration.

## Task Commits

Each TDD task was committed RED then GREEN:

1. **Task 1: Trusted operation propagation and ingress contracts**
   - RED - `75f521df7` (test): failing operation-context, HTTP/CLI inventory, resume, and scheduler contracts.
   - GREEN - `2dad6b205` (feat): trusted ingress context, strict adapter metadata, CLI key construction, resume propagation, and stable scheduled operations.
2. **Task 2: Registry ownership at gateway and mutating-tool coverage**
   - RED - `5d631cf83` (test): failing registry-decision, retry, completion-ambiguity, and owner-metadata contracts.
   - GREEN - `934b58d0f` (feat): gateway Begin/replay/complete/indeterminate lifecycle plus built-in tool operation descriptors.
3. **Task 3: MCP operation metadata and reconnect policy**
   - RED - `fd63e16e2` (test): failing stdio/HTTP wire metadata and mutation reconnect/replay contracts.
   - GREEN - `5ed18c402` (feat): Aura `_meta` propagation and classification-aware transport recovery.

Corrective commits:

- `8caad3542` (fix): preserve legacy scheduler fixtures/owners by mapping non-UUID registry ownership to the seeded local identity.
- `02356163b` (test): raise registry/gateway statement coverage with lifecycle and edge-decision cases.

## Files Created/Modified

- `internal/idempotency/context.go` and tests - immutable trusted operation carrier, identity match, and value-copy extraction.
- `internal/agui/idempotency_http.go` plus mutation coverage - strict header/key policy, finite route metadata, decision-to-HTTP projection, and source-scanned route completeness.
- `cmd/aura/idempotency.go`, `main.go`, and command coverage - normalized CLI mutation inventory, explicit/generated operation keys, stable fingerprints, and dispatch context.
- `internal/cron/dispatch.go` and resume tests - logical task/run operation identity and preservation through approval/runner boundaries.
- `internal/agent/tools/spec.go` and built-in tools - runtime-only operation scope, canonical normalizer, and replay policy metadata.
- `internal/gateway/{gateway,reserve,decide}.go` - shared registry seam, pre-effect acquisition, typed replay projection, bounded completion, and indeterminate transitions.
- `internal/agent/llm_agent_retry.go` - execute only acquired work, return completed replay without invoking the tool, and terminally record ambiguous failure.
- `internal/agent/mcptools/*` and `internal/mcp/*` - bridged mutation metadata, Aura wire envelope, and no reconnect/reissue after a mutating transport ambiguity.
- `cmd/aura/chat_boot.go` - process-wide PostgreSQL operation registry wiring.

## Decisions Made

- Correlation stays audit-only and may vary per attempt; it never participates in public operation ownership or normalized intent.
- Gateway registry acquisition precedes the existing append-only internal reservation and external effect. A completed replay bypasses both policy side effects and tool execution.
- Mutating tools require exact agreement among trusted context scope, tool descriptor metadata, and canonical typed-argument fingerprint. Any mismatch denies execution.
- MCP remote metadata is correlation material only: no trusted identity crosses the protocol boundary, and model-controlled `arguments._meta` cannot replace outer Aura metadata.
- A tracked mutating MCP operation receives one send attempt. If the transport becomes ambiguous, the caller gets a typed transport error and the operation is not automatically reissued; classified reads keep session recovery.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Wired the durable registry at the production gateway composition root**
- **Found during:** Task 2 full-runtime review.
- **Issue:** The requested gateway lifecycle existed but no process-wide Plan 01 store was attached, which would leave production on the legacy reservation alone.
- **Fix:** Constructed one `idempotency.Store` from the live pool in `cmd/aura/chat_boot.go` and attached it through `SetOperationRegistry`.
- **Verification:** Gateway unit/race suites and real tagged PostgreSQL integration pass.
- **Committed in:** `934b58d0f`.

**2. [Rule 1 - Bug] Preserved historical scheduler identities without weakening registry UUID ownership**
- **Found during:** Full touched-package test run after Task 3.
- **Issue:** Existing local/test scheduled tasks use legacy owner strings such as `local`; strict operation validation correctly rejected them before dispatch.
- **Fix:** Valid UUID owners remain unchanged; legacy/local sentinels use the seeded local UUID only for registry ownership while the original `Task.IdentityID` still drives delivery.
- **Verification:** Full cron/cmd suite and focused scheduled-operation tests pass.
- **Committed in:** `8caad3542`.

**3. [Rule 2 - Missing Critical] Added lifecycle coverage needed by the phase floor**
- **Found during:** Plan-level coverage gate.
- **Issue:** Correct gateway/idempotency behavior passed but branch coverage left the two newly owned packages below the required statement floor.
- **Fix:** Added focused context edge, replay decoding/bounding, unavailable registry, completion, indeterminate, and nil-safe lifecycle tests.
- **Verification:** `internal/idempotency` 87.0%; `internal/gateway` 89.1%.
- **Committed in:** `02356163b`.

---

**Total deviations:** 3 auto-fixed (2 missing-critical, 1 compatibility bug)
**Impact on plan:** All changes are within the operation-lifecycle boundary and add no new dependency, migration, protocol authority, or scheduler cadence behavior.

## Issues Encountered

- Native Windows could not run Go's race detector because no C compiler was configured. A disposable matching Go 1.26.5 Linux toolchain under WSL ran every requested race package and the tagged gateway/idempotency PostgreSQL suites; it was removed afterward.
- The first idempotency integration attempt correctly refused the live `aura` database. A verified-new `aura_idempotency_3902` database was migrated and tested, then dropped and verified absent.
- The live two-identity gate initially found local test-infrastructure drift: Garage's admin API was not host-published and the global `mcp-neo4j-cypher` 0.6.0 installation had been combined with incompatible FastMCP 3.4.4. A disposable port forward plus isolated environment resolved the documented `<2.14` dependency, the gate passed, and all temporary state was removed.

## Threat Surface Scan

- Operation keys reject missing, duplicate, blank, control-character, and over-limit values; identity and finite scope come only from trusted runtime context.
- Typed fingerprints are computed from Aura-owned normalizers and canonical arguments; raw normalized payloads, identity, and keys are absent from public/model-visible errors.
- Only a durable acquired decision can cross the effect boundary. Conflict, in-progress, indeterminate, malformed replay, and registry outage execute no tool.
- Post-effect completion failure is terminally marked indeterminate, preventing automatic duplicate effects.
- MCP metadata contains no trusted identity or raw payload, and mutating transport ambiguity performs zero reconnect/reissue.

## Known Stubs

None. The temporary RED compile-contract files were deleted in their corresponding GREEN commits; a scan found no new TODO, FIXME, HACK, panic, or not-implemented marker on the final operation surface.

## Verification

- Focused Task 1, Task 2, and Task 3 commands all pass in a fresh post-cleanup run.
- Full touched surface: `go test ./internal/gateway ./internal/agent/... ./internal/mcp ./internal/cron ./internal/agui ./cmd/aura` passes.
- Linux race: `go test -race ./internal/idempotency ./internal/gateway ./internal/agent/... ./internal/mcp ./internal/cron ./internal/agui ./cmd/aura` passes.
- Tagged database race: `go test -tags db_integration -race -count=1 -p 1 ./internal/gateway` and `./internal/idempotency` pass.
- Full live isolation: `TestTwoIdentityCrossDeny` and `TestProvisionLoginIsolatedRun` pass with `db_integration`, `neo4j_integration`, `garage_integration`, `authula_integration`, and `musr_e2e`.
- `go vet` on every touched package, `go build ./...`, and `git diff --check` pass.
- Statement coverage: idempotency 87.0%, gateway 89.1%, agent 88.3%, tools 86.2%, mcptools 92.0%, MCP 91.6%, runner 88.5%.

## User Setup Required

None. The registry uses Plan 01's existing migration and database configuration; no new environment variables or persistent services were introduced.

## Next Phase Readiness

- Plan 39-04 can attach one trace/metric lifecycle to stable operation decisions without treating per-attempt request IDs as ownership.
- Plan 39-06 can expire bounded replay material through the Plan 01 store while keeping public operation and audit facts.
- No blockers.

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: `internal/idempotency/context.go`
- FOUND: `internal/agui/idempotency_http.go`
- FOUND: `cmd/aura/idempotency.go`
- FOUND: `internal/gateway/reserve.go`
- FOUND: `internal/gateway/idempotency_test.go`
- FOUND: `internal/agent/mcptools/bridge_reconnect_mutation_test.go`
- FOUND: `internal/mcp/tool_methods.go`
- FOUND: `internal/runner/runner_resume_idempotency_test.go`

**Commits verified to exist:**
- FOUND: `75f521df7` (Task 1 RED)
- FOUND: `2dad6b205` (Task 1 GREEN)
- FOUND: `5d631cf83` (Task 2 RED)
- FOUND: `934b58d0f` (Task 2 GREEN)
- FOUND: `fd63e16e2` (Task 3 RED)
- FOUND: `5ed18c402` (Task 3 GREEN)
- FOUND: `8caad3542` (scheduler compatibility fix)
- FOUND: `02356163b` (coverage hardening)
