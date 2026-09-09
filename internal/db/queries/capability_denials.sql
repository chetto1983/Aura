-- name: InsertCapabilityDenial :exec
-- The only query this table needs: RequireCapability's three refusal branches write here,
-- and the read side goes through audit_store.go's UNION leg (raw pgx, no sqlc query), not
-- through a second generated method.
INSERT INTO aura.capability_denials (identity_id, capability, route, cause)
VALUES ($1, $2, $3, $4);
