-- The per-identity skill catalog (migration 0118). Every statement carries owner_identity_id
-- in its predicate AND runs under the two RLS policies: the predicate is what makes the
-- intent readable, the policy is what makes it true when a future caller forgets to write it.

-- name: UpsertSkillCatalog :one
-- Landing a skill is idempotent per (owner, name): a rewrite of the same skill updates the
-- row it already has. created_at is left alone on conflict -- it records when this identity
-- first authored the skill, which an edit does not change.
INSERT INTO aura.skill_catalog (owner_identity_id, name, description, always_apply, content_hash)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (owner_identity_id, name) DO UPDATE SET
    description  = EXCLUDED.description,
    always_apply = EXCLUDED.always_apply,
    content_hash = EXCLUDED.content_hash,
    updated_at   = now()
RETURNING id, owner_identity_id, name, description, always_apply, content_hash, created_at, updated_at;

-- name: DeleteSkillCatalog :execrows
-- execrows, not exec: a caller that reports "removed" without knowing whether a row existed
-- cannot tell a real removal from a no-op on someone else's row that RLS filtered away.
DELETE FROM aura.skill_catalog
WHERE owner_identity_id = $1 AND name = $2;

-- name: ListSkillCatalogForOwner :many
SELECT id, owner_identity_id, name, description, always_apply, content_hash, created_at, updated_at
FROM aura.skill_catalog
WHERE owner_identity_id = $1
ORDER BY name;

-- name: ListAlwaysApplySkills :many
-- The always-on block is rendered at the top of every turn, so this must be an index lookup
-- (skill_catalog_always_apply_idx, partial on always_apply) and never a scan -- acceptance
-- criterion 7 of amendment #214 asserts the plan carries no Seq Scan on this table.
SELECT id, owner_identity_id, name, description, always_apply, content_hash, created_at, updated_at
FROM aura.skill_catalog
WHERE owner_identity_id = $1 AND always_apply
ORDER BY name;

-- name: ListSkillCatalogByIDs :many
-- The shared-in half of a reader's view: the rows an ACL lookup already said they may see.
-- It carries NO owner predicate on purpose -- these are by definition someone else's skills
-- -- so the ids MUST come from aura.resource_acl and never from user input.
SELECT id, owner_identity_id, name, description, always_apply, content_hash, created_at, updated_at
FROM aura.skill_catalog
WHERE id = ANY(@ids::uuid[])
ORDER BY name;
