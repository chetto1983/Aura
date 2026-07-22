# Aura retention and learning-capacity runbook

Applies to `AuraRetentionFailures`, `AuraRetentionBacklog`, `AuraRetentionDiskWarning`, `AuraRetentionDiskUrgent`, `AuraRetentionTraceSuppression`, and `AuraLearningCapacityPressure`.

## Meaning

Retention failures and backlog indicate the two-phase mark/remove/finalize policy is not converging. Disk alerts implement the locked 70/80/85 percent boundaries: warning, urgent operator action, then suppression of optional full/debug trace creation. Learning pressure reports the 10,000 learned-example store cap; pinned/manual evaluation seeds are outside this eviction budget.

A missing retention-backlog series means the PostgreSQL observation failed or Aura is not scrape-reachable. Treat it as unknown, never as an empty backlog; restore database/scrape health before using the backlog threshold for recovery decisions.

Tempo exclusively owns its trace blocks and 14-day compaction. Aura cleanup must never delete Tempo blocks.

## Drilldown and correlation

Open `aura-data-retention` and use only bounded `operation`, `outcome`, `error_class`, `state`, and `tool_class` dimensions. Compare failure/backlog growth with disk utilization and learned-store size/age. Use Tempo spans for the matching `retention_plan`, `retention_apply`, `retention_delete`, or `learning_compact` time range; never promote file paths, object keys, identities, conversation IDs, or content hashes into metric labels.

## Immediate safe actions

1. At 70 percent, inspect growth and confirm scheduled cleanup is progressing.
2. At 80 percent, pause optional producers/compaction and run the cleanup CLI in dry-run mode. Review the deterministic plan/token before apply.
3. At 85 percent, verify optional full/debug trace creation is disabled. Do not emergency-delete active, canonical, referenced, or classification-ambiguous data.
4. For failed cleanup, preserve the durable deleting claim and retry backing-object removal before metadata finalization.
5. Revalidate owner and active-conversation evidence immediately before deletion; automatic retention skips activity and never cancels work.
6. For learning pressure, run bounded compaction and preserve TTL, newest-25-percent, quality/novelty selection, and pinned-seed exclusion.
7. Leave Tempo volume management to Tempo's compactor.

## Escalation

Escalate if disk remains at or above 80 percent for fifteen minutes, reaches 85 percent, cleanup failures repeat after a safe retry, backlog exceeds 100, a referenced/active item appears in a deletion plan, or a learned store cannot converge under its hard cap. Include policy version, counts, bytes, bounded error classes, and trace links without content or ownership identifiers.

## Recovery evidence

Require disk below 70 percent, backlog below 100 and decreasing, no new retention failures, all claimed items either finalized or safely retryable, learned stores below 10,000 with bounded loads, and Tempo reporting healthy compaction. Re-run dry-run and prove it is stable/idempotent before closing the incident.
