-- Source: PRD Phase 36 (MUSR-01/MUSR-03) + 36-02-PLAN Task 1 + 36-RESEARCH OQ2.
-- The migration floor before this is 0026 (local_admin_caps); paused_states gains
-- its owning identity at 0027.
--
-- OQ2 resolution: add a first-class identity_id column to aura.paused_states rather
-- than deriving the owner through a per-read `conversation_id -> conversations`
-- subquery. A stored column gives the kernel-isolation layer (plan 04) a clean
-- `*ForIdentity` predicate and lets the RLS owner-scoping policy (plan 04) key on the
-- row itself, not on a join. It is ADDITIVE: existing rows are backfilled from their
-- parent conversation; the write-path population lands with plan 05, so the column is
-- left NULLABLE here (no NOT NULL / no default — a fresh pre-plan-05 pause carries NULL
-- until its writer is wired).

ALTER TABLE aura.paused_states ADD COLUMN identity_id uuid;

-- Backfill from the parent conversation. paused_states.conversation_id was promoted to
-- uuid + FK REFERENCES aura.conversations(id) in 0005, so this join is total for every
-- existing row (the FK guarantees a matching conversation).
UPDATE aura.paused_states AS p
    SET identity_id = c.identity_id
    FROM aura.conversations AS c
    WHERE p.conversation_id = c.id;

-- Index the owner column for the plan-04 `*ForIdentity` scans / RLS owner filter.
CREATE INDEX paused_states_identity_idx
    ON aura.paused_states (identity_id);

-- The existing aura_app DML grant (0003:26) already covers the new column — no new GRANT.

COMMENT ON COLUMN aura.paused_states.identity_id IS
    'Owning identity (Phase 36, OQ2): a first-class column backfilled from the parent aura.conversations.identity_id so the plan-04 kernel-isolation / RLS owner filter keys on the row, not a subquery. NULLABLE until the plan-05 write path populates it.';
