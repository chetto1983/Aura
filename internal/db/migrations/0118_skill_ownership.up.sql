-- Skills acquire an owner (amendment #214). The floor before this is 0117
-- (llm_provider_routes); the skill catalog and the resource ACL land at 0118.
--
-- WHY. A skill is executable instruction the model follows. Until now there was ONE skills
-- root for the whole deployment, so every identity's agent loaded every identity's skills:
-- a skill written by one person changed what another person's model was told to do. In
-- single-operator deployments that is invisible, which is exactly how it survived. It is a
-- prompt-injection boundary, not an ordering preference.
--
-- WHAT IS A ROW AND WHAT IS NOT (D-214-1). Postgres owns the CATALOG and the ACL; the
-- filesystem keeps the BODIES, under one root per owner ($AURA_SKILLS_IDENTITY_DIR/<id>).
-- Bodies stay on disk because a skill is docker-cp'd into a box at every create and resume:
-- rows would mean re-rendering a row into a tree on every resolve. Bodies-in-DB is left
-- OPEN, not closed -- the ACL does not depend on it and that migration would be additive.
--
-- The deployment's OWN skills ($AURA_SKILLS_DIR) are deliberately NOT rows here. They are
-- the read-only overlay every identity sees, exactly as migration 0101 says of the MCP
-- recipes: "Recipes that are merely DECLARED in code are not rows here: they are overlaid
-- read-only at read time." Upgrading the deployment therefore still updates them, and on a
-- name collision the GLOBAL skill wins (D-214-3) -- a skill the operator ships is house
-- policy, and a person quietly shadowing it is how the policy stops applying without
-- anyone noticing.
--
-- IDENTITY IS uuid HERE. aura.skill_audit.identity_id is `text DEFAULT 'local'` -- the
-- identity NAME, not its id -- while aura.identities.id and the RLS of 0100 are uuid. That
-- is an inherited inconsistency, not a pattern: this migration uses uuid and the audit
-- ledger stays as it is until someone deliberately aligns it (it is append-only, so
-- rewriting its history is not on the table).

CREATE TABLE aura.skill_catalog (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    -- The skill-name grammar the filesystem chokepoint enforces (skills.SanitizeName):
    -- lowercase, digits and dashes. It is repeated here because this column is joined into
    -- a filesystem path by every reader, and a name that cannot name a directory must not
    -- be storable in the first place.
    name              text        NOT NULL CHECK (name ~ '^[a-z0-9-]{1,64}$'),
    description       text        NOT NULL DEFAULT '',
    -- Denormalised out of the SKILL.md frontmatter, and indexed, for the reason LibreChat
    -- records on the same column (packages/data-schemas/src/schema/skill.ts): the always-on
    -- block is rendered at the top of EVERY turn, so "which of this identity's skills are
    -- always-on" must be an index lookup, never a scan of the catalog.
    always_apply      boolean     NOT NULL DEFAULT false,
    -- The canonical content hash of the body on disk (skills.HashSkillDir), so a row can be
    -- matched against the tree it describes without reading the tree.
    content_hash      text        NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    -- A name is unique PER OWNER, not globally (LibreChat's {name, author, tenantId}): two
    -- people may each keep a skill called `deploy`, and each sees their own.
    UNIQUE (owner_identity_id, name)
);

-- The always-on lookup (acceptance criterion 7). Partial, because the query only ever asks
-- for the true rows: the index holds one entry per always-on skill instead of one per skill.
CREATE INDEX skill_catalog_always_apply_idx
    ON aura.skill_catalog (owner_identity_id)
    WHERE always_apply;

-- Generic from day one (D-214-4): resource_type is a column although this slice writes only
-- 'skill'. The per-identity MCP work (#209/#210) wants exactly this table, and a second
-- bespoke ACL is how a deployment ends up with two that disagree about what a grant means.
--
-- The type ENUM is closed at 'skill' even though the TABLE is generic. Admitting a value
-- nothing writes would be dark schema; widening it is one ALTER when the second consumer
-- lands, and until then a typo'd resource_type is rejected instead of stored.
CREATE TABLE aura.resource_acl (
    resource_type  text        NOT NULL CHECK (resource_type IN ('skill')),
    -- Deliberately NOT a foreign key: the whole point of the table is that it outlives any
    -- single resource table. Orphans are prevented by the two triggers below, which is the
    -- same guarantee an FK gives without pinning the column to one table.
    resource_id    uuid        NOT NULL,
    -- 'group' is admitted and NOT built (D-214-4): "shared with the team" has no answer
    -- while Aura has no principal that is not one person, and the code writes only
    -- 'identity' and 'public'. The enum records the shape the next slice will fill.
    principal_type text        NOT NULL CHECK (principal_type IN ('identity', 'public', 'group')),
    principal_id   uuid,
    -- A bitmask, LibreChat's aclEntry.permBits: 1=view, 2=edit, 4=delete, 8=share. One
    -- column instead of a boolean per verb, so a new verb is a constant rather than a
    -- migration.
    perm_bits      integer     NOT NULL CHECK (perm_bits > 0),
    -- CASCADE, not SET NULL. The write policies admit a grant only when granted_by IS the
    -- caller AND the caller owns the resource, so the granter is always the owner: a grant
    -- cannot outlive its granter in any meaningful sense, because the skill it shares goes
    -- with them. SET NULL was the first attempt and TestIdentityReferencesCascadeOrAre-
    -- ExplicitlyExempt refused it (CI run 1790) — rightly, since it leaves a row alive whose
    -- owner is gone, and de-provisioning deletes the identity LAST, after the memory database
    -- and the bucket are already erased.
    granted_by     uuid        REFERENCES aura.identities (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- A public grant is addressed to nobody in particular and an identity/group grant must
    -- name its principal. Without this a NULL principal_id on an 'identity' row would be a
    -- grant to everyone wearing the label of a grant to one person.
    CONSTRAINT resource_acl_principal_shape CHECK (
        (principal_type = 'public' AND principal_id IS NULL)
     OR (principal_type IN ('identity', 'group') AND principal_id IS NOT NULL)
    )
);

-- One grant per (resource, principal). Two partial indexes rather than a primary key,
-- because principal_id is NULL for a public grant and NULL is not unique in a PK.
CREATE UNIQUE INDEX resource_acl_principal_idx
    ON aura.resource_acl (resource_type, resource_id, principal_type, principal_id)
    WHERE principal_id IS NOT NULL;

CREATE UNIQUE INDEX resource_acl_public_idx
    ON aura.resource_acl (resource_type, resource_id)
    WHERE principal_type = 'public';

-- The read the grantee side runs: "which resources of this type may I see". Leading with
-- the principal keys the lookup to the caller instead of scanning every grant in the
-- deployment.
CREATE INDEX resource_acl_principal_lookup_idx
    ON aura.resource_acl (principal_type, principal_id, resource_type);

-- Orphan prevention, both directions. A deleted RESOURCE takes its grants with it; a
-- deleted PRINCIPAL takes the grants addressed to it. Triggers rather than foreign keys
-- because resource_id points at whichever table resource_type names, and principal_id will
-- point at a group table the day groups exist. Acceptance criterion 6 (deprovisioning an
-- identity leaves no orphan ACL row) is closed by the pair: the identity cascade removes
-- its skill_catalog rows, which fires the first trigger, and the identity delete itself
-- fires the second.
--
-- BOTH ARE SECURITY DEFINER, and that is what makes them work at all rather than a
-- hardening flourish. A plpgsql trigger function runs as the CALLING role, so its DELETE on
-- aura.resource_acl is filtered by that table's own RLS -- including the restrictive floor
-- that denies everything when app.current_identity is unset. Deprovisioning is exactly that
-- caller: internal/identity.Store.DeleteIdentity runs `DELETE FROM aura.identities` on the
-- bare pool, with no identity set, so an INVOKER trigger would delete zero grants, report
-- success, and leave every one of them orphaned behind a deleted principal. Running as the
-- owner (aura_migrate, who owns both tables and is therefore not subject to their row
-- security) makes the collection unconditional. search_path is pinned because a SECURITY
-- DEFINER function must never resolve an identifier through a caller-controlled path
-- (PostgreSQL manual, CREATE FUNCTION / "Writing SECURITY DEFINER Functions Safely"), and
-- both bodies are closed statements over fully-qualified tables with no caller input in them.
CREATE FUNCTION aura.delete_acl_for_skill() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
    AS $$
BEGIN
    DELETE FROM aura.resource_acl WHERE resource_type = 'skill' AND resource_id = OLD.id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER skill_catalog_acl_cascade
    AFTER DELETE ON aura.skill_catalog
    FOR EACH ROW EXECUTE FUNCTION aura.delete_acl_for_skill();

CREATE FUNCTION aura.delete_acl_for_principal() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
    AS $$
BEGIN
    DELETE FROM aura.resource_acl
     WHERE principal_type = 'identity' AND principal_id = OLD.id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER identities_acl_principal_cascade
    AFTER DELETE ON aura.identities
    FOR EACH ROW EXECUTE FUNCTION aura.delete_acl_for_principal();

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.skill_catalog TO aura_app;
GRANT ALL                           ON aura.skill_catalog TO aura_migrate;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.resource_acl  TO aura_app;
GRANT ALL                           ON aura.resource_acl  TO aura_migrate;

-- Isolation is RLS, not a WHERE clause someone can forget (D-214-5). Both layers from 0087,
-- in the order and with the predicates migration 0100 uses: the permissive policy scopes
-- rows to their owner, and the RESTRICTIVE floor is what makes a caller with no
-- app.current_identity see NOTHING rather than everything. Adding only the first would be
-- the failure 0087 exists to prevent (acceptance criterion 8).
ALTER TABLE aura.skill_catalog ENABLE ROW LEVEL SECURITY;

CREATE POLICY skill_catalog_owner_isolation ON aura.skill_catalog
    USING (owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

-- Sharing is a READ, and only a read: a grantee sees the row, never edits or deletes it, so
-- this permissive policy is FOR SELECT and the owner-isolation policy above remains the only
-- way to write. Permissive policies OR together, so a row is visible when it is yours or
-- when a grant names you. The subquery is itself subject to aura.resource_acl's policies,
-- which admit exactly the public rows and the rows naming this caller -- it cannot be used
-- to probe grants that are none of the caller's business.
CREATE POLICY skill_catalog_shared_read ON aura.skill_catalog
    FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM aura.resource_acl a
         WHERE a.resource_type = 'skill'
           AND a.resource_id = aura.skill_catalog.id
           AND (a.principal_type = 'public'
                OR (a.principal_type = 'identity'
                    AND a.principal_id = NULLIF(current_setting('app.current_identity', true), '')::uuid))
    ));

CREATE POLICY skill_catalog_requires_identity ON aura.skill_catalog
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

ALTER TABLE aura.resource_acl ENABLE ROW LEVEL SECURITY;

-- Three ways a grant row is yours to SEE: you granted it, it names you, or it is public.
CREATE POLICY resource_acl_visibility ON aura.resource_acl
    USING (granted_by = NULLIF(current_setting('app.current_identity', true), '')::uuid
           OR principal_type = 'public'
           OR (principal_type = 'identity'
               AND principal_id = NULLIF(current_setting('app.current_identity', true), '')::uuid));

-- Seeing is not granting, and the visibility predicate must NOT be allowed to double as the
-- insert check. Its disjuncts are satisfiable by the attacker: a row with
-- principal_type='public', or one naming the caller as principal, passes it whatever
-- resource_id it carries -- so under the visibility predicate alone ANY identity could grant
-- ITSELF view on ANY resource id it can name, or publish somebody else's skill to the whole
-- deployment, and the shared-read policy on aura.skill_catalog would then honour that grant.
-- "The caller checks ownership first" is not the answer: D-214-5 puts this boundary in the
-- database precisely so it does not depend on a caller remembering.
--
-- So the write check is its own RESTRICTIVE policy (AND-combined, no later permissive policy
-- can loosen it): you may only create a grant attributed to yourself, on a resource you
-- OWN. The EXISTS runs under aura.skill_catalog's policies, but it tests owner_identity_id
-- explicitly, so a row visible to the caller only through skill_catalog_shared_read -- a
-- skill somebody shared WITH them -- cannot be re-shared onward.
CREATE POLICY resource_acl_grant_what_you_own ON aura.resource_acl
    AS RESTRICTIVE FOR INSERT TO aura_app
    WITH CHECK (
        granted_by = NULLIF(current_setting('app.current_identity', true), '')::uuid
        AND resource_type = 'skill'
        AND EXISTS (
            SELECT 1 FROM aura.skill_catalog c
             WHERE c.id = aura.resource_acl.resource_id
               AND c.owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
        )
    );

-- The same rule for the UPDATE half of ON CONFLICT DO UPDATE ("share it again, with edit
-- this time"): USING governs which existing row may be re-granted, WITH CHECK what it may
-- become. Without the USING half a caller could re-permission a grant they can merely see.
CREATE POLICY resource_acl_regrant_what_you_own ON aura.resource_acl
    AS RESTRICTIVE FOR UPDATE TO aura_app
    USING (granted_by = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (
        granted_by = NULLIF(current_setting('app.current_identity', true), '')::uuid
        AND resource_type = 'skill'
        AND EXISTS (
            SELECT 1 FROM aura.skill_catalog c
             WHERE c.id = aura.resource_acl.resource_id
               AND c.owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
        )
    );

-- Revoking is likewise narrower than seeing. Under the visibility predicate alone any
-- identity could DELETE a public grant somebody else made -- unsharing another person's
-- skill from the whole deployment -- because "it is public" is true for everyone. Two roles
-- may remove a grant: the person who made it, and the person it names (declining a share is
-- theirs to do). The two AFTER DELETE cascades above are unaffected: they run as the table
-- owner and are not subject to this at all.
CREATE POLICY resource_acl_revoke_your_own ON aura.resource_acl
    AS RESTRICTIVE FOR DELETE TO aura_app
    USING (granted_by = NULLIF(current_setting('app.current_identity', true), '')::uuid
           OR (principal_type = 'identity'
               AND principal_id = NULLIF(current_setting('app.current_identity', true), '')::uuid));

CREATE POLICY resource_acl_requires_identity ON aura.resource_acl
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY skill_catalog_requires_identity ON aura.skill_catalog IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

COMMENT ON POLICY resource_acl_requires_identity ON aura.resource_acl IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

COMMENT ON TABLE aura.skill_catalog IS
    'The per-identity skill catalog (migration 0118, amendment #214). One row per skill an identity owns; the body lives on the filesystem under $AURA_SKILLS_IDENTITY_DIR/<identity>/<name>, because skills are docker-cp''d into a box at every resolve. The deployment''s own skills ($AURA_SKILLS_DIR) are NOT rows: they are the read-only overlay every identity sees, and they WIN a name collision (D-214-3).';

COMMENT ON TABLE aura.resource_acl IS
    'Generic resource ACL (migration 0118, amendment #214), shaped on LibreChat''s aclEntry: principal_type x resource_type x perm_bits, one table for every shareable resource. This slice writes only resource_type=''skill'' and principal_type in (identity, public); ''group'' is admitted by the enum and deliberately not built. resource_id carries no foreign key on purpose -- the two AFTER DELETE triggers keep it free of orphans without pinning the column to one table.';
