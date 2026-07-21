# Phase 39: Idempotency + Observability Pack - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-21
**Phase:** 39-idempotency-observability-pack
**Areas discussed:** Idempotency boundaries, Readiness semantics, Telemetry and alerts, Retention and cleanup

---

## Idempotency Boundaries

### Mutation identity

| Option | Description | Selected |
|--------|-------------|----------|
| Caller-stable operation key + payload fingerprint | Scope caller intent by identity and tool/operation; preserve the Phase-35 tuple as internal execution identity; reject key reuse with changed arguments. | ✓ |
| Keep the Phase-35 tuple only | Treat conversation_id, request_id, and tool_call_id as both transport and logical mutation identity. | |
| Derive identity from normalized arguments | Generate idempotency identity entirely from the mutation payload. | |

**User's choice:** Caller-stable operation key + payload fingerprint.
**Notes:** User accepted the researched industrial recommendation without changes.

### Enforcement and propagation

| Option | Description | Selected |
|--------|-------------|----------|
| Every mutating surface, propagated end-to-end | Apply to API, agent tools, scheduler, CLI, approvals/resumes, and MCP metadata when supported; use Aura's ledger for legacy downstream systems. | ✓ |
| Aura boundary only | Deduplicate at Aura without carrying intent to downstream services. | |
| Agent tool calls only | Protect only LLM-issued tool mutations. | |

**User's choice:** Every mutating surface, propagated end-to-end when supported.
**Notes:** No additional qualification.

### Duplicate while executing

| Option | Description | Selected |
|--------|-------------|----------|
| Immediate typed in_progress outcome | Return retry guidance immediately, replay completed results, and make ambiguous/orphaned operations terminal indeterminate without reinvocation. | ✓ |
| Bounded follower wait | Hold duplicate callers briefly for the original result before returning in-progress. | |
| Keep the current benign placeholder | Continue returning the reservation-held preview that can resemble a successful tool result. | |

**User's choice:** Immediate typed in_progress outcome.
**Notes:** No additional qualification.

### Replay lifetime

| Option | Description | Selected |
|--------|-------------|----------|
| Operation lifetime plus 30 days | Keep full replay protection through completion plus 30 days; retain smaller audit metadata longer. | ✓ |
| Fixed 24 hours | Expire every key one day after first use. | |
| Conversation or job lifetime | Retain only while the parent conversation/job exists. | |

**User's choice:** Operation lifetime plus 30 days.
**Notes:** No additional qualification.

---

## Readiness Semantics

### Fatal versus unready

| Option | Description | Selected |
|--------|-------------|----------|
| Fatal boot invariants; runtime dependencies become unready | Exit for invalid config, migration, bind, or listener-loop failure; keep liveness and return readyz 503 for runtime dependency loss. | ✓ |
| Readiness-only for every failure | Keep the process alive even when boot invariants cannot be established. | |
| Fatal on every critical failure | Exit whenever a critical runtime dependency becomes unavailable. | |

**User's choice:** Fatal boot invariants; runtime dependencies become unready.
**Notes:** No additional qualification.

### Readiness dependency set

| Option | Description | Selected |
|--------|-------------|----------|
| Core serving contract only | Gate PostgreSQL, Neo4j, schema-at-head, listener, and enabled scheduler; degrade optional integrations at feature level. | ✓ |
| Every configured dependency | Make any configured provider or sidecar failure fail global readiness. | |
| Minimal HTTP contract | Gate only that the HTTP listener can answer. | |

**User's choice:** Core serving contract only.
**Notes:** No additional qualification.

### Scheduler readiness

| Option | Description | Selected |
|--------|-------------|----------|
| Progress-based health | Require a running loop, fresh heartbeat/tick, working DB claim path, and non-draining state; alert separately on job failure/lag. | ✓ |
| Backlog-sensitive health | Fail readiness whenever queued work or lag crosses a threshold. | |
| Goroutine-presence only | Treat scheduler as ready whenever its worker goroutine exists. | |

**User's choice:** Progress-based health.
**Notes:** Disabled scheduler is healthy by configuration.

### Probe execution and reporting

| Option | Description | Selected |
|--------|-------------|----------|
| Parallel, globally budgeted aggregate | Run DB probes concurrently below the Compose client deadline; use in-process snapshots and stable sanitized typed JSON. | ✓ |
| Sequential live probes | Keep one live dependency probe after another. | |
| Cached snapshot only | Avoid all live probes and report cached component status. | |

**User's choice:** Parallel, globally budgeted aggregate.
**Notes:** Current truth is immediate; Compose retries provide debounce.

---

## Telemetry and Alerts

### Canonical instrumentation path

| Option | Description | Selected |
|--------|-------------|----------|
| OpenTelemetry-native with Prometheus reader | Use one MeterProvider for OTLP and Prometheus, pin GenAI conventions, and migrate existing collectors through a bounded compatibility window. | ✓ |
| Separate native Prometheus and OTel metrics | Maintain two permanent instrumentation paths. | |
| OpenTelemetry export only | Remove the local Prometheus scrape surface. | |

**User's choice:** OpenTelemetry-native canonical path with Prometheus reader.
**Notes:** Permanent duplicate instrumentation is explicitly disallowed.

### Labels and trace content

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded metrics; metadata-only traces by default | Use controlled dimensions and budgets, prohibit identity/raw-content labels, and require opt-in redacted content capture. | ✓ |
| Full diagnostic dimensions | Put request, user, path, error, and content-like dimensions directly into telemetry. | |
| Aggregate-only telemetry | Remove most component/provider/tool dimensions. | |

**User's choice:** Bounded metrics and metadata-only traces by default.
**Notes:** Overflow dimensions fold into other.

### Alert policy

| Option | Description | Selected |
|--------|-------------|----------|
| SLO- and symptom-based two-tier alerts | Page sustained user impact; warn/ticket on component causes; use debounce, burn rates, dashboards, and runbooks. | ✓ |
| Page on every component threshold | Send urgent pages for individual dependency and component thresholds. | |
| Dashboard only | Ship visual monitoring without alert delivery. | |

**User's choice:** SLO- and symptom-based two-tier alerting.
**Notes:** User accepted the Prometheus/Google SRE recommendation.

### Dashboard/rule management

| Option | Description | Selected |
|--------|-------------|----------|
| GitOps observability pack with CI validation | Version rules and provisioned dashboards with stable UIDs; validate syntax, queries/contracts, JSON, and provisioning. | ✓ |
| Dashboard JSON with syntax checks only | Check in dashboards but leave alert/query/provisioning behavior weakly validated. | |
| UI-managed dashboards and alerts | Configure production observability through mutable Grafana UI state. | |

**User's choice:** GitOps observability pack with CI validation.
**Notes:** Alerts link to exact panels and runbooks.

---

## Retention and Cleanup

### Cleanup execution safety

| Option | Description | Selected |
|--------|-------------|----------|
| Unified two-phase policy engine | Automatic policy sweeper plus dry-run-first CLI; mark/delete/finalize, bounded claims, no overlap, revalidation, retry, and audit. | ✓ |
| Dry-run-only operator cleanup | Require manual review and execution for every cleanup. | |
| Independent best-effort jobs | Let each artifact type delete independently without one lifecycle contract. | |

**User's choice:** Unified two-phase policy engine with automatic sweeper and dry-run-first CLI.
**Notes:** Informed by agent-memory, LibreChat, Kubernetes finalizers, and PostgreSQL worker-claim patterns.

### Artifact lifetimes

| Option | Description | Selected |
|--------|-------------|----------|
| Class-based lifecycle preserving canonical data | 24h temp/orphans, 24h production full traces, 7d trusted-development full traces, 14d metadata traces; referenced artifacts follow conversation lifetime. | ✓ |
| One global TTL | Delete every artifact class after the same age. | |
| Disk-size-only eviction | Retain until disk pressure and then evict by size/age. | |

**User's choice:** Class-based lifecycle with canonical conversation data preserved.
**Notes:** Conversations are unlimited by default; disk warnings at 70%, urgent alerts at 80%, and optional debug-trace shedding at 85%.

### Active exclusion and export/delete

| Option | Description | Selected |
|--------|-------------|----------|
| Durable activity evidence + consistent export | Protect leases, heartbeats, pauses, jobs, and artifact work; automatic cleanup skips; explicit delete uses ordered teardown; export is checksummed and snapshot-consistent. | ✓ |
| Status and timestamp only | Decide activity only from conversation status/last_active_at. | |
| In-memory checks only | Use the local process session registry without a durable cross-process lease. | |

**User's choice:** Durable activity evidence plus consistent export and ordered delete.
**Notes:** Combined export-delete requires successful export first; plain delete creates no hidden backup.

### Learning-store caps

| Option | Description | Selected |
|--------|-------------|----------|
| Hybrid TTL, hard caps, recency, and diversity reservoir | Bound seen hashes, per-tier/tool examples, global store size, loads, and compaction while retaining recent and diverse samples. | ✓ |
| TTL only | Expire by age with no count or load bound. | |
| Count cap with oldest-first eviction | Enforce size but discard strictly by age without diversity/quality preservation. | |

**User's choice:** Hybrid TTL, hard caps, recency, and diversity reservoir.
**Notes:** Locked defaults: seen 100,000 hashes/30d; examples 90d, 512 per tier/tool, 10,000 per store; newest 25% preserved.

---

## the agent's Discretion

- Exact schema, package, command, and configuration names.
- Exact heartbeat, alert burn-rate, and debounce windows, based on measured baselines.
- The reviewed OpenTelemetry GenAI semantic-convention version to pin.
- The deterministic quality/novelty scoring and compaction mechanics within the locked caps and recency share.
- The next real migration number. The roadmap's 0026 label is stale; the current repository reaches 0042.

## Deferred Ideas

None. Secret-like trace-persistence enforcement/full-trace production fail-fast remains Phase 40; backup/DR remains Phase 41.
