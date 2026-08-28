ALTER TABLE aura.paused_states
    DROP CONSTRAINT IF EXISTS paused_states_worker_attribution_exclusive;

ALTER TABLE aura.paused_states
    DROP COLUMN IF EXISTS owning_worker_id,
    DROP COLUMN IF EXISTS pending_action_id;
