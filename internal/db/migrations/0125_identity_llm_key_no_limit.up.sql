-- A NULL limit_usd is a key with no spending limit: the admin's own key is minted that way
-- (docs/superpowers/specs/2026-09-10-management-key-onboarding-design.md). Zero stays the
-- member default (CRED-02).
ALTER TABLE aura.identity_llm_key ALTER COLUMN limit_usd DROP NOT NULL;

COMMENT ON COLUMN aura.identity_llm_key.limit_usd IS
    'Spending cap in USD. NULL is a key with no limit (the admin key); 0 refuses until an admin tops it up.';
