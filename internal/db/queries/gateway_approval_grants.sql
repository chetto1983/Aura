-- name: GrantGatewayApproval :exec
INSERT INTO aura.gateway_approval_grants (identity_id, tool, action, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (identity_id, tool, action) DO NOTHING;

-- name: HasGatewayApprovalGrant :one
SELECT EXISTS (
    SELECT 1
    FROM aura.gateway_approval_grants
    WHERE identity_id = $1
      AND tool = $2
      AND action = $3
) AS has_grant;

-- name: ListGatewayApprovalGrants :many
SELECT identity_id, tool, action, granted_at, granted_by
FROM aura.gateway_approval_grants
WHERE identity_id = $1
ORDER BY tool ASC, action ASC;

-- name: RevokeGatewayApprovalGrant :execrows
DELETE FROM aura.gateway_approval_grants
WHERE identity_id = $1
  AND tool = $2
  AND action = $3;
