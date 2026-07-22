---
phase: 39-idempotency-observability-pack
plan: 04
subsystem: observability
tags: [go, opentelemetry, prometheus, otlp, metrics, tracing, cardinality]

# Dependency graph
requires:
  - phase: 39-idempotency-observability-pack
    plan: 02
    provides: "Trusted operation propagation across runner, scheduler, tools, and MCP boundaries"
  - phase: 39-idempotency-observability-pack
    plan: 03
    provides: "Joined serve/listener/scheduler lifecycle and truthful readiness seams"
provides:
  - "One OpenTelemetry resource lifecycle for traces plus Prometheus and optional OTLP metrics"
  - "Dedicated loopback Prometheus registry/listener absent from public AG-UI routes"
  - "Finite metric catalog and normalized six-key attribute vocabulary with explicit histogram buckets"
  - "Exactly-once bounded metrics/spans for LLM, tool, MCP, pause/resume, DB, scheduler, listener, idempotency, and retention boundaries"
affects: ["39-05 retention", "39-06 learning observability", "39-07 dashboards and alerts", "40 legacy metric removal"]

# Tech tracking
tech-stack:
  added:
    - "go.opentelemetry.io/otel/exporters/prometheus v0.66.0"
    - "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.44.0"
    - "go.opentelemetry.io/otel/sdk/metric v1.44.0"
  patterns:
    - "Catalog-owned OTel instruments with finite normalization and one canonical Prometheus projection"
    - "Scoped boundary start/end handles with sync.Once terminal emission and panic-safe completion"
    - "Private metrics listener joined to the root serve lifecycle"

key-files:
  created:
    - internal/obs/meter.go
    - internal/obs/boundary.go
    - internal/cron/observability.go
    - internal/db/observability.go
    - internal/mcp/observability.go
    - internal/redact/string.go
  modified:
    - internal/obs/init.go
    - internal/obs/catalog.go
    - internal/agent/metrics.go
    - internal/agent/llm_agent.go
    - internal/runner/runner_resume.go
    - internal/mcp/client.go
    - internal/cron/scheduler.go
    - internal/db/db.go
    - cmd/aura/serve.go

key-decisions:
  - "One sdkmetric.MeterProvider owns independent Prometheus and optional OTLP readers while sharing the trace resource and reverse-order idempotent shutdown stack."
  - "Metric dimensions are restricted to operation, tool_class, transport, outcome, error_class, and state; arbitrary inputs normalize to finite enums or other."
  - "Runtime instrumentation lives at semantic owners, with DB query coverage supplied by a pgx QueryTracer that never reads or records SQL text."
  - "Legacy expvar metrics remain a named projection-only compatibility adapter through Phase 40; OTel owns canonical Prometheus collection."

patterns-established:
  - "Boundary.End and Boundary.PanicSafe may be invoked repeatedly, but sync.Once emits one count/duration and restores in-flight to zero exactly once."
  - "New idempotency/retention/learning packages can emit catalog signals through obs.NewGlobalBoundary without importing application packages."
  - "Global boundaries resolve the current tracer at Start so test/runtime provider replacement preserves trace parenting."

requirements-completed: [OBS-03]

coverage:
  - id: D1
    description: "A single OTel runtime fans one measurement to dedicated Prometheus and optional OTLP readers and shuts trace/metric providers down safely."
    requirement: OBS-03
    verification:
      - kind: unit
        ref: "internal/obs/meter_test.go#TestMeterProvider* and internal/obs/init_test.go#Test*Shutdown*"
        status: pass
      - kind: other
        ref: "go test -race ./internal/obs ./cmd/aura under WSL"
        status: pass
    human_judgment: false
  - id: D2
    description: "Prometheus scraping uses only a dedicated registry and loopback-private listener, never public AG-UI routes or prometheus.DefaultRegisterer."
    requirement: OBS-03
    verification:
      - kind: integration
        ref: "internal/obs/meter_test.go#TestPrometheus* and cmd/aura/serve_observability_test.go#Test*Prometheus*"
        status: pass
    human_judgment: false
  - id: D3
    description: "The telemetry catalog has stable descriptors, explicit buckets, finite attributes, and one canonical owner per Prometheus family."
    requirement: OBS-03
    verification:
      - kind: unit
        ref: "internal/obs/catalog_test.go and internal/agent/metrics_observability_test.go"
        status: pass
      - kind: other
        ref: "FuzzNormalizeAttribute and FuzzClassifyTool (88k+ executions during Task 2)"
        status: pass
    human_judgment: false
  - id: D4
    description: "LLM, tool, MCP, pause/resume, DB, scheduler, listener, idempotency, and retention boundaries emit exactly-once bounded spans and metrics across success/error/cancel/panic paths."
    requirement: OBS-03
    verification:
      - kind: unit
        ref: "internal/obs/boundary_test.go#TestBoundaryRecordsSuccessErrorCancelAndPanicExactlyOnce"
        status: pass
      - kind: other
        ref: "go test -race ./internal/obs ./internal/agent/... ./internal/mcp ./internal/cron ./internal/db ./cmd/aura under WSL"
        status: pass
    human_judgment: false

duration: 1h 14m
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 04: Bounded OpenTelemetry Metrics and Runtime Boundaries Summary

**Aura now exports one bounded OpenTelemetry trace/metric story through private Prometheus and optional OTLP readers, with exactly-once semantic boundary instrumentation and no user-controlled metric cardinality.**

## Performance

- **Duration:** 1h 14m
- **Started:** 2026-07-21T12:01:19Z
- **Completed:** 2026-07-21T13:15:25Z
- **Tasks:** 3 TDD tasks
- **Files modified:** 45

## Accomplishments

- Added one shared-resource OTel runtime with Prometheus plus optional OTLP metric readers, partial-construction cleanup, exporter-failure isolation, and reverse-order context-bounded idempotent shutdown.
- Added a separately configurable `AURA_METRICS_BIND` listener defaulting to `127.0.0.1:9464`, backed by a dedicated registry and joined to serve drain without mounting metrics on public AG-UI routes.
- Centralized every Aura metric in a deterministic catalog with explicit type/unit/description/buckets, six permitted dimensions, finite normalizers, and duplicate-family rejection.
- Replaced direct client_golang agent ownership with OTel instruments while retaining only a named expvar compatibility projection for Phase 40 removal.
- Instrumented LLM, built-in tool, MCP stdio/HTTP/bridge, pause/resume commit, pgx query/transaction, scheduler scan/claim/job, public/private listener, idempotency, and retention seams with panic-safe exactly-once helpers.

## Task Commits

Each TDD task was committed RED then GREEN:

1. **Task 1: Build the dual-reader OpenTelemetry metric lifecycle**
   - RED - `20a72a574` (test): disabled/dual/partial/shutdown/private-registry lifecycle contract.
   - GREEN - `ce05b061f` (feat): shared trace/metric runtime, dual readers, dedicated scrape listener, and bounded shutdown.
2. **Task 2: Define a finite telemetry catalog and migrate legacy agent metrics once**
   - RED - `5d4abefc6` (test): descriptor golden, finite normalizer, and duplicate-owner contract.
   - GREEN - `e5955f8c8` (feat): bounded catalog, OTel agent metrics, expvar compatibility projection, and cycle-free sanitizer.
3. **Task 3: Instrument all required runtime boundaries**
   - RED - `7addf05d0` (test): success/error/cancel/panic exactly-once boundary contract.
   - GREEN - `69589e3aa` (feat): scoped runtime metrics/spans across every required owner seam.
   - TIDY - `f45f516d3` (chore): direct-versus-indirect module ownership after final imports.

## Files Created/Modified

- `internal/obs/meter.go` - Prometheus/OTLP reader construction, views, isolation, and shutdown ownership.
- `internal/obs/init.go` - one resource and top-level trace/metric runtime lifecycle.
- `internal/obs/catalog.go` - stable descriptor inventory, explicit buckets, bounded vocabularies, and hooks for later plans.
- `internal/obs/boundary.go` - scoped exactly-once count/duration/in-flight/span helper.
- `internal/agent/metrics.go` - OTel agent instruments plus temporary projection-only expvar compatibility.
- `internal/agent/llm_agent*.go` - owner-scoped LLM, tool, and pause observations without raw content attributes.
- `internal/mcp/observability.go` and `internal/agent/mcptools/bridge.go` - bounded transport/bridge metrics and spans.
- `internal/runner/runner_resume.go` and `resume_committer.go` - resume and durable commit lifecycle observations.
- `internal/db/observability.go` - SQL-blind pgx query tracer plus transaction boundary.
- `internal/cron/observability.go` - scheduler lifecycle, scan, claim, and job boundaries.
- `cmd/aura/serve_observability.go` and `serve_lifecycle.go` - private metrics component and listener observations joined to drain.
- `internal/redact/string.go` - dependency-light sanitization seam shared without an agent/AG-UI import cycle.

## Decisions Made

- A logical metric has one OTel owner. Prometheus is a reader/projection of that owner; compatibility expvar counters do not register Prometheus collectors.
- Prometheus and OTLP are independent readers on one provider, so exporter failure cannot take down the daemon or duplicate a measurement inside either reader.
- Boundary span attributes use the same finite catalog values as metrics. Recovered panic values and terminal error text are never persisted; only bounded classes are emitted.
- Tool metrics classify raw names before observation. MCP server names, URLs, operation keys, request/conversation/tool-call IDs, prompts, responses, SQL, and error strings are excluded from metric attributes.
- pgx pool tracing is the single query seam, while explicit transaction helpers provide a distinct `db_transaction` semantic boundary.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Extracted dependency-light sanitization to break an import cycle**
- **Found during:** Task 2 (agent metric migration).
- **Issue:** Reusing the existing AG-UI sanitizer from the catalog path would create `agent -> obs -> agui -> agent`.
- **Fix:** Moved the shared string sanitizer to `internal/redact` and retained the AG-UI wrapper contract.
- **Files modified:** `internal/redact/string.go`, `internal/agui/server_redact.go`, `internal/agent/workflow_edges_internal_test.go`, `internal/agui/server_p1_test.go`.
- **Verification:** Full module tests, vet, build, catalog fuzzing, and commit-hook lint pass.
- **Committed in:** `e5955f8c8`.

**2. [Rule 3 - Blocking] Split observability composition out of the near-limit serve file**
- **Found during:** Task 1 (private metrics listener wiring).
- **Issue:** `cmd/aura/serve.go` could not absorb the metrics lifecycle without violating the mandatory 600-LOC ceiling.
- **Fix:** Kept root composition in `serve.go` and moved the owned metrics component into `serve_observability.go` with shared lifecycle helpers.
- **Files modified:** `cmd/aura/serve.go`, `cmd/aura/serve_observability.go`, `cmd/aura/serve_lifecycle.go`.
- **Verification:** Pre-commit file-size gate passes; `serve.go` remains below 600 lines; serve tests and race suite pass.
- **Committed in:** `ce05b061f` and `69589e3aa`.

---

**Total deviations:** 2 auto-fixed blocking issues.
**Impact on plan:** Both changes preserved the locked architecture and project constraints; no optional feature or public exposure was introduced.

## Issues Encountered

- Native Windows Go could not run `-race` with the local CGO toolchain. The full requested race matrix ran against the same workspace under Ubuntu/WSL and passed.
- The pre-commit lint gate caught one obsolete `recordLLMDuration` shim after boundary ownership moved; it was removed and the commit was retried successfully without bypassing hooks.
- The repository defines module-integrity and vulnerability gates but no automated dependency-license target. `go mod verify` passed, and every newly introduced OTel/Prometheus module exposes an Apache-2.0 `LICENSE` in the module cache.

## Threat Surface Scan

- Metric attributes are generated only from the catalog's `operation`, `tool_class`, `transport`, `outcome`, `error_class`, and `state` allowlists; arbitrary values collapse to `other`.
- No identity, conversation, request, operation-key, tool-call, raw tool/server name, URL, prompt, response, SQL, or error string is emitted as a metric attribute.
- The Prometheus exporter receives a dedicated registry; production code never uses `prometheus.DefaultRegisterer`, and public AG-UI routing does not mount the handler.
- The default metrics bind is loopback-only and non-loopback addresses fail validation before listener creation.
- In-flight values return to zero on success, error, cancellation, timeout, and panic. `sync.Once` prevents repeated End/PanicSafe calls from decrementing below zero or double-emitting terminal events.
- OTLP construction/export/shutdown failures are isolated from core serving and all successfully constructed components still receive bounded shutdown.

## Known Stubs

None introduced. The added-code scan found no TODO, FIXME, XXX, HACK, placeholder, or "not implemented" marker. Added bare `return nil` statements are tested nil-receiver/disabled shutdown guards, successful validation returns, or test fakes—not incomplete implementations.

## User Setup Required

None. Prometheus scraping is available on the new optional private listener using `AURA_METRICS_BIND`; its secure default is `127.0.0.1:9464`. Existing OTLP settings continue to control exporter enablement.

## Next Phase Readiness

- Plans 39-05/39-06 can attach retention, idempotency, and learning outcomes through the catalog-owned public boundary helper without importing application code.
- Plan 39-07 can build dashboards and alerts against stable canonical `aura_*` families and the private scrape surface.
- Phase 40 can remove the explicitly named expvar compatibility adapter after its migration window.
- No blockers.

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: `internal/obs/meter.go`
- FOUND: `internal/obs/boundary.go`
- FOUND: `internal/obs/catalog.go`
- FOUND: `internal/agent/metrics.go`
- FOUND: `internal/cron/observability.go`
- FOUND: `internal/db/observability.go`
- FOUND: `internal/mcp/observability.go`
- FOUND: `cmd/aura/serve_observability.go`

**Commits verified to exist:**
- FOUND: `20a72a574` (Task 1 RED)
- FOUND: `ce05b061f` (Task 1 GREEN)
- FOUND: `5d4abefc6` (Task 2 RED)
- FOUND: `e5955f8c8` (Task 2 GREEN)
- FOUND: `7addf05d0` (Task 3 RED)
- FOUND: `69589e3aa` (Task 3 GREEN)
- FOUND: `f45f516d3` (module tidy)

**Fresh plan-level verification:**
- `go test ./... -count=1` - pass.
- `go test -race ./internal/obs ./internal/agent/... ./internal/mcp ./internal/cron ./internal/db ./cmd/aura` under WSL - pass.
- `go vet ./...` - pass.
- `go build ./...` - pass.
- `go mod tidy` plus `go mod verify` - pass and clean after `f45f516d3`.
- `govulncheck` over the repository-owned package set - zero reachable vulnerabilities.
- New dependency license inspection - Apache-2.0 for all five introduced modules.
- Commit hooks - gofmt, 600-LOC, whole-owned-package vet, golangci-lint all pass.
