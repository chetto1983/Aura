-- Skill audit rows are append-only historical evidence, while paused_states rows
-- are ephemeral workflow state owned by a conversation. A live FK from
-- skill_audit.paused_state_token back to paused_states made those lifetimes
-- incompatible: deleting a conversation cascades its paused_states, but Postgres
-- rejected the cascade while skill_audit still referenced the pause token.
--
-- Keep the token value and D-29 coherence CHECK, but remove the live FK so hard
-- conversation delete can remove ephemeral pauses without mutating audit history.

ALTER TABLE aura.skill_audit
    DROP CONSTRAINT IF EXISTS skill_audit_paused_state_token_fkey;

COMMENT ON COLUMN aura.skill_audit.paused_state_token IS
    'Historical ask_user pause token. Intentionally not a live FK: paused_states are ephemeral and may be cascade-deleted with their conversation while audit rows remain append-only.';
