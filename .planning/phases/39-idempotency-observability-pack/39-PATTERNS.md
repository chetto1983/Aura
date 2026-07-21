# Phase 39 — Implementation Pattern Map

**Mapped:** 2026-07-21  
**Source:** Live worktree inspection; the project codebase map was reported stale by the `plan:pre` drift gate.  
**Rule:** Re-read every cited file immediately before implementation; paths and symbols below are planning anchors, not permission to edit from memory.

## Responsibility and Data-Flow Map

| Capability | Primary owner | Existing analogs to preserve | Phase 39 extension |
|---|---|---|---|
| Public mutation idempotency | `internal/toolinvocations` store + `internal/gateway` orchestration | `Store.Reserve`, `Store.GetEnd`, `Gateway.reserve`, `Reconciler` | Add a separate identity-scoped operational registry/state machine; link it to the append-only execution ledger rather than replacing the Phase 35 tuple. |
| Serving lifecycle/readiness | `cmd/aura` composition root + `internal/agui` handler | `runServe`, `bootServe`, `serveReadinessProbes`, `ReadinessProbe`, `handleReadyz` | Own the listener synchronously, propagate unexpected exit, expose typed in-process snapshots, and run only live DB probes concurrently under one global deadline. |
| Process telemetry | `internal/obs` provider + package-local instrumentation helpers | `obs.Init`, `agent.NewTracerProvider`, `agentMetrics`, existing Prometheus registration helpers | Build one resource/MeterProvider with Prometheus and OTLP readers; give every semantic metric one owner and a bounded attribute policy. |
| Appliance observability | `compose.yaml`, `.github/workflows/ci.yml`, new `observability/` tree | Existing Compose healthcheck and CI container/service patterns | Add pinned Prometheus/Grafana/Tempo services, immutable rules/dashboards/runbooks, and syntax/behavior/provisioning checks. |
| Automatic retention | New `internal/retention` domain with storage adapters | `documents.StorageOrphanService`, `conversations.Sweeper`, `ScanOrphans` | Reuse deterministic preview/apply and joined-worker patterns; add durable bounded claims, two-phase external deletion, activity revalidation, audit, and policy versioning. |
| Explicit export/delete | `internal/runner` + AG-UI/API/CLI adapters | `Runner.DeleteConversationLifecycle`, `agui.handleConversationExport`, share snapshot builders | Export a complete owner-scoped versioned manifest first; invoke the canonical owner-delete saga only after a requested export succeeds. Automatic retention must not invoke it. |
| Learning bounds | `internal/activelearn`, `internal/reasoningstore`, `internal/toolselectstore` | Async learner worker, hash-keyed Neo4j `MERGE`, APOC vector JSON projection | Bound the seen set and all loads/writes; add TTL/cap metadata, deterministic selection, compaction, pinned-seed separation, and metrics. |

## Pattern 1 — Durable-Before-Effect Reservation

### Read first

- `internal/toolinvocations/store_reserve.go`
- `internal/gateway/reserve.go`
- `internal/gateway/reconcile.go`
- `internal/db/migrations/0042_drop_compaction.up.sql`
- The current migration directory listing at execution time

### Existing shape

`toolinvocations.Store.Reserve` uses `INSERT ... ON CONFLICT DO NOTHING` rows affected as the ownership decision. A store error fails closed; a duplicate reads the recorded terminal fact. `gateway.Reconciler` lists `start ∧ ¬end` rows, rechecks immediately before append, and records terminal ambiguity without reinvoking the tool.

```go
acquired, replay, err := g.store.Reserve(ctx, start)
if err != nil { return denyReservationFailure }
if acquired { return allowEffectOwner }
return replayWithoutExecuting(replay)
```

### Phase 39 application

- Keep `(conversation_id, request_id, tool_call_id)` as the internal execution/audit identity.
- Add an operational registry whose unique identity is `(identity_id, operation_scope, operation_key)`.
- Fingerprint normalized typed payloads with SHA-256; reject a same-key/different-fingerprint request.
- Make `in_progress`, `completed`, and terminal `indeterminate` explicit typed outcomes.
- Expire replay bodies independently after the configured 30-day default while preserving longer-lived audit metadata.
- Allocate the next free migration from disk at implementation time; current head is `0042`, so the expected slot is `0043+`. Never reuse ROADMAP’s historical `0026` label.

### Do not copy

The current duplicate preview (`[reservation held: result not yet available]`) is not the new public typed contract, and the execution tuple alone cannot cover scheduler/CLI operations or identity-scoped caller intent.

## Pattern 2 — Joined Worker with Bounded Shutdown

### Read first

- `internal/conversations/sweeper.go`
- `internal/gateway/reconcile.go`
- `cmd/aura/serve.go`

### Existing shape

`conversations.Sweeper` receives injected work, launches one goroutine only when enabled, exits on context/stop, owns a ticker, and joins its `WaitGroup` under a fixed timeout. The reconciler follows the same shape and performs a boot one-shot before periodic ticks.

### Phase 39 application

- Retention and learning compaction workers must be non-overlapping, bounded per batch, context-cancellable, and joined during drain.
- A disabled worker must launch no goroutine and report healthy-by-configuration where relevant.
- Worker progress snapshots should carry configured/running, last heartbeat/tick, last successful claim, draining, and bounded freshness state.
- Never spawn one goroutine per retention candidate or readiness dependency without a fixed bound.

## Pattern 3 — Typed Readiness Owned by the Composition Root

### Read first

- `internal/agui/readiness.go`
- `cmd/aura/serve.go` (`runServe`, `bootServe`, `serveReadinessProbes`)
- `compose.yaml` Aura healthcheck

### Existing shape and gap

`ReadinessProbe` is already injected by the composition root, which keeps dependency ownership testable. The current handler, however, gives each probe its own three-second timeout and runs sequentially; `runServe` starts `ListenAndServe` in a goroutine and only logs unexpected failure.

### Phase 39 application

- Bind with `net.Listen` before declaring boot complete; pass the listener to `http.Server.Serve` and surface every non-shutdown return to the top-level run group.
- Create one request deadline shorter than Compose’s `curl --max-time 3`; run PostgreSQL and Neo4j probes concurrently beneath it.
- Read schema-head, listener, scheduler, and drain status from in-process snapshots.
- Return stable bounded component codes, never raw dependency error strings; write redacted detail only to logs.
- Mark draining before teardown. A lost critical dependency makes `/readyz` return 503 while `/healthz` remains live.

## Pattern 4 — One Provider, Explicit Adapters, Ordered Shutdown

### Read first

- `internal/obs/init.go`
- `internal/agent/tracing.go`
- `internal/agent/metrics.go`
- `internal/agui/auth.go` and metrics-route tests

### Existing shape and gap

`obs.Init` is the process bootstrap and returns one shutdown function. It currently installs tracing only. `agentMetrics` dual-writes expvar/client_golang and uses sanitized but unbudgeted dynamic labels such as tool names.

### Phase 39 application

- Build one OTel resource and one `sdkmetric.MeterProvider` with both an OTel Prometheus reader and an OTLP periodic reader.
- Set global providers once; return one shutdown function that drains metrics and traces in a tested order under the caller’s timeout.
- Use an Aura-owned instrument/attribute catalog with finite enums, configured allowlists, budgets, and `other` overflow.
- Forbid identity, conversation/request/operation keys, prompts, arguments, results, paths, raw errors, and arbitrary MCP strings as metric attributes.
- Put the Prometheus handler on an internal-only metrics listener/network with no host port; do not weaken Aura’s cookie-auth boundary on the existing user-facing route.
- Keep legacy client_golang collectors behind a named compatibility adapter with a removal task and a descriptor-overlap test. No metric may be emitted by both owners.

## Pattern 5 — Deterministic Preview, Revalidation, Then Destruction

### Read first

- `internal/documents/orphans.go`
- `internal/conversations/orphan_scan.go`
- `internal/runner/runner_delete.go`
- `internal/agui/share_export.go`
- `internal/reasoningtrace/reasoningtrace.go`

### Existing shapes

- `StorageOrphanService.DryRun` sorts candidates and hashes the exact set; `Cleanup` recomputes the dry-run and rejects token drift.
- `ScanOrphans` reconstructs paths from trusted identifiers and uses `Lstat` so symlinks are never traversed.
- `DeleteConversationLifecycle` checks owner scope before cancelling work, expiring pauses, evicting tools, terminating jobs, revoking shares, and deleting persistence.
- The current export path owner-gates before reads and derives formats from one redacted snapshot.

### Phase 39 application

- Put policy evaluation in a pure planner that returns a deterministically ordered plan plus SHA-256 confirmation token.
- Claim a bounded PostgreSQL batch with deterministic `ORDER BY`, parameterized `LIMIT`, and `FOR UPDATE SKIP LOCKED`; mark `deleting` with lease and policy version before external effects.
- Revalidate owner/activity immediately before each destructive batch; delete backing objects first and finalize metadata last.
- Leave failed items retryable and append audit facts for policy, counts, bytes, and failures.
- Automatic retention skips protected conversations and never calls the owner-delete saga.
- Explicit export-delete finalizes a versioned manifest with safe names, sizes, and SHA-256 checksums before calling `DeleteConversationLifecycle`.

## Pattern 6 — Bounded Neo4j `MERGE` Stores

### Read first

- `internal/activelearn/learner.go`
- `internal/reasoningstore/store.go`
- `internal/toolselectstore/store.go`
- `internal/neostore` coercion/hash helpers

### Existing shape and gap

Both stores preserve idempotency with a content-hash `MERGE` key and use APOC JSON projection because list-valued columns are not returned reliably through the MCP Cypher seam. Both `LoadExamples` queries currently materialize every matching node, and `activelearn.Learner.seen` is an unbounded `sync.Map`.

### Phase 39 application

- Preserve current hash/MERGE identities and APOC vector projection.
- Store `created_at`, `expires_at`, bucket, pinned/manual marker, quality, novelty, and source metadata using Cypher 5-compatible syntax.
- Validate a maximum load limit before query execution; every read uses deterministic `ORDER BY` and `LIMIT`.
- Cap the seen hash set at 100,000 with 30-day TTL and no raw text.
- Enforce 90-day TTL, 512 per tier/tool bucket, 10,000 per learned store, newest-25% preservation, and deterministic seeded quality/novelty-weighted selection at write admission and in bounded compaction.
- Keep pinned/manual evaluation seeds separate from learned capacity and emit size/age/load/drop/expiry/compaction/eviction outcomes through the shared metric catalog.

## Observability-as-Code Layout

```text
observability/
  prometheus/
    prometheus.yml
    rules/aura-recording.yml
    rules/aura-alerts.yml
    tests/aura-rules.test.yml
  grafana/
    provisioning/datasources/aura.yml
    provisioning/dashboards/aura.yml
    dashboards/aura-overview.json
    dashboards/aura-agent-llm.json
    dashboards/aura-tools-mcp.json
    dashboards/aura-data-retention.json
  tempo/tempo.yml
  runbooks/*.md
scripts/verify-observability.ps1
```

- Resolve official Prometheus/Grafana/Tempo tags and immutable digests at implementation time.
- Recording rules precede alerts; dashboards query only descriptor-manifest names.
- Grafana UIDs and panel IDs are stable APIs used by alert/runbook links; validate uniqueness and set `allowUiUpdates: false`.
- CI runs rule syntax and behavior tests, JSON/schema checks, UID/datasource/query contracts, `docker compose config`, and a provisioning smoke test.

## Project Constraints to Carry into Every Plan

- `prd.md` is the architectural truth source; amend it before implementation if a plan discovers a contradiction.
- Re-read the migration directory before creating any migration; do not infer a number from ROADMAP or CLAUDE prose.
- No implementation file may exceed 600 LOC; refactor touched files instead of creating god files.
- Use TDD for business logic, state machines, bounded algorithms, and endpoint contracts.
- After Go edits run the affected package tests and race tests; wave/phase gates include vet, build, lint, integration tags, no-skip-as-green checks, the ≥85% owned-surface coverage gate, and a ≥70% mutation spot-check on critical logic.
- Integration coverage must use the disposable coverage DB path (`make coverage-docker` / `scripts/coverage_docker.sh`), never the live `aura` database.
- Existing untracked `.planning/phases/39-idempotency-observability-pack/39-research-plan-input.json` is not a Phase 39 implementation artifact and must remain untouched.

## Planner Checklist

- Foundation contracts and Wave 0 seams land before dependent streams.
- Idempotency propagation follows registry/store state-machine work.
- Dashboards and alerts follow stable metric descriptors/recording rules.
- Deletion adapters follow durable claims and activity evidence.
- Every plan lists created symbols and exact files.
- Every task includes `read_first`, concrete `action`, automated `verify`, and objective `acceptance_criteria`.
- Every plan includes an ASVS L1 `<threat_model>` and no high-severity threat remains unmitigated.
