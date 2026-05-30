-- name: GrantCapability :exec
INSERT INTO aura.capability_grants (identity_id, capability)
VALUES ($1, $2)
ON CONFLICT (identity_id, capability) DO NOTHING;

-- name: RevokeCapability :exec
DELETE FROM aura.capability_grants
WHERE identity_id = $1
  AND capability = $2;

-- name: ListCapabilities :many
SELECT identity_id, capability, granted_at
FROM aura.capability_grants
WHERE identity_id = $1
ORDER BY capability ASC;

-- name: HasCapability :one
SELECT EXISTS (
    SELECT 1
    FROM aura.capability_grants
    WHERE identity_id = $1
      AND (capability = '*' OR capability = $2)
) AS has_capability;
