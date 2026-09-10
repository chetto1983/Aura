-- Going back turns a key with no limit into a zero cap: Aura then refuses that identity's
-- turns until an admin sets a cap. Failing closed is the right side to fail on.
UPDATE aura.identity_llm_key SET limit_usd = 0 WHERE limit_usd IS NULL;
ALTER TABLE aura.identity_llm_key ALTER COLUMN limit_usd SET NOT NULL;
COMMENT ON COLUMN aura.identity_llm_key.limit_usd IS NULL;
