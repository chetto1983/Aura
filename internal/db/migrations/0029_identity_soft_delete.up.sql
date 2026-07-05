-- Source: PRD Phase 36 (D-27 grace-window purge) + 36-02-PLAN Task 1. The migration
-- floor before this is 0028 (saga_journal); identity soft-delete lands at 0029.
--
-- Soft-delete columns on aura.identities (deleted_at precedent: 0020_assets:33).
-- deactivated_at marks the moment an identity is disabled (login refused, work paused);
-- purge_after is the end of the grace window after which plan 08's de-provisioning saga
-- may hard-delete the identity and cascade its owned rows. Both NULLABLE: a live identity
-- has neither set. This is pure additive schema — the sweep/purge behavior is driven by
-- the plan-08 saga, not by any trigger here.

ALTER TABLE aura.identities ADD COLUMN deactivated_at timestamptz;
ALTER TABLE aura.identities ADD COLUMN purge_after    timestamptz;

-- The existing aura_app DML grant (0004:21) already covers the new columns — no new GRANT.

COMMENT ON COLUMN aura.identities.deactivated_at IS
    'Soft-delete marker (Phase 36, D-27): non-NULL once the identity is deactivated. Login is refused and work is paused; rows are retained until purge_after.';
COMMENT ON COLUMN aura.identities.purge_after IS
    'End of the soft-delete grace window (Phase 36, D-27): after this instant the plan-08 de-provisioning saga may hard-delete the identity and cascade its owned data.';
