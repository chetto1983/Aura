-- Admit the memory embedding backfill sweep as a scheduler kind.
--
-- Every fact written to ArcadeDB landed WITHOUT a vector — from onboarding, from the
-- MCP tool, from the CLI — and nothing ever filled them in. The machinery existed
-- (Client.EmbedMissingFacts, reachable as `aura memory reembed`), but it had no
-- scheduled caller, so the dense leg of memory retrieval stayed blind until an
-- operator typed a command. memory_embed_backfill IS that caller: the sixth
-- system-seeded sweep, seeded by seedMemoryEmbedBackfillSweep and routed to
-- handlers.NewMemoryEmbedBackfillHandler.
--
-- The 0009 constraint is unnamed (inline column CHECK), so Postgres auto-named it
-- `scheduler_tasks_kind_check`; drop + re-add it with the extra member, exactly as
-- 0034 and 0083 did. The member list is 0083's plus 'memory_embed_backfill'. 0009
-- GRANTed aura_app DML on scheduler_tasks already, so no grant change is needed here
-- (aura_migrate owns DDL).

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill'));
