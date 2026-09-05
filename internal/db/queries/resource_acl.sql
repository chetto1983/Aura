-- The generic resource ACL (migration 0118). granted_by is always the CALLING identity, and
-- that is enforced one layer down: resource_acl_grant_what_you_own is a RESTRICTIVE write
-- policy admitting only a grant attributed to the caller, on a resource the caller OWNS. So a
-- grant forged in somebody else's name, or made over somebody else's skill, is refused by the
-- database rather than by a check a caller can forget (D-214-5).

-- name: GrantResourceToIdentity :exec
-- Re-granting the same pair replaces the permission bits, which is what "share again with
-- edit this time" means. created_at is left alone: it records when the share began.
INSERT INTO aura.resource_acl (resource_type, resource_id, principal_type, principal_id, perm_bits, granted_by)
VALUES ($1, $2, 'identity', $3, $4, $5)
ON CONFLICT (resource_type, resource_id, principal_type, principal_id) WHERE principal_id IS NOT NULL
DO UPDATE SET perm_bits = EXCLUDED.perm_bits;

-- name: GrantResourcePublic :exec
-- The public grant carries no principal_id (the resource_acl_principal_shape CHECK), so it
-- conflicts on the partial index keyed by resource alone.
INSERT INTO aura.resource_acl (resource_type, resource_id, principal_type, principal_id, perm_bits, granted_by)
VALUES ($1, $2, 'public', NULL, $3, $4)
ON CONFLICT (resource_type, resource_id) WHERE principal_type = 'public'
DO UPDATE SET perm_bits = EXCLUDED.perm_bits;

-- name: RevokeResourceFromIdentity :execrows
DELETE FROM aura.resource_acl
WHERE resource_type = $1 AND resource_id = $2 AND principal_type = 'identity' AND principal_id = $3;

-- name: RevokeResourcePublic :execrows
DELETE FROM aura.resource_acl
WHERE resource_type = $1 AND resource_id = $2 AND principal_type = 'public';

-- name: ListAccessibleResources :many
-- The grantee's half of the two-query read LibreChat runs (findAccessibleResources → the
-- domain query filtered on those ids): which resources of this type may this identity see
-- with AT LEAST these permission bits. The bitmask test is `& want = want`, so a view query
-- also matches a row granted view+edit.
SELECT DISTINCT resource_id
FROM aura.resource_acl
WHERE resource_type = $1
  AND (perm_bits & @want::integer) = @want::integer
  AND (principal_type = 'public'
       OR (principal_type = 'identity' AND principal_id = @principal_id::uuid));

-- name: ListResourceGrants :many
-- Every grant standing on one resource, for the operator asking "who can read this?" before
-- deciding whether to revoke. Ordered so a listing is stable across calls.
SELECT resource_type, resource_id, principal_type, principal_id, perm_bits, granted_by, created_at
FROM aura.resource_acl
WHERE resource_type = $1 AND resource_id = $2
ORDER BY principal_type, principal_id;
