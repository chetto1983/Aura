-- name: InsertCacheMetric :exec
-- Idempotent on (conversation_id, seq): the metric write is a separate, non-transactional
-- observation following the assistant turn (runner_persist.go). ON CONFLICT DO NOTHING
-- makes a re-run for an already-recorded turn a no-op rather than a PK violation or a
-- duplicate, so a retry after a transient failure can never double-count the metric (WR-03).
INSERT INTO aura.cache_metrics (conversation_id, seq, prompt_tokens, cached_tokens, cost_usd)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (conversation_id, seq) DO NOTHING;

-- name: ListCacheMetricsSince :many
SELECT conversation_id, seq, ts, prompt_tokens, cached_tokens, cost_usd
FROM aura.cache_metrics
WHERE ts >= sqlc.arg(since)::timestamptz
ORDER BY ts ASC;

-- name: AggregateCacheMetricsSince :one
SELECT count(*)                          AS turns,
       coalesce(sum(prompt_tokens), 0)   AS total_prompt_tokens,
       coalesce(sum(cached_tokens), 0)   AS total_cached_tokens,
       coalesce(sum(cost_usd), 0)        AS total_cost_usd
FROM aura.cache_metrics
WHERE ts >= sqlc.arg(since)::timestamptz;

-- name: SumIdentitySpendSince :one
-- Per-identity period spend (CRED-06): sums aura.cache_metrics.cost_usd, joined to
-- aura.conversations for the identity scope none of the three queries above needs.
-- Callers MUST run this through internal/db.WithIdentityTx(ctx, pool, identityID, ...)
-- scoped to the SUBJECT identity of the read, never the caller -- migration 0032's
-- conversations_owner_isolation RLS policy filters the join to app.current_identity,
-- and an admin's OWN identity in that session var would silently lose every row
-- belonging to the identity being inspected (mirrors internal/agui/audit_store.go's
-- ListActivityForIdentity doc comment: "a read scoped to the caller while asking about
-- the subject silently loses the joined rows"). Returns an exact decimal zero for an
-- identity with no rows (coalesce), matching AggregateCacheMetricsSince's own
-- convention -- never a null read as an error, and never rounded here: the caller
-- (internal/agui/credit_ledger.go) reads this at full stored scale and rounds only at
-- the display boundary.
SELECT coalesce(sum(cm.cost_usd), 0) AS total_spend
FROM aura.cache_metrics cm
JOIN aura.conversations c ON c.id = cm.conversation_id
WHERE c.identity_id = sqlc.arg(identity_id)::uuid
  AND cm.ts >= sqlc.arg(since)::timestamptz;
