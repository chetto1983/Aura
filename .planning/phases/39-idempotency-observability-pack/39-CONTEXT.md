# Phase 39: Idempotency + Observability Pack - Context

**Gathered:** 2026-07-21
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 39 makes mutating operations retry-safe and ships the production observability and retention surface required by OBS-01..06. It adds an end-to-end operation-key contract, truthful readiness, OpenTelemetry metrics and spans with Prometheus/Grafana assets, safe sidecar/trace cleanup and per-conversation export/delete, and bounded learning stores.

Security enforcement for secret-like trace content remains Phase 40 work; backup/DR and the broader production-operations closeout remain Phase 41 work. This phase may expose the configuration and telemetry seams those later phases consume, but must not absorb their acceptance scope.

</domain>

<decisions>
## Implementation Decisions

### Idempotency boundaries
- **D-01:** The public mutation identity is a caller-stable operation key plus a normalized payload fingerprint, scoped by identity and tool/operation. The existing Phase-35 tuple (conversation_id, request_id, tool_call_id) remains the internal execution and audit identity.
- **D-02:** Reusing an operation key with changed arguments is rejected as an idempotency conflict. A matching completed operation replays its stored result.
- **D-03:** Every mutating surface participates and propagates the same logical key when supported: HTTP/API, agent tools, scheduler, CLI, approvals/resumes, and MCP metadata. Aura's ledger remains the protection boundary for legacy downstream systems that cannot accept a key.
- **D-04:** A duplicate whose original is still executing returns an immediate typed in_progress outcome with retry guidance. Crash-orphaned or remotely ambiguous mutations become terminal indeterminate and are never automatically reinvoked.
- **D-05:** Replay protection lasts for the operation lifetime plus 30 days by default. Longer-lived audit metadata is retained separately from full replay bodies.

### Readiness semantics
- **D-06:** Invalid configuration, incomplete/failed migration, listener bind failure, and unexpected listener-loop exit are fatal. Runtime loss of a critical dependency makes /readyz return 503 while /healthz remains live.
- **D-07:** Readiness gates only the core serving contract: PostgreSQL, Neo4j, schema-at-head, AG-UI listener, and scheduler state when enabled. LLM providers, Garage, MCP servers, GPU/multimodal services, PIM, WhatsApp, and other optional integrations degrade at feature level and emit telemetry.
- **D-08:** Scheduler readiness is progress-based: the loop is running, its heartbeat/tick is fresh, the DB claim path works, and it is not draining. Individual job failures and queue lag alert separately; readiness fails only when progress stops past a hard threshold. A disabled scheduler is healthy by configuration.
- **D-09:** /readyz runs live database probes concurrently under one global deadline shorter than Compose's 3-second client timeout. Migration/listener/scheduler/drain checks use in-process snapshots. The response is stable, typed, sanitized JSON; detailed redacted causes go to logs. The endpoint reports current truth immediately and Compose retries provide debounce.

### Telemetry and alerts
- **D-10:** OpenTelemetry is the canonical metrics instrumentation API. One MeterProvider feeds OTLP metrics and the Prometheus reader. Pin a reviewed GenAI semantic-convention version and migrate existing client_golang collectors through a time-bounded compatibility layer without permanent duplicate instrumentation.
- **D-11:** Metric dimensions are bounded and low-cardinality, with explicit budgets and overflow folded into other. Identity, conversation/request/operation keys, raw errors, paths, prompts, arguments, and results are forbidden as metric labels. Traces carry correlation metadata; content capture is explicit opt-in and redacted.
- **D-12:** Alerting is two-tier, SLO- and symptom-based. Page only for sustained user impact such as readiness loss, error/latency budget burn, resume failure, or scheduler no-progress. Route component causes such as MCP/tool timeouts, queue lag, disk pressure, and cleanup failures to warning/ticket alerts. Use debounce durations, multi-window burn rates, and dashboard/runbook links.
- **D-13:** Ship an immutable observability pack in Git: versioned Prometheus recording/alert rules, provisioned Grafana dashboards with stable UIDs, and no production-only UI edits. CI validates rule syntax, metric/query contracts, dashboard JSON, and provisioning. Dashboard layers cover overview/SLO, agent/LLM, tools/MCP, and data/scheduler/retention; alerts link to exact panels and runbooks.

### Retention and cleanup
- **D-14:** One idempotent policy engine serves both the scheduled sweeper and operator CLI. The sweeper automatically applies configured policy; the CLI defaults to dry-run and requires explicit apply. Cleanup is two-phase (mark deleting, remove external artifacts, finalize metadata), bounded, non-overlapping, retryable, revalidated immediately before deletion, and fully audited with policy version, counts, bytes, and failures.
- **D-15:** Retention is class-based. Temporary/unreferenced crash artifacts retain 24 hours. Full-content local reasoning traces retain 24 hours in production and 7 days in trusted development. Metadata-only OpenTelemetry traces retain 14 days. Referenced sidecars and agent artifacts follow the conversation lifetime; conversations are unlimited by default until an operator configures retention. Warn at 70% disk, raise urgent alerts at 80%, and stop optional full/debug trace creation at 85%; never emergency-delete active or canonical data.
- **D-16:** Active-conversation protection uses durable activity evidence, not conversations.status = active: live turn lease, fresh worker heartbeat, pending pause/approval, queued/running scheduler work, background tool/sandbox job, or unfinished artifact operation. Automatic retention skips protected conversations and never cancels work. Explicit owner deletion uses Aura's ordered teardown. Export is an owner-scoped consistent snapshot with versioned manifest, conversation/turn data, referenced sidecars/assets, policy metadata, sizes, and checksums. Combined export-delete starts deletion only after export succeeds; plain delete creates no hidden backup.
- **D-17:** activelearn.seen is capped at 100,000 content hashes with a 30-day TTL and stores no raw text. Learned examples have a 90-day TTL, a 512-example cap per reasoning tier/tool, and a 10,000-example global cap per store. Preserve the newest 25% of a bucket and choose the remainder with quality/novelty-weighted reservoir sampling. Enforce hard caps on writes, compact in bounded background batches, keep manual/pinned evaluation seeds separately, bound every load, and emit size/age/load/drop/expiry/compaction/eviction metrics.

### the agent's Discretion
- Exact table, column, package, command, and configuration names, provided the contracts above stay stable and typed.
- Exact readiness heartbeat and alert burn-rate windows, chosen from measured baselines with conservative appliance defaults and configuration overrides.
- The reviewed OpenTelemetry GenAI semantic-convention version to pin during implementation; do not float an experimental convention implicitly.
- The exact deterministic quality/novelty score and bounded compaction algorithm, provided the locked recency share, TTLs, hard caps, per-bucket fairness, and load bounds hold.
- Migration allocation. ROADMAP.md says migration 0026, but 0026 is already historical and the repository currently reaches 0042_drop_compaction. Planning must allocate the next actual migration number and must not reuse 0026.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase and architecture contracts
- .planning/REQUIREMENTS.md — OBS-01..06 and the Phase 39 traceability row; SEC-01 marks the adjacent Phase-40 trace-security boundary.
- .planning/ROADMAP.md — Phase 39 goal and four success criteria. Its migration-0026 label is stale; use the next on-disk migration slot.
- prd.md — persisted pause/resume, conversation, scheduler, and future-retention architecture background.
- .planning/codebase/STACK.md — deployed Go, PostgreSQL, Neo4j, Compose, OpenTelemetry, Prometheus, and Grafana stack.
- .planning/codebase/ARCHITECTURE.md — package boundaries and serving composition.
- .planning/codebase/INTEGRATIONS.md — external dependency and sidecar topology.
- .planning/phases/34-agent-loop-correctness-durable-ledger/34-CONTEXT.md — sidecar fencing, crash-orphan reconciliation, and pause/resume correctness.
- .planning/phases/35-toolgateway-policy-engine/35-CONTEXT.md — existing ToolGateway reservation, audit, and replay contract.
- .planning/phases/36-multi-user-identity-isolation-authula-cutover/36-CONTEXT.md — identity scoping, ordered conversation deletion, and soft-delete/purge saga conventions.
- .planning/phases/38-mcp-governance-hardening/38-CONTEXT.md — locked MCP trust, timeout, lifecycle, and health semantics that Phase 39 must observe rather than redefine.

### Idempotency implementation and industrial references
- internal/gateway/reserve.go — current ToolGateway reservation and duplicate handling.
- internal/gateway/reconcile.go — orphan reservation reconciliation and indeterminate terminal posture.
- internal/toolinvocations/store_reserve.go — durable uniqueness and replay persistence.
- D:/tmp/aura-pim-mcp/docs/mcp-tools.md — local MCP mutation surface and tool contracts.
- D:/tmp/aura-pim-mcp/changelogs/2026-05-03-attachment-get-delete.md — local idempotent attachment mutation precedent.
- https://docs.stripe.com/api/idempotent_requests — stable client keys, replay, and argument mismatch behavior.
- https://docs.stripe.com/api-v2-overview — current 30-day idempotency retention precedent.
- https://aws.amazon.com/builders-library/making-retries-safe-with-idempotent-APIs/ — caller intent, semantic equivalence, and safe retry design.
- https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ECS_Idempotency.html — scoped client-token contract and mismatch errors.
- https://google.aip.dev/155 — request-id and expiration guidance.
- https://modelcontextprotocol.io/specification/2025-11-25/basic/index — current MCP metadata extension point.

### Readiness and serving lifecycle
- internal/agui/readiness.go — current sequential PostgreSQL/Neo4j /readyz implementation.
- compose.yaml — current healthcheck timing; curl has a 3-second maximum and Compose supplies retries/start period.
- cmd/aura/serve.go — listener, scheduler, sweeper, drain, and shutdown composition.
- https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes — readiness/liveness separation and probe semantics.
- https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/ — current probe configuration behavior.
- https://sre.google/sre-book/addressing-cascading-failures/ — fail-fast and overload/cascading-failure guidance.
- https://docs.docker.com/compose/how-tos/startup-order/ — Compose health-based dependency startup.
- https://learn.microsoft.com/en-us/aspnet/core/host-and-deploy/health-checks?view=aspnetcore-10.0 — separate live and ready endpoints.
- D:/tmp/agent-memory/deploy/cloudrun/service.yaml — local contrast case that reuses one endpoint for startup/liveness.
- D:/tmp/spike-librechat/client/src/hooks/SSE/useResumableSSE.ts — local bounded SERVER_NOT_READY/Retry-After client behavior.

### Telemetry, alerts, and dashboards
- internal/obs/init.go — current trace-only OpenTelemetry initialization.
- internal/agent/metrics.go — existing direct client_golang metrics to migrate/adapt.
- D:/tmp/aura-pim-mcp/docs/telemetry.md — local OTLP topology, redaction, and sampling reference; do not copy its latency-ms names or identity-like metric labels.
- D:/tmp/agent-memory/typescript/src/observability.ts — local typed request/response/error event precedent.
- https://opentelemetry.io/docs/specs/semconv/ — stable semantic-convention release surface.
- https://github.com/open-telemetry/semantic-conventions-genai — current GenAI convention development and versioning.
- https://github.com/open-telemetry/semantic-conventions/blob/main/docs/gen-ai/gen-ai-metrics.md — GenAI metric definitions and development status.
- https://opentelemetry.io/blog/2026/genai-observability/ — 2026 GenAI observability direction.
- https://prometheus.io/docs/practices/naming/ — metric naming and base-unit conventions.
- https://prometheus.io/docs/practices/instrumentation/ — bounded instrumentation/cardinality guidance.
- https://prometheus.io/docs/practices/alerting/ — symptom-first, actionable alerting guidance.
- https://prometheus.io/docs/practices/rules/ — recording/alert rule conventions.
- https://sre.google/workbook/index/ — SLO and multi-window burn-rate methodology.
- https://grafana.com/docs/grafana/latest/administration/provisioning/ — dashboard/config provisioning as code.
- https://grafana.com/docs/grafana/latest/alerting/set-up/provision-alerting-resources/file-provisioning/ — file-provisioned alert resources.

### Retention, cleanup, export, and learning caps
- internal/conversations/orphan_scan.go — existing 24-hour temp/crash-sidecar grace, symlink guards, and disk warning.
- internal/conversations/sweeper.go — existing periodic reconciliation worker.
- internal/reasoningtrace/reasoningtrace.go — current 8 MiB active trace plus one backup.
- internal/runner/runner_delete.go — canonical ordered owner-scoped conversation teardown.
- internal/activelearn/learner.go — currently unbounded content-hash sync.Map.
- internal/reasoningstore/store.go — currently loads every ReasoningExample.
- internal/toolselectstore/store.go — currently loads every ToolSelectionExample.
- D:/tmp/agent-memory/docs/modules/ROOT/pages/how-to/consolidation.adoc — dry-run-first, idempotent, audited consolidation precedent.
- D:/tmp/agent-memory/src/neo4j_agent_memory/memory/consolidation.py — local candidate caps and consolidation implementation; archive marks but does not delete.
- D:/tmp/spike-librechat/packages/api/src/files/sweep.ts — bounded non-overlapping sweeper and storage-aware deletion.
- D:/tmp/spike-librechat/packages/data-schemas/src/utils/retention.ts — explicit retention-visibility filters.
- D:/tmp/spike-librechat/packages/data-schemas/src/schema/file.ts — backing-storage-first, metadata-last retention contract.
- https://grafana.com/docs/tempo/latest/configuration/ — Tempo 14-day trace default and compaction configuration.
- https://grafana.com/docs/tempo/latest/reference-tempo-architecture/components/compaction/ — persisted retention/redaction jobs, worker lifecycle, and metrics.
- https://prometheus.io/docs/prometheus/3.5/storage/ — time/size retention and 80–85% maximum disk allocation guidance.
- https://www.postgresql.org/docs/current/ddl-partitioning.html — detach/drop partitions for large retention sets when justified.
- https://www.postgresql.org/docs/current/sql-select.html — SKIP LOCKED for bounded queue-like worker claims.
- https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/ — mark-then-cleanup-before-final-delete pattern.
- https://redis.io/docs/latest/develop/reference/eviction/ — bounded cache TTL/LRU/LFU and decay principles.
- https://www.cs.umd.edu/~samir/498/vitter.pdf — bounded reservoir sampling.
- https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/ — current 2026 vector storage/query capabilities and bounded-neighbor APIs.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- ToolGateway reservation/reconciliation: extend the durable ledger rather than building a second retry system. Preserve the existing execution tuple for audit while adding the public operation-key/fingerprint layer.
- Readiness handler and Compose healthcheck: retain the endpoints and response seam, replacing sequential probes with a globally budgeted aggregate and adding in-process component state.
- Runner session registry and DeleteConversationLifecycle: reuse the existing ordered explicit-delete teardown; automatic retention adds durable activity leases and must skip rather than cancel.
- ScanOrphans and RunDirSweeper: reuse their path reconstruction, Lstat/symlink discipline, 24-hour crash grace, and periodic worker lifecycle inside the unified retention engine.
- Existing Prometheus collectors: bridge or migrate them through the OTel MeterProvider compatibility window; do not emit the same semantic metric twice.

### Established Patterns
- Durable-before-effect and terminal indeterminate: ToolGateway already refuses blind reinvocation after an ambiguous mutation.
- Identity-scoped ownership: every mutation, export, cleanup claim, and audit record must preserve Phase-36 identity isolation.
- Reconstruct paths from trusted identifiers and never follow sidecar symlinks.
- Scheduled workers are bounded, stoppable, joined during drain, and must not overlap themselves.
- Production artifacts are versioned and CI-validated; generated Grafana dashboards/rules are not edited only in a running UI.

### Integration Points
- Operation-key fields and indexes attach to the existing tool_invocations/ToolGateway store. The planner must reconcile schema changes with the current 0042 migration head.
- Readiness state attaches at db.Open/migration completion, AG-UI listener startup/exit, scheduler start/tick/claim/drain, and serve shutdown.
- OTel initialization in internal/obs/init.go becomes the metrics provider used by LLM, tool, MCP, pause/resume, DB, scheduler, listener, and retention instrumentation.
- Prometheus rule files, Grafana provisioning, and CI validation integrate with compose.yaml and .github/workflows/ci.yml.
- Retention spans PostgreSQL metadata, the run directory, Garage-backed agent assets, reasoning traces, and Neo4j learning nodes; the policy engine must finalize metadata only after owned external objects are resolved.
- Learning-store writes and loads need timestamps, bucket keys, deterministic ordering, hard-cap eviction, and metrics; current schemaless Neo4j MERGE keys remain the idempotency anchor.

</code_context>

<specifics>
## Specific Ideas

- Follow Stripe/AWS/Google's caller-declared intent pattern: an operation key is not merely a regenerated transport request ID.
- Treat D:/tmp examples as comparative evidence, not copy targets. Agent-memory has the right dry-run/audit posture but only archives; LibreChat has the right bounded storage-first sweep but relies on database TTL behavior that cannot protect Aura's cross-store lifecycle by itself; the PIM telemetry document has useful OTLP topology but metric names/labels must be normalized for Prometheus.
- The observability pack should feel like an appliance feature: one checked-in stack, stable dashboard UIDs, alert-to-panel/runbook navigation, and safe defaults that work without a separate SaaS control plane.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope. Secret-like persistence enforcement/full-trace production fail-fast remains explicitly assigned to Phase 40, and backup/DR remains Phase 41.

</deferred>

---

*Phase: 39-idempotency-observability-pack*
*Context gathered: 2026-07-21*
