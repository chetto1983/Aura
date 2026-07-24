# Aura readiness and serving runbook

Applies to `AuraServiceUnavailable`, `AuraListenerFailure`, `AuraSchedulerNoProgress`, `AuraResumeFailures`, `AuraDBErrorRateHigh`, and `AuraDBSaturation`.

## Meaning

These alerts cover the core serving contract: Aura scrape/listener reachability, scheduler progress, durable resume, and PostgreSQL boundaries. Paging alerts indicate sustained user impact. Database error and saturation alerts are warnings so an operator can remove the component cause before it becomes an availability event.

## Drilldown and correlation

Open the linked panel on dashboard `aura-overview`. Filter only by catalog-owned bounded dimensions such as `operation`, `outcome`, `error_class`, and `state`; never add identity, conversation, request, tool-call, operation-key, URL, path, or raw-error labels. Compare the alert start with `/readyz` codes (`postgres_unavailable`, `neo4j_unavailable`, `listener_unavailable`, `migration_incompatible`, `scheduler_stalled`, `draining`) and then use Tempo for traces in the same time range. Trace IDs stay in traces and logs, not metric labels.

## Immediate safe actions

1. Query `/healthz` and `/readyz` from the Aura container namespace. Liveness 200 plus readiness 503 means the process is alive but should receive no work.
2. Check the Aura, PostgreSQL, Neo4j, and scheduler container health without restarting all services at once.
3. For migration mismatch, stop serving traffic and run the repository migration command with the migrate role. Do not let `aura serve` apply migrations.
4. For scheduler no-progress, confirm it is enabled and not intentionally draining. Preserve the durable queue; do not delete or replay jobs manually.
5. For resume failures, preserve pause and operation records before retrying. Never reinvoke a terminal `indeterminate` mutation.
6. For DB pressure, pause optional batch work and retention sweeps before changing connection limits.

## Escalation

Escalate immediately if two core dependencies fail together, a listener exits unexpectedly, a migration is dirty/ahead/behind, resume failures persist for ten minutes, or database in-flight work remains above 50 after optional workers are paused. Include timestamps, bounded reason/error classes, deployment revision, and trace links. Do not include credentials, DSNs, prompts, arguments, or results.

## Recovery evidence

Close the incident only after `/readyz` is stable 200 for two healthcheck intervals, listener and scheduler progress panels advance, resume failures stop increasing, DB error ratio stays below five percent, DB in-flight work is below 50, and a fresh synthetic read-only turn completes. Record the last firing time and the trace link used for confirmation.
