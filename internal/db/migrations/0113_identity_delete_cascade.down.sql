-- Restore the blocking delete rules. telegram_* were NO ACTION and
-- benchmark_settings_overrides was RESTRICT; the difference is only when the check
-- runs (end of statement vs immediately), and both refuse the delete.

ALTER TABLE aura.telegram_accounts
    DROP CONSTRAINT telegram_accounts_identity_id_fkey,
    ADD CONSTRAINT telegram_accounts_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities (id);

ALTER TABLE aura.telegram_setup_pending
    DROP CONSTRAINT telegram_setup_pending_identity_id_fkey,
    ADD CONSTRAINT telegram_setup_pending_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities (id);

ALTER TABLE aura.benchmark_settings_overrides
    DROP CONSTRAINT benchmark_settings_overrides_owner_id_fkey,
    ADD CONSTRAINT benchmark_settings_overrides_owner_id_fkey
        FOREIGN KEY (owner_id) REFERENCES aura.identities (id) ON DELETE RESTRICT;
