-- Deleting an identity failed on tables that block it, AFTER the de-provisioning
-- saga had already dropped that identity's memory database, object-store bucket and
-- filesystem roots. The saga orders identity_row last on purpose, so a blocking FK
-- there is not a refusal: it is a half-erased person plus an orphan nobody sweeps.
--
-- Measured live 2026-08-31, twice in a row, deleting one real identity:
--   ERROR: update or delete on table "identities" violates foreign key constraint
--          "telegram_accounts_identity_id_fkey" on table "telegram_accounts"
--   ERROR: ... "telegram_setup_pending_identity_id_fkey" on table "telegram_setup_pending"
-- Both had to be deleted by hand to get past it, which is exactly how two orphan
-- ArcadeDB tenant databases were created that same session.
--
-- 19 of the 24 identity references were already ON DELETE CASCADE. These three are
-- identity-OWNED state with no reason to outlive their owner: a Telegram binding and
-- its pending setup token belong to one person, and a benchmark run lease is
-- ephemeral operational state. They join the majority.
--
-- aura.audit_logs.actor_identity_id is deliberately NOT included. An audit trail is
-- supposed to outlive its subject, so cascading it would destroy the record of what
-- someone did as a side effect of deleting them. It is still RESTRICT and still a
-- landmine (serve_provisioning.go names it), but it is DORMANT: zero rows and no
-- writer outside tests, measured 2026-08-31. Arming it safely is a retention
-- decision -- drop the FK, or anonymize the actor on purge -- not a delete rule.

ALTER TABLE aura.telegram_accounts
    DROP CONSTRAINT telegram_accounts_identity_id_fkey,
    ADD CONSTRAINT telegram_accounts_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities (id) ON DELETE CASCADE;

ALTER TABLE aura.telegram_setup_pending
    DROP CONSTRAINT telegram_setup_pending_identity_id_fkey,
    ADD CONSTRAINT telegram_setup_pending_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities (id) ON DELETE CASCADE;

ALTER TABLE aura.benchmark_settings_overrides
    DROP CONSTRAINT benchmark_settings_overrides_owner_id_fkey,
    ADD CONSTRAINT benchmark_settings_overrides_owner_id_fkey
        FOREIGN KEY (owner_id) REFERENCES aura.identities (id) ON DELETE CASCADE;
