-- Restore the pre-0018 live FK shape. NOT VALID keeps rollback possible even if
-- audit rows already preserve tokens for pauses deleted while 0018 was active;
-- new writes are still checked after the constraint is reintroduced.

ALTER TABLE aura.skill_audit
    ADD CONSTRAINT skill_audit_paused_state_token_fkey
    FOREIGN KEY (paused_state_token) REFERENCES aura.paused_states (token)
    NOT VALID;

COMMENT ON COLUMN aura.skill_audit.paused_state_token IS NULL;
