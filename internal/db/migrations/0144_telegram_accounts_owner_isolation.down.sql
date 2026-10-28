-- The restrictive floor goes first, so no intermediate state is stricter than either version.
DROP FUNCTION IF EXISTS aura.retire_legacy_local_rows(uuid);
DROP POLICY IF EXISTS telegram_accounts_owner_or_pre_identity ON aura.telegram_accounts;
DROP POLICY IF EXISTS telegram_accounts_pre_identity_read ON aura.telegram_accounts;
DROP POLICY IF EXISTS telegram_accounts_owner_isolation ON aura.telegram_accounts;
ALTER TABLE aura.telegram_accounts DISABLE ROW LEVEL SECURITY;
