-- name: InsertCacheMetric :exec
INSERT INTO aura.cache_metrics (conversation_id, seq, prompt_tokens, cached_tokens, cost_usd)
VALUES ($1, $2, $3, $4, $5);

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
