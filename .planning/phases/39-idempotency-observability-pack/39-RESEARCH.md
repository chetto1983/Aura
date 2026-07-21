# Phase 39: Idempotency + Observability Pack — Research

**Researched:** 2026-07-21  
**Question:** What must be known to plan Phase 39 well?  
**Overall confidence:** HIGH for repository findings and architecture; MEDIUM for fast-moving GenAI semantic-convention details.

<user_constraints>
## User Constraints

### Locked Decisions

#### Idempotency boundaries
- **D-01:** The public mutation identity is a caller-stable operation key plus a normalized payload fingerprint, scoped by identity and tool/operation. The existing Phase-35 tuple (conversation_id, request_id, tool_call_id) remains the internal execution and audit identity.
- **D-02:** Reusing an operation key with changed arguments is rejected as an idempotency conflict. A matching completed operation replays its stored result.
- **D-03:** Every mutating surface participates and propagates the same logical key when supported: HTTP/API, agent tools, scheduler, CLI, approvals/resumes, and MCP metadata. Aura's ledger remains the protection boundary for legacy downstream systems that cannot accept a key.
- **D-04:** A duplicate whose original is still executing returns an immediate typed in_progress outcome with retry guidance. Crash-orphaned or remotely ambiguous mutations become terminal indeterminate and are never automatically reinvoked.
- **D-05:** Replay protection lasts for the operation lifetime plus 30 days by default. Longer-lived audit metadata is retained separately from full replay bodies.

#### Readiness semantics
- **D-06:** Invalid configuration, incomplete/failed migration, listener bind failure, and unexpected listener-loop exit are fatal. Runtime loss of a critical dependency makes /readyz return 503 while /healthz remains live.
- **D-07:** Readiness gates only the core serving contract: PostgreSQL, Neo4j, schema-at-head, AG-UI listener, and scheduler state when enabled. LLM providers, Garage, MCP servers, GPU/multimodal services, PIM, WhatsApp, and other optional integrations degrade at feature level and emit telemetry.
- **D-08:** Scheduler readiness is progress-based: the loop is running, its heartbeat/tick is fresh, the DB claim path works, and it is not draining. Individual job failures and queue lag alert separately; readiness fails only when progress stops past a hard threshold. A disabled scheduler is healthy by configuration.
- **D-09:** /readyz runs live database probes concurrently under one global deadline shorter than Compose's 3-second client timeout. Migration/listener/scheduler/drain checks use in-process snapshots. The response is stable, typed, sanitized JSON; detailed redacted causes go to logs. The endpoint reports current truth immediately and Compose retries provide debounce.

#### Telemetry and alerts
- **D-10:** OpenTelemetry is the canonical metrics instrumentation API. One MeterProvider feeds OTLP metrics and the Prometheus reader. Pin a reviewed GenAI semantic-convention version and migrate existing client_golang collectors through a time-bounded compatibility layer without permanent duplicate instrumentation.
- **D-11:** Metric dimensions are bounded and low-cardinality, with explicit budgets and overflow folded into other. Identity, conversation/request/operation keys, raw errors, paths, prompts, arguments, and results are forbidden as metric labels. Traces carry correlation metadata; content capture is explicit opt-in and redacted.
- **D-12:** Alerting is two-tier, SLO- and symptom-based. Page only for sustained user impact such as readiness loss, error/latency budget burn, resume failure, or scheduler no-progress. Route component causes such as MCP/tool timeouts, queue lag, disk pressure, and cleanup failures to warning/ticket alerts. Use debounce durations, multi-window burn rates, and dashboard/runbook links.
- **D-13:** Ship an immutable observability pack in Git: versioned Prometheus recording/alert rules, provisioned Grafana dashboards with stable UIDs, and no production-only UI edits. CI validates rule syntax, metric/query contracts, dashboard JSON, and provisioning. Dashboard layers cover overview/SLO, agent/LLM, tools/MCP, and data/scheduler/retention; alerts link to exact panels and runbooks.

#### Retention and cleanup
- **D-14:** One idempotent policy engine serves both the scheduled sweeper and operator CLI. The sweeper automatically applies configured policy; the CLI defaults to dry-run and requires explicit apply. Cleanup is two-phase (mark deleting, remove external artifacts, finalize metadata), bounded, non-overlapping, retryable, revalidated immediately before deletion, and fully audited with policy version, counts, bytes, and failures.
- **D-15:** Retention is class-based. Temporary/unreferenced crash artifacts retain 24 hours. Full-content local reasoning traces retain 24 hours in production and 7 days in trusted development. Metadata-only OpenTelemetry traces retain 14 days. Referenced sidecars and agent artifacts follow the conversation lifetime; conversations are unlimited by default until an operator configures retention. Warn at 70% disk, raise urgent alerts at 80%, and stop optional full/debug trace creation at 85%; never emergency-delete active or canonical data.
- **D-16:** Active-conversation protection uses durable activity evidence, not conversations.status = active: live turn lease, fresh worker heartbeat, pending pause/approval, queued/running scheduler work, background tool/sandbox job, or unfinished artifact operation. Automatic retention skips protected conversations and never cancels work. Explicit owner deletion uses Aura's ordered teardown. Export is an owner-scoped consistent snapshot with versioned manifest, conversation/turn data, referenced sidecars/assets, policy metadata, sizes, and checksums. Combined export-delete starts deletion only after export succeeds; plain delete creates no hidden backup.
- **D-17:** activelearn.seen is capped at 100,000 content hashes with a 30-day TTL and stores no raw text. Learned examples have a 90-day TTL, a 512-example cap per reasoning tier/tool, and a 10,000-example global cap per store. Preserve the newest 25% of a bucket and choose the remainder with quality/novelty-weighted reservoir sampling. Enforce hard caps on writes, compact in bounded background batches, keep manual/pinned evaluation seeds separately, bound every load, and emit size/age/load/drop/expiry/compaction/eviction metrics.

### The Agent's Discretion

- Exact table, column, package, command, and configuration names, provided the contracts above stay stable and typed.
- Exact readiness heartbeat and alert burn-rate windows, chosen from measured baselines with conservative appliance defaults and configuration overrides.
- The reviewed OpenTelemetry GenAI semantic-convention version to pin during implementation; do not float an experimental convention implicitly.
- The exact deterministic quality/novelty score and bounded compaction algorithm, provided the locked recency share, TTLs, hard caps, per-bucket fairness, and load bounds hold.
- Migration allocation. ROADMAP.md says migration 0026, but 0026 is already historical and the repository currently reaches 0042_drop_compaction. Planning must allocate the next actual migration number and must not reuse 0026.

### Deferred Ideas

None — discussion stayed within phase scope. Secret-like persistence enforcement/full-trace production fail-fast remains explicitly assigned to Phase 40, and backup/DR remains Phase 41.
</user_constraints>

<phase_requirements>
## Phase Requirements

| Requirement | Exact obligation | Research support |
|---|---|---|
| OBS-01 | AG-UI listener startup/runtime failure is fatal to the serving process OR reflected in `/readyz`; the Docker/Compose healthcheck probes `/readyz`; a port conflict fails startup or readiness. | Bind the listener synchronously, make unexpected serve-loop exit fatal, retain the already-correct Compose `/readyz` probe, and add port-conflict/runtime-exit tests. |
| OBS-02 | `/readyz` reflects database, listener, migration state, and scheduler state; it fails when any critical serving dependency fails. | Replace sequential per-probe deadlines with one concurrent global budget and combine live DB probes with typed component snapshots. |
| OBS-03 | OpenTelemetry spans wrap LLM, tool, MCP, pause/resume, DB, and scheduler work; the OTel **metric** path is wired (today only traces are) following the target-architecture identifiers + GenAI semantic conventions. | Build one MeterProvider with OTLP and Prometheus readers, pin semconv `v1.39.0`, and instrument each named boundary with attribute-budget tests. |
| OBS-04 | Prometheus alert rules + Grafana dashboards ship in-repo (loop error rate, tool timeout rate, queue lag, LLM latency, MCP timeout rate, resume failures, listener state) and are syntax/JSON-validated in CI. | Add provisioned assets, rule behavior tests, stable UID/query-contract validation, runbooks, and containerized CI tooling. |
| OBS-05 | Sidecar/trace retention is a first-class operation — retention config, cleanup command, disk-usage metrics, per-conversation export/delete, with active-conversation exclusion + dry-run. | Unify the current sweeper, path guards, dry-run token, ordered deletion, and export projection behind a two-phase policy engine. |
| OBS-06 | Learning stores (`activelearn` `seen`, `reasoningstore`, `toolselectstore`) have a retention cap (max per label/tool, TTL/compaction, bounded load) + metrics. | Replace the unbounded map/load-all queries with timestamped caps, deterministic bounded reads, hard write admission, and batched compaction. |
</phase_requirements>

## Executive Recommendation

Plan this as four coordinated implementation streams with explicit contracts between them: (1) a durable public-operation registry layered over the existing ToolGateway audit tuple, (2) a truthful serving-state aggregate, (3) a single OTel telemetry pipeline plus immutable observability assets, and (4) one retention policy engine that also bounds Neo4j learning data. These can be implemented in parallel only after the migration and shared type contracts land. [VERIFIED: repository audit]

The next migration is **0043 or later**, allocated from the on-disk head at execution time; `0026` must not be reused. The current highest pair is `0042_drop_compaction`. [VERIFIED: `migrations/` inventory, 2026-07-21]

The highest-risk planning mistake is treating each requirement as a local patch. Idempotency spans every mutating ingress, readiness spans boot and runtime ownership, metrics share one cardinality policy, and retention crosses PostgreSQL, filesystem sidecars, Garage assets, local trace files, Tempo, and Neo4j. Each needs a typed cross-package contract before adapters are changed. [VERIFIED: codebase integration audit]

## Current-State Gap Inventory

| Area | Reusable baseline | Planning-critical gap |
|---|---|---|
| Idempotency | `internal/gateway/reserve.go`, reconciler, and `toolinvocations` rows-affected reservation already provide durable-before-effect behavior for the Phase-35 execution tuple. | The tuple is not a caller-stable identity-scoped operation key; duplicates currently return a preview, not typed `in_progress`; replay bodies cannot expire independently from append-only audit metadata. [VERIFIED: code audit] |
| Listener lifecycle | `cmd/aura/serve.go` owns the server and shutdown path. | `ListenAndServe` runs in a goroutine and logs non-shutdown errors while the daemon continues, so bind failure and unexpected exit are fail-soft. [VERIFIED: `cmd/aura/serve.go`] |
| Readiness | `/healthz`, `/readyz`, Postgres/Neo4j probes, and a Compose healthcheck already exist; Compose uses `curl --max-time 3`. | Probes execute sequentially with individual 3-second contexts; response values are ad-hoc strings; listener, schema-head, scheduler-progress, and drain snapshots are absent. [VERIFIED: `internal/agui/readiness.go`, `compose.yaml`] |
| OTel | `internal/obs/init.go` initializes tracing and agent code emits LLM/tool/turn spans. | There is no MeterProvider, and MCP, pause/resume, DB, scheduler, listener, and retention coverage is incomplete. [VERIFIED: `internal/obs`, `internal/agent`] |
| Prometheus | `internal/agent/metrics.go`, AG-UI, and panic metrics use `client_golang`; `/metrics` exists. | Dynamic tool labels are sanitized/truncated but not allowlisted or budgeted; direct collectors and `expvar` can become duplicate semantic sources during migration. `/metrics` is intentionally cookie-auth gated. [VERIFIED: code and auth tests] |
| Observability pack | Docker/Compose and CI exist. | No checked-in Prometheus rules, Grafana provisioning/dashboard pack, Tempo config, or corresponding asset-validation suite was found. [VERIFIED: repository file inventory] |
| Retention | `ScanOrphans`, `RunDirSweeper`, reasoning-trace rotation, ordered conversation deletion, share export, and a deterministic dry-run cleanup token are reusable primitives. | Cleanup is not one audited two-phase policy engine; active evidence is incomplete; export lacks a versioned full manifest/checksums; disk thresholds are not percentage-based. [VERIFIED: code audit] |
| Learning | Neo4j stores use idempotent `MERGE` anchors; `activelearn.seen` stores only hashes. | `seen` is an unbounded `sync.Map`; reasoning/tool-selection loads are unbounded; TTL, hard caps, compaction, fairness, and metrics are absent. [VERIFIED: `internal/activelearn`, `internal/reasoningstore`, `internal/toolselectstore`] |

## Standard Stack and Versions

Use the repository's existing Go stack and add only the official OTel metric modules that match its OTel release. [VERIFIED: `go.mod` and Go module origin metadata]

| Purpose | Package/tool | Verified version or rule |
|---|---|---|
| Language | Go | `1.26.5` in `go.mod` and local toolchain. [VERIFIED: local environment] |
| PostgreSQL | `github.com/jackc/pgx/v5` | `v5.10.0`. [VERIFIED: `go.mod`] |
| Neo4j | `github.com/neo4j/neo4j-go-driver/v5` | `v5.28.4`; use Cypher 5-compatible syntax, not Cypher 25-only features. [VERIFIED: `go.mod`, project runtime] |
| OTel core/SDK | `go.opentelemetry.io/otel`, `otel/sdk` | `v1.44.0`. [VERIFIED: `go.mod`, Go proxy/VCS origin] |
| OTel Prometheus reader | `go.opentelemetry.io/otel/exporters/prometheus` | `v0.66.0`, the release aligned with OTel Go `v1.44.0`. [VERIFIED: Go module metadata] |
| OTLP metric exporter | `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | `v1.44.0`. [VERIFIED: Go module metadata] |
| GenAI conventions | `go.opentelemetry.io/otel/semconv/v1.39.0` | Pin this reviewed import path in code and tests; never import a floating experimental alias. [VERIFIED: installed OTel module; MEDIUM confidence because GenAI conventions remain developmental] |
| Legacy Prometheus | `github.com/prometheus/client_golang` | Existing `v1.23.2`, compatibility only with a removal deadline. [VERIFIED: `go.mod`] |
| Rule validation | `promtool` | Use the exact Prometheus image pinned by tag and digest in Compose/CI; run `check rules` and `test rules`. [CITED: https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules/] |
| Dashboards | Grafana | Provision files with stable UIDs and `allowUiUpdates: false`; pin an official Grafana 12 image digest during implementation. [CITED: https://grafana.com/docs/grafana/latest/administration/provisioning/] |
| Trace backend | Tempo | Keep backend block retention in Tempo compactor config and application-local trace retention in Aura. Pin an official Tempo 2 image digest during implementation. [CITED: https://grafana.com/docs/tempo/latest/configuration/] |

### Package Legitimacy Audit

The GSD registry verifier does not support Go modules. The three proposed OTel additions were therefore checked through `go list -m -json`: each resolves to the official `open-telemetry/opentelemetry-go` repository and a tagged module version. No third-party idempotency, canonical-JSON, retention, or sampling package is necessary. [VERIFIED: Go module proxy/VCS metadata]

Prometheus, Grafana, and Tempo should be consumed only as official container images pinned by immutable digest. Resolve and record exact digests in the implementation task because they are deployment artifacts, not Go dependencies. [RECOMMENDATION]

## Architecture Patterns

### 1. Public operation registry over the existing audit ledger

Add an operational table such as `aura.idempotency_operations` in migration 0043+, owned by the existing `toolinvocations` store package. Keep `aura.tool_invocations` append-only as the internal execution/audit ledger. This separation permits 30-day replay-body expiry without destroying longer-lived audit facts and supports scheduler/CLI mutations that do not naturally have a conversation tuple. [RECOMMENDATION, grounded in D-01/D-05 and current append-only schema]

The registry key must be unique on `(identity_id, operation_scope, operation_key)`. Store a bounded canonical payload hash, state (`in_progress`, `completed`, `indeterminate`), internal audit linkage, typed replay result or sidecar reference, replay expiry, timestamps, and version/lease fields needed for conditional transitions. Derive identity from trusted auth/runtime context, never from a client-supplied identity field. [RECOMMENDATION]

Use this state machine:

```text
absent --atomic insert--> in_progress --conditional complete--> completed
                              |
                              +-- crash/ambiguous effect --> indeterminate (terminal)

same key + different fingerprint -> conflict
same key + fresh in_progress      -> in_progress + retry guidance
same key + completed/unexpired    -> replay exact typed result
same key + indeterminate          -> indeterminate; never auto-reinvoke
```

Normalize at the operation's typed boundary, serialize deterministically, then hash with SHA-256. Do not infer semantic equivalence from arbitrary raw JSON and do not use the transport request ID as the operation key. [CITED: https://docs.stripe.com/api/idempotent_requests; https://aws.amazon.com/builders-library/making-retries-safe-with-idempotent-APIs/]

Thread one `OperationContext` through HTTP/API, ToolGateway, scheduler, CLI, approval/resume, and MCP `_meta`. For downstreams that accept keys, forward the same logical key; otherwise the Aura reservation must be durable before effect. [CITED: https://modelcontextprotocol.io/specification/2025-11-25/basic/index]

### 2. Truthful readiness as a bounded aggregate

Create small snapshot interfaces for listener, migration, scheduler, and drain state. Run only the Postgres and Neo4j network probes concurrently beneath one `context.WithTimeout` shorter than three seconds. Collect stable component enums and emit a schema-versioned response; log detailed redacted causes separately. [RECOMMENDATION, grounded in D-06..D-09]

Bind the AG-UI socket synchronously with `net.Listen` before background workers start. Pass that listener to `http.Server.Serve`; any unexpected return must propagate to the top-level run group and terminate the process. Mark draining immediately on shutdown so readiness fails before work is torn down. [RECOMMENDATION]

Scheduler readiness should expose configured/running, last loop heartbeat, last successful DB-claim probe, draining, and the computed freshness result. Queue lag and individual job failures are metrics/alerts, not readiness gates. [RECOMMENDATION]

Readiness is for ability to serve; liveness is for whether the process should be restarted. A lost critical dependency should fail readiness without making `/healthz` fail. [CITED: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes]

### 3. One OTel MeterProvider, two readers

Extend `internal/obs` to construct a resource once, a Prometheus exporter reader, and an OTLP periodic reader, then attach both with repeated `sdkmetric.WithReader`. Set one global MeterProvider and return one bounded shutdown function for traces and metrics. [VERIFIED: OTel Go SDK v1.44 APIs; https://pkg.go.dev/go.opentelemetry.io/otel/sdk/metric]

Use a dedicated Prometheus registry for the OTel exporter. Existing direct collectors move behind a named, time-bounded compatibility adapter; each semantic metric has exactly one owner. Add a test that fails if legacy and OTel descriptor names overlap. [RECOMMENDATION]

Because production auth accepts only Aura session cookies and deliberately ignores `Authorization`, Prometheus cannot safely scrape the current gated `/metrics` route using a bearer secret. Serve the OTel Prometheus handler on a second metrics listener: loopback-only in local mode, and attached only to an `internal: true` Compose observability network in appliance mode, with no published host port. Keep the user-facing `/metrics` route gated or remove it after compatibility migration. [VERIFIED: `internal/agui/auth.go` and auth tests; RECOMMENDATION]

Define finite enums and explicit overflow budgets for every attribute. Good labels include operation class, bounded tool registry name, transport class, outcome class, scheduler job kind, retention class, DB system, and provider/model from configured allowlists. Fold unknown values to `other`. Never use identity, conversation/request/operation key, raw error, path, prompt, arguments, result, query text, or MCP-supplied arbitrary strings. [CITED: https://prometheus.io/docs/practices/instrumentation/]

Pin semconv `v1.39.0` and wrap it behind Aura-owned instrumentation helpers. Use reviewed GenAI duration/token instruments and a strict attribute allowlist; content remains opt-in/redacted and Phase 40 owns secret-like persistence enforcement. [VERIFIED: installed semconv module; CITED: https://github.com/open-telemetry/semantic-conventions-genai]

### 4. Observability as immutable appliance code

Recommended layout:

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

Grafana dashboard UIDs and panel IDs are APIs: alerts and runbooks link to them, so validate uniqueness and immutability. Set `allowUiUpdates: false`; provisioned files overwrite database state on restart. [CITED: https://grafana.com/docs/grafana/latest/administration/provisioning/]

CI must run rule syntax checks, `promtool test rules` behavior fixtures, JSON parsing/schema checks, UID and datasource uniqueness, a metric/query contract check against Aura's descriptor manifest, Compose config validation, and a provisioning smoke test. Use the pinned official containers because `promtool` and `grafana-server` are not installed locally. [VERIFIED: local environment; CITED: Prometheus/Grafana official docs]

Implement symptom/SLO recording rules first, then page alerts and component warnings. Exact burn windows and scheduler freshness belong in typed configuration with conservative defaults and tests; calibrate them against measured appliance baselines instead of burying constants in dashboard expressions. [RECOMMENDATION]

### 5. One two-phase retention policy engine

Create an `internal/retention` service with a pure evaluator/planner and store adapters. The scheduler calls `Apply`; the CLI calls the same engine but defaults to `DryRun`. Reuse the existing documents cleanup pattern: deterministic ordered plan, SHA-256 confirmation token, rerun/revalidate before apply, and explicit confirmation. [VERIFIED: `internal/documents/orphans.go`; RECOMMENDATION]

Claim bounded candidates in PostgreSQL with deterministic `ORDER BY`, `LIMIT`, and `FOR UPDATE SKIP LOCKED`. Mark them `deleting` with a lease and policy version, commit, revalidate durable activity evidence, delete external objects idempotently, then finalize metadata. Failure leaves a retryable, audited state. [CITED: https://www.postgresql.org/docs/current/sql-select.html; https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/]

Automatic retention must only skip protected conversations. It must never call the existing `DeleteConversationLifecycle`, because that owner-triggered path intentionally cancels work. Explicit owner delete/export-delete should reuse that ordered teardown after authorization and after any requested export has completed successfully. [VERIFIED: `internal/runner/runner_delete.go`; RECOMMENDATION]

Build durable active evidence from live turn leases, fresh worker heartbeat, pending pause/approval, queued/running scheduler work, background tool/sandbox work, and unfinished artifact operations. Recheck immediately before external deletion. Retain current trusted-ID path reconstruction plus `Lstat`/symlink rejection. [VERIFIED: existing filesystem safety patterns; RECOMMENDATION]

The export format should be a versioned manifest plus immutable payload entries: owner and conversation metadata, turns, referenced sidecars/assets, retention-policy version, sizes, and SHA-256 checksums. The snapshot must be owner-scoped and consistent; export-delete begins teardown only after archive finalization succeeds. [RECOMMENDATION]

Tempo owns backend OTel block retention through its compactor; Aura owns local full-content reasoning-trace and business-object retention. Do not have Aura delete Tempo blocks directly. [CITED: https://grafana.com/docs/tempo/latest/reference-tempo-architecture/components/compaction/]

### 6. Bounded learning stores

Replace `activelearn.seen sync.Map` with a bounded timestamped hash set: 100,000 hashes, 30-day TTL, no raw text, hard admission/eviction on write, and bounded batch compaction. A lock-protected heap/ring plus map is sufficient; keep compaction deterministic and expose size/drop/expiry metrics. [RECOMMENDATION, grounded in D-17]

For Neo4j examples, add `created_at`, `expires_at`, bucket identity, pinned/manual marker, quality, and novelty metadata while preserving the existing `MERGE` idempotency key. Every load query must have a deterministic order and a parameterized `LIMIT`; no call may materialize all examples. Use Cypher 5-compatible syntax and bounded `UNWIND` batches. [VERIFIED: current Neo4j v5 dependency; RECOMMENDATION]

Enforce the 90-day TTL, 512 per reasoning-tier/tool bucket, 10,000 global per store, newest 25% preservation, and quality/novelty-weighted reservoir selection both at write admission and in a bounded background compactor. Pinned/manual evaluation seeds live in a separate label or store and do not consume learned-example capacity. [RECOMMENDATION, grounded in D-17; CITED: https://www.cs.umd.edu/~samir/498/vitter.pdf]

## Representative Implementation Shapes

### Dual-reader MeterProvider

```go
promReader, err := otelprom.New(otelprom.WithRegisterer(registry))
if err != nil { return nil, err }

otlpExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(endpoint))
if err != nil { return nil, err }

meterProvider := sdkmetric.NewMeterProvider(
    sdkmetric.WithResource(resource),
    sdkmetric.WithReader(promReader),
    sdkmetric.WithReader(sdkmetric.NewPeriodicReader(otlpExporter)),
)
otel.SetMeterProvider(meterProvider)
```

This is the supported OTel Go composition shape; production code must add TLS/insecure policy, intervals, error wrapping, and ordered shutdown. [VERIFIED: OTel Go v1.44 API]

### Bounded retention claim

```sql
WITH candidates AS (
    SELECT id
    FROM aura.retention_items
    WHERE state = 'eligible' AND eligible_at <= now()
    ORDER BY eligible_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE aura.retention_items AS item
SET state = 'deleting', lease_until = $2, policy_version = $3
FROM candidates
WHERE item.id = candidates.id
RETURNING item.*;
```

The exact schema is discretionary, but deterministic order, parameterized bounds, locking, explicit state, and post-claim revalidation are required. [CITED: https://www.postgresql.org/docs/current/sql-select.html]

### Bounded Cypher read

```cypher
MATCH (e:ReasoningExample {tier: $tier})
WHERE e.expires_at > datetime() AND coalesce(e.pinned, false) = false
RETURN e
ORDER BY e.created_at DESC, e.content_hash ASC
LIMIT $limit
```

Every reasoning/tool-selection load needs an explicit bound and deterministic tie-breaker; enforce a validated maximum before passing `$limit`. [RECOMMENDATION]

## Do Not Hand-Roll

- Do not build a second independent retry ledger in each ingress. Use one operation registry and one typed context propagated across adapters. [RECOMMENDATION]
- Do not implement custom metric exposition or OTLP transport. Use the official OTel SDK/exporters and Prometheus reader. [VERIFIED: standard stack]
- Do not invent a dashboard deployment API. Use Grafana file provisioning and stable UIDs. [CITED: Grafana provisioning docs]
- Do not create a new filesystem traversal scheme. Reuse trusted-ID path reconstruction, `Lstat`, and no-symlink behavior. [VERIFIED: existing code]
- Do not create a second explicit-delete saga. Reuse `DeleteConversationLifecycle` for owner deletes only. [VERIFIED: existing code]
- Do not implement unbounded goroutine fan-out for readiness or compaction. Use bounded structured workers with context cancellation and joined shutdown. [VERIFIED: project Go conventions]
- Do not add a reservoir-sampling dependency; the bounded algorithm is small, testable, and described by the cited primary paper. [CITED: Vitter reservoir-sampling paper]

## Common Pitfalls

1. **Extending only the append-only invocation ledger.** That prevents independent replay-body expiry and excludes non-conversation mutations. Keep audit and operational replay state separate. [VERIFIED: schema/code constraints]
2. **A timeout per readiness probe.** Sequential three-second probes can exceed Compose's entire three-second client limit. Use one global budget and concurrency. [VERIFIED: current code/Compose]
3. **Logging a listener error and continuing.** This leaves a live process that cannot serve. Synchronous bind and top-level error propagation are required. [VERIFIED: current `serve.go` gap]
4. **Returning raw dependency strings from `/readyz`.** Even sanitized text is not a stable response contract. Return bounded typed codes; keep redacted detail in logs. [RECOMMENDATION]
5. **Dual-emitting legacy and OTel metrics.** A compatibility period must have single ownership per metric and an enforced removal date. [RECOMMENDATION]
6. **Using tool/model/server names directly as labels.** Sanitization does not bound cardinality. Use configured allowlists, budgets, and `other`. [VERIFIED: current gap; Prometheus guidance]
7. **Making `/metrics` public to simplify scraping.** The route is deliberately whole-origin gated. Use an internal-only listener/network and no published port. [VERIFIED: auth boundary]
8. **Treating queue lag as readiness.** Lag alerts separately; readiness fails only when scheduler progress stops beyond the hard threshold. [LOCKED: D-08]
9. **Deleting metadata before backing objects.** This loses the ownership map needed for retry. Mark, remove external objects, then finalize metadata. [CITED: finalizer pattern]
10. **Using owner-delete teardown in automatic cleanup.** It cancels active work, violating automatic retention's skip-only rule. [VERIFIED: current deletion behavior; LOCKED: D-16]
11. **Checking activity once.** A turn can start between planning and deletion. Persist evidence and revalidate immediately before every destructive batch. [RECOMMENDATION]
12. **`LIMIT` without deterministic order or hard admission.** Loads may be biased and the store can still grow without bound. Enforce both write and read caps. [RECOMMENDATION]
13. **Making Aura manage Tempo blocks.** Tempo compactor owns backend trace blocks; Aura owns only its local trace artifacts and configuration. [CITED: Tempo docs]
14. **Reusing migration 0026.** It is historical; scan the migration directory at implementation time and allocate 0043+. [VERIFIED: migration inventory]

## Runtime State Inventory

| State class | Existing state | Required action type |
|---|---|---|
| Stored PostgreSQL data | `aura.tool_invocations`, conversations/turns, pauses/approvals, scheduler jobs, asset metadata. | **Data migration:** add 0043+ operational idempotency and retention/activity/audit state without rewriting the append-only invocation history; backfill only metadata that has a safe deterministic source. [VERIFIED: schema inventory; RECOMMENDATION] |
| Stored Neo4j data | `ReasoningExample` and `ToolSelectionExample` nodes may lack timestamps/cap metadata. | **Data migration/online normalization:** tolerate legacy missing properties, stamp on touch, compact in bounded batches, and never run an unbounded startup rewrite. [VERIFIED: store queries; RECOMMENDATION] |
| Files/object data | Run-directory temp/crash sidecars, local reasoning traces, referenced sidecars, Garage assets, exports. | **Runtime policy transition:** classify existing objects conservatively, dry-run first, reconstruct paths from trusted IDs, and never delete an unclassified/reference-ambiguous object. [VERIFIED: storage topology; RECOMMENDATION] |
| Tempo data | Backend OTel blocks and compactor metadata. | **Configuration change:** set 14-day block retention in Tempo; no Aura data migration or direct block deletion. [CITED: Tempo docs] |
| Live service config | Compose services/networks/healthcheck, Prom rules, Grafana provisioning, Tempo config. | **Code/config/provisioning change:** add immutable files and recreate containers; existing Grafana UI edits are not authoritative. [CITED: Grafana provisioning docs] |
| OS-registered state | Docker images, networks, named volumes; no separate Windows/Linux service-manager registrations were identified in scope. | **Deployment recreation:** pull digest-pinned images and recreate affected services while preserving named data volumes. [VERIFIED: repository deployment audit] |
| Secrets/environment | OTel exporter settings, web-auth cookie secrets, future metrics listener address and deployment config. | **Additive configuration:** no secret rename is required; never put credentials in metric labels, dashboards, or checked-in Prometheus files. [VERIFIED: config/auth audit; RECOMMENDATION] |
| Build artifacts | Go binary/container, embedded web assets, generated dashboard/rule fixtures. | **Rebuild/revalidate:** rebuild Aura and observability containers; no user-data migration belongs in build output. [VERIFIED: build inventory] |

## Validation Architecture

### Test stack

Use Go's standard test tooling, table-driven named subtests, `-race`, existing integration build tags, `go vet`, repository lint, and `goleak` where worker lifecycle changes. Use `promtool` from the pinned Prometheus container, JSON/contract scripts for dashboards, and Docker Compose for provisioning smoke tests. [VERIFIED: `go.mod`, repository test conventions, local environment]

### Requirement-to-test map

| Requirement | Minimum automated evidence | Suggested location/status |
|---|---|---|
| OBS-01 | Port already occupied fails boot; unexpected listener exit reaches top-level failure; Compose healthcheck still targets `/readyz`. | Extend `cmd/aura` serve/container tests; listener lifecycle test seam is missing. [VERIFIED: test inventory] |
| OBS-02 | Concurrent probes obey one deadline; typed JSON is stable/sanitized; PG/Neo4j/schema/listener/scheduler/drain truth table; disabled scheduler healthy; `/healthz` stays 200. | Extend `internal/agui/readiness_test.go`; add scheduler snapshot unit tests and tagged DB integration tests. [VERIFIED: current coverage] |
| OBS-03 | One MeterProvider drives both readers; shutdown is bounded; spans/metrics exist for LLM/tool/MCP/pause-resume/DB/scheduler; forbidden/high-cardinality attributes rejected; legacy name overlap fails. | New `internal/obs/*_test.go` plus boundary-package tests; current tests cover only part of agent tracing. [VERIFIED: test inventory] |
| OBS-04 | `promtool check rules`; `promtool test rules` fixtures for firing/non-firing/debounce; dashboard JSON parse/schema; stable/unique UIDs; datasource/query metric contracts; provisioning smoke. | New `observability/**` and CI job; no current assets/tests. [VERIFIED: repository inventory] |
| OBS-05 | Dry-run changes nothing; apply token detects drift; bounded/non-overlap claims; activity exclusion races; external-delete failure retries without metadata loss; symlink/path defenses; owner export manifest/checksums; export-delete ordering; disk thresholds. | Extend conversations/documents/runner tests and add `internal/retention` unit + tagged integration tests. [VERIFIED: reusable tests and gaps] |
| OBS-06 | Exact TTL/cap boundaries; newest 25%; deterministic seeded selection; pinned exclusion; hard write admission; bounded loads; batch size; cancellation/no goroutine leak; metric outcomes. | Extend `activelearn`, `reasoningstore`, `toolselectstore`; add Neo4j-tagged integration coverage. [VERIFIED: current gaps] |

### Fast and full commands

```powershell
# Fast package loop
go test ./internal/obs ./internal/agui ./internal/gateway ./internal/toolinvocations ./internal/retention ./internal/activelearn ./internal/reasoningstore ./internal/toolselectstore

# Lifecycle/concurrency gate
go test -race ./internal/obs ./internal/agui ./internal/cron ./internal/retention ./internal/activelearn

# Repository gate
go test ./...
go vet ./...

# Observability-as-code gate, using the exact pinned image from Compose
docker compose run --rm prometheus promtool check rules /etc/prometheus/rules/*.yml
docker compose run --rm prometheus promtool test rules /etc/prometheus/tests/*.yml
docker compose config
```

The exact repository lint and integration commands should be copied from `.github/workflows/ci.yml` into the plan rather than approximated. A skipped integration dependency must report skipped, not green. [VERIFIED: project validation conventions]

### Wave 0 test-enablement work

Before feature tasks, plan these seams: listener factory/error channel, injectable readiness clock and global deadline, scheduler readiness snapshot, OTel descriptor/attribute recorder, retention adapters and deterministic clock/ID source, learning-store clock/random source, Prometheus rule fixtures, and Grafana asset validator. These are testability contracts, not optional cleanup. [RECOMMENDATION]

## Security Domain

Phase 39 is security-sensitive because it accepts replay identifiers, exposes operational state, exports/deletes owner data, and destroys cross-store objects. Apply ASVS Level 1 with the configured high-severity threshold; the following controls are acceptance criteria. [VERIFIED: phase security profile and code boundaries]

| Threat/control domain | Required control and test |
|---|---|
| Cross-identity replay / BOLA (V4, V13) | Scope registry uniqueness to the authenticated identity obtained from server context. A key created by identity A must be absent/conflicting—not replayable—to B. Test same key across two principals. |
| Key reuse with changed intent (V5, V11) | Normalize typed arguments, hash them, and return a typed conflict on mismatch. Test order-equivalent inputs and materially changed inputs. |
| Duplicate race / double effect (V11) | Atomic unique insert plus conditional state transition; run concurrent reservation integration tests and assert one effect owner. |
| Ambiguous downstream outcome (V11) | Terminal `indeterminate`; never automatically reinvoke. Test crash after effect-before-complete and reconciler behavior. |
| Cardinality/resource exhaustion (V5, V7) | Validate key lengths, bound replay bodies, allowlist labels, fold overflow to `other`, and test adversarial unique strings. |
| Telemetry disclosure (V7, V8, V9) | No content/identity/request/path/raw-error labels; trace content opt-in/redacted; internal-only metrics network with no host publish; OTLP TLS/auth follows deployment policy. Test descriptors and Compose exposure. |
| Readiness information leak (V7) | Public response has only typed bounded status/codes; detailed causes stay in redacted logs. Fuzz error strings containing credentials/paths. |
| Destructive race / TOCTOU (V4, V11) | Owner authorization for explicit actions; durable activity evidence for automatic policy; revalidate after claim and before delete. Test activity appearing between dry-run/claim/delete. |
| Path traversal/symlink attack (V5, V12) | Reconstruct from trusted IDs, reject traversal and symlinks with `Lstat`, never trust stored/user paths directly. Preserve existing adversarial tests. |
| Partial deletion / repudiation (V7, V8, V11) | Mark state and append audit before external effects; backing-object-first; record policy/counts/bytes/failures; idempotent retry. Test every failure boundary. |
| Export confidentiality/integrity (V4, V8) | Owner-scoped consistent snapshot, no hidden backup for delete, versioned manifest, per-entry sizes/checksums, safe archive names, bounded streaming. Test cross-owner denial and archive traversal. |
| Unsafe operational configuration (V5, V14) | Validate retention durations/caps/disk thresholds/readiness windows at boot; invalid config is fatal. Never silently coerce a dangerous value. |

Phase 40—not this phase—owns fail-fast enforcement for secret-like full-trace persistence. Phase 39 must preserve the explicit opt-in/redaction seam and must not broaden stored trace content. Backup/DR remains Phase 41. [LOCKED: phase boundary]

## Planning Sequence and Dependencies

1. **Foundation:** allocate 0043+ and define shared typed operation, readiness, metric-attribute, retention-policy/activity, and learning-cap contracts plus Wave 0 test seams. [RECOMMENDATION]
2. **Idempotency:** implement registry/store state machine, then propagate through each mutating ingress and reconcile crash/expiry behavior. [RECOMMENDATION]
3. **Serving truth:** fix synchronous listener ownership, schema-head state, scheduler progress snapshots, and concurrent readiness aggregation. [RECOMMENDATION]
4. **Telemetry core:** build the dual-reader provider and bounded instrument catalog, then instrument LLM/tool/MCP/pause-resume/DB/scheduler/listener/retention. [RECOMMENDATION]
5. **Observability assets:** add recording rules before alerts, dashboards/runbooks after metric contracts, then CI and Compose provisioning. [RECOMMENDATION]
6. **Retention/export:** add policy/claim/audit engine, adapters, CLI/sweeper, active evidence, export, and explicit delete integration. [RECOMMENDATION]
7. **Learning bounds:** update in-memory seen set and both Neo4j stores, then compaction/metrics/integration tests. [RECOMMENDATION]
8. **Cross-cutting gates:** race/leak tests, ASVS cases, containerized rule/provision tests, migration-head verification, and requirement traceability. [RECOMMENDATION]

Do not put idempotency propagation before the registry contract, dashboards before metric descriptors, or deletion adapters before durable claim/activity state. [RECOMMENDATION]

## Environment and Tool Availability

The workstation has Go 1.26.5, Docker Engine 29.6.1, Docker Compose 5.3.0, Node 24.16, npm 11.17, `jq`, and WSL2 Ubuntu. `promtool` and `grafana-server` are not installed directly, so the plan must make pinned containers the supported local/CI validation route. [VERIFIED: local command audit, 2026-07-21]

No environment blocker was found. Exact Prometheus/Grafana/Tempo image tags and digests must be resolved against official registries at implementation time and recorded in Compose/CI. [VERIFIED: environment; RECOMMENDATION]

## Open Decisions for the Planner

These are implementation-detail decisions within the granted discretion, not user blockers:

- Choose exact table/package/config names while retaining the architecture above. [LOCKED: agent discretion]
- Set conservative default scheduler/readiness and burn-rate windows, expose overrides, and add rule tests; include a post-deploy baseline-calibration note. [LOCKED: agent discretion]
- Resolve immutable image digests and the exact CI container invocation. [RECOMMENDATION]
- Select a deterministic quality/novelty formula and seeded reservoir implementation; document it in tests so compaction is reproducible. [LOCKED: agent discretion]

## Assumptions Log

No behavior-changing assumption was required. Repository facts were verified against the current worktree; external technical guidance came from fresh research-cache entries and primary documentation. Migration head and container digests are explicitly revalidation tasks because they may change before execution. [VERIFIED: research process]

## Sources and Provenance

### Primary repository evidence

- `.planning/phases/39-idempotency-observability-pack/39-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, `.planning/STATE.md`. [VERIFIED]
- `cmd/aura/serve.go`, `compose.yaml`, `internal/agui/readiness.go`, `internal/agui/auth.go`, readiness/auth/container tests. [VERIFIED]
- `internal/obs/init.go`, `internal/agent/metrics.go`, `internal/agent/tracing.go`, AG-UI and panic collectors. [VERIFIED]
- `internal/gateway/reserve.go`, `internal/gateway/reconcile.go`, `internal/toolinvocations/store_reserve.go`, migration files through `0042_drop_compaction`. [VERIFIED]
- `internal/conversations/orphan_scan.go`, `internal/conversations/sweeper.go`, `internal/documents/orphans.go`, `internal/runner/runner_delete.go`, `internal/agui/share_export.go`, `internal/reasoningtrace/reasoningtrace.go`. [VERIFIED]
- `internal/activelearn/learner.go`, `internal/reasoningstore/store.go`, `internal/toolselectstore/store.go`, `go.mod`, `.github/workflows/ci.yml`. [VERIFIED]

### Primary external references

- OTel Go SDK/readers: https://pkg.go.dev/go.opentelemetry.io/otel/sdk/metric, https://pkg.go.dev/go.opentelemetry.io/otel/exporters/prometheus, https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc. [CITED]
- GenAI semantic conventions: https://opentelemetry.io/docs/specs/semconv/, https://github.com/open-telemetry/semantic-conventions-genai, https://github.com/open-telemetry/semantic-conventions/blob/main/docs/gen-ai/gen-ai-metrics.md. [CITED]
- Prometheus naming/instrumentation/alerts/rules/tests: https://prometheus.io/docs/practices/naming/, https://prometheus.io/docs/practices/instrumentation/, https://prometheus.io/docs/practices/alerting/, https://prometheus.io/docs/practices/rules/, https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules/. [CITED]
- Grafana provisioning: https://grafana.com/docs/grafana/latest/administration/provisioning/. [CITED]
- Tempo retention/compaction: https://grafana.com/docs/tempo/latest/configuration/, https://grafana.com/docs/tempo/latest/reference-tempo-architecture/components/compaction/. [CITED]
- Idempotency: https://docs.stripe.com/api/idempotent_requests, https://docs.stripe.com/api-v2-overview, https://aws.amazon.com/builders-library/making-retries-safe-with-idempotent-APIs/, https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ECS_Idempotency.html, https://google.aip.dev/155. [CITED]
- Readiness/Compose: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes, https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/, https://docs.docker.com/compose/how-tos/startup-order/. [CITED]
- Retention primitives: https://www.postgresql.org/docs/current/sql-select.html, https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/, https://www.cs.umd.edu/~samir/498/vitter.pdf, https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/. [CITED]

### Research seam audit

| Question | Provider | Cache state | Confidence | Result used |
|---|---|---|---|---|
| One MeterProvider with OTLP + Prometheus readers | Context7 | Fresh hit | MEDIUM | Repeated `WithReader`, official exporters, bounded shutdown. |
| GenAI semconv stability/pinning | Context7 | Fresh hit | MEDIUM | Convention is developmental; pin reviewed version and gate content. |
| Prometheus rules/cardinality/promtool | Context7 | Fresh hit | MEDIUM | Bounded labels, recording rules, syntax plus behavioral rule tests. |
| Grafana provisioning/stable UIDs | Context7 | Fresh hit | MEDIUM | File provisioning, stable UIDs, `allowUiUpdates: false`. |
| Readiness/liveness semantics | Primary documentation cache | Fresh hit | MEDIUM | Readiness removes traffic; liveness controls restart. |
| Idempotency behavior | Primary documentation web cache | Fresh hit | MEDIUM after verification | Caller key, replay, mismatch, ambiguous outcome, 30-day precedent. |
| PostgreSQL bounded claims | Primary documentation cache | Fresh hit | MEDIUM | Deterministic `SKIP LOCKED` queue-like claims. |
| Tempo retention ownership | Primary documentation cache | Fresh hit | MEDIUM | Backend compactor owns block retention. |

All eight research-plan questions were fresh cache hits on 2026-07-21; no cache write or redundant external fetch was needed. No prompt-like instructions from retrieved content were followed. The project knowledge graph was 156 commits stale, so it was used only to locate candidates; every material conclusion was checked against the live tree. [VERIFIED: research cache/graph audit]

## Confidence and Refresh Window

- Repository/current-state findings: **HIGH**, valid until the worktree or migration head changes. [VERIFIED]
- Architecture recommendations: **HIGH**, because they directly satisfy locked decisions and reuse verified seams. [VERIFIED]
- OTel Go module versions/origins: **HIGH**, verified through module metadata on 2026-07-21. [VERIFIED]
- GenAI semantic-convention pin: **MEDIUM**, refresh immediately before implementation because the convention remains developmental. [CITED: official GenAI convention repository]
- Prometheus/Grafana/Tempo deployment versions: **MEDIUM**, refresh and pin exact official image digests before execution. [RECOMMENDATION]

**Recommended research refresh date:** 2026-08-20, or immediately after any OTel/module/migration/deployment-stack update.
