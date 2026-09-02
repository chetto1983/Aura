-- Admit the memory mention-link backfill sweep as a scheduler kind.
--
-- A MENTIONS edge between two entities is only decidable against the WHOLE corpus: an
-- entity mentioned by more than a configured share of all facts is excluded from linking
-- (a hub cap), and no single fact-write can evaluate a corpus-wide share. Linking is
-- therefore a periodic rebuild, never a write hook — memory_mention_link IS that
-- scheduled caller, seeded by seedMemoryMentionLinkSweep and routed to
-- handlers.NewMemoryMentionLinkHandler. Without it, facts written after the last run
-- stay unreachable from their neighbours (no second hop).
--
-- The 0009 constraint is unnamed (inline column CHECK), so Postgres auto-named it
-- `scheduler_tasks_kind_check`; drop + re-add it with the extra member, exactly as 0091
-- and 0104 did. The member list is 0104's (the latest migration to touch this
-- constraint) plus 'memory_mention_link'. 0009 GRANTed aura_app DML on scheduler_tasks
-- already, so no grant change is needed here (aura_migrate owns DDL).

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill', 'observability_check',
        'memory_mention_link'));
