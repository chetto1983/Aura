-- name: SetGatewayToolPolicy :exec
INSERT INTO aura.gateway_tool_policies (identity_id, tool, action, policy, set_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (identity_id, tool, action)
DO UPDATE SET policy = EXCLUDED.policy, set_at = now(), set_by = EXCLUDED.set_by;

-- name: GetGatewayToolPolicy :one
SELECT policy
FROM aura.gateway_tool_policies
WHERE identity_id = $1
  AND tool = $2
  AND action = $3;

-- name: ListGatewayToolPolicies :many
SELECT identity_id, tool, action, policy, set_at, set_by
FROM aura.gateway_tool_policies
WHERE identity_id = $1
ORDER BY tool ASC, action ASC;

-- name: ClearGatewayToolPolicy :execrows
DELETE FROM aura.gateway_tool_policies
WHERE identity_id = $1
  AND tool = $2
  AND action = $3;
