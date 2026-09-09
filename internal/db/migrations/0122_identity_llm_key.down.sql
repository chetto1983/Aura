-- Down for 0122_identity_llm_key. Irreversible (recorded in 02-01-PLAN.md's checkpoint,
-- approved before this file was written): the raw OpenRouter key is returned exactly
-- once by POST /api/v1/keys and there is no endpoint that returns it again, so dropping
-- this table forces a re-mint per identity and orphans the old keys at the provider.

DROP TABLE aura.identity_llm_key;
