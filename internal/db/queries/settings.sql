-- name: UpsertSetting :one
INSERT INTO aura.settings (key, value, is_secret, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE
SET value      = EXCLUDED.value,
    is_secret  = EXCLUDED.is_secret,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING key, value, is_secret, updated_at, updated_by;

-- name: ListSettings :many
SELECT key, value, is_secret, updated_at, updated_by
FROM aura.settings
ORDER BY key;

-- name: GetSetting :one
SELECT key, value, is_secret, updated_at, updated_by
FROM aura.settings
WHERE key = $1;

-- name: DeleteSetting :exec
DELETE FROM aura.settings WHERE key = $1;

-- name: InsertBenchmarkSettingsOverride :one
INSERT INTO aura.benchmark_settings_overrides (
    run_id, owner_id, state, lease_expires_at, heartbeat_at, started_at, updated_at
) VALUES (
    sqlc.arg(run_id), sqlc.arg(owner_id), 'active',
    clock_timestamp() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
    clock_timestamp(), clock_timestamp(), clock_timestamp()
)
RETURNING run_id, owner_id, state, lease_expires_at, heartbeat_at,
          started_at, updated_at, completed_at, recovery_count;

-- name: CaptureBenchmarkSetting :exec
INSERT INTO aura.benchmark_settings_override_rows (
    run_id, key, existed, value, is_secret, updated_at, updated_by
)
SELECT sqlc.arg(run_id), requested.key, current.key IS NOT NULL,
       current.value, current.is_secret, current.updated_at, current.updated_by
FROM (SELECT sqlc.arg(key)::text AS key) AS requested
LEFT JOIN aura.settings AS current ON current.key = requested.key;

-- name: UpsertBenchmarkSetting :exec
INSERT INTO aura.settings (key, value, is_secret, updated_at, updated_by)
VALUES (sqlc.arg(key), sqlc.arg(value), false, clock_timestamp(), sqlc.arg(updated_by))
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    is_secret = false,
    updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by;

-- name: GetOpenBenchmarkSettingsOverride :one
SELECT run_id, owner_id, state, lease_expires_at, heartbeat_at,
       started_at, updated_at, completed_at, recovery_count
FROM aura.benchmark_settings_overrides
WHERE state IN ('active', 'restoring')
ORDER BY started_at
LIMIT 1
FOR UPDATE;

-- name: BenchmarkSettingsOverrideExpired :one
SELECT lease_expires_at <= clock_timestamp()
FROM aura.benchmark_settings_overrides
WHERE run_id = $1;

-- name: CountBenchmarkSettingsOverrideRows :one
SELECT count(*)::integer
FROM aura.benchmark_settings_override_rows
WHERE run_id = $1;

-- name: MarkBenchmarkSettingsOverrideRestoring :execrows
UPDATE aura.benchmark_settings_overrides
SET state = 'restoring', updated_at = clock_timestamp()
WHERE run_id = $1 AND state IN ('active', 'restoring');

-- name: RestoreBenchmarkSetting :exec
WITH original AS (
    SELECT saved.key, saved.existed, saved.value, saved.is_secret,
           saved.updated_at, saved.updated_by
    FROM aura.benchmark_settings_override_rows AS saved
    WHERE saved.run_id = sqlc.arg(run_id)
      AND saved.key = sqlc.arg(setting_key)
),
deleted AS (
    DELETE FROM aura.settings AS setting
    USING original
    WHERE setting.key = original.key AND NOT original.existed
)
INSERT INTO aura.settings (key, value, is_secret, updated_at, updated_by)
SELECT key, value, is_secret, updated_at, updated_by
FROM original
WHERE existed
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    is_secret = EXCLUDED.is_secret,
    updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by;

-- name: CountBenchmarkSettingsRestoreMismatches :one
SELECT count(*)::integer
FROM aura.benchmark_settings_override_rows AS original
FULL OUTER JOIN aura.settings AS current
    ON current.key = original.key
   AND current.key IN (
        'AURA_LLM_PROVIDER', 'AURA_LLM_MODEL', 'AURA_LLM_BASE_URL'
   )
WHERE original.run_id = $1
  AND (
      original.existed IS DISTINCT FROM (current.key IS NOT NULL)
      OR (
          original.existed
          AND (
              original.value IS DISTINCT FROM current.value
              OR original.is_secret IS DISTINCT FROM current.is_secret
              OR original.updated_at IS DISTINCT FROM current.updated_at
              OR original.updated_by IS DISTINCT FROM current.updated_by
          )
      )
  );

-- name: CompleteBenchmarkSettingsOverride :execrows
UPDATE aura.benchmark_settings_overrides
SET state = sqlc.arg(state),
    completed_at = clock_timestamp(),
    updated_at = clock_timestamp(),
    recovery_count = CASE WHEN sqlc.arg(state)::text = 'recovered' THEN 1 ELSE 0 END
WHERE run_id = sqlc.arg(run_id)
  AND state = 'restoring'
  AND sqlc.arg(state)::text IN ('completed', 'recovered');

-- name: HeartbeatBenchmarkSettingsOverride :execrows
UPDATE aura.benchmark_settings_overrides
SET heartbeat_at = clock_timestamp(),
    lease_expires_at =
        clock_timestamp() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
    updated_at = clock_timestamp()
WHERE run_id = sqlc.arg(run_id) AND state = 'active';
