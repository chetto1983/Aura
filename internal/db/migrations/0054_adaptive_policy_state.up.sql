-- Distributed adaptive-intelligence kill switch and rollout policy.
--
-- Postgres is authoritative and every decision reads this row before an adaptive
-- action can affect execution. A database partition therefore falls back to the
-- baseline instead of serving a stale active policy.
CREATE TABLE aura.adaptive_policy_state (
    scope           text PRIMARY KEY CHECK (scope = 'global'),
    epoch           bigint NOT NULL CHECK (epoch >= 1),
    policy_version  text NOT NULL CHECK (btrim(policy_version) <> ''),
    mode            text NOT NULL CHECK (
        mode IN ('off', 'shadow', 'canary', 'active', 'rollback')
    ),
    rollout_bps     integer NOT NULL CHECK (rollout_bps BETWEEN 0 AND 10000),
    config          jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (mode = 'off' AND rollout_bps = 0)
        OR (mode = 'shadow' AND rollout_bps = 0)
        OR (mode = 'canary' AND rollout_bps BETWEEN 1 AND 9999)
        OR (mode = 'active' AND rollout_bps = 10000)
        OR (mode = 'rollback' AND rollout_bps = 0)
    )
);

INSERT INTO aura.adaptive_policy_state (
    scope, epoch, policy_version, mode, rollout_bps, config
) VALUES (
    'global', 1, 'bootstrap-off', 'off', 0, '{}'::jsonb
);

-- Runtime processes may read the current epoch and perform an internal
-- compare-and-swap transition. They cannot insert or delete the singleton row.
GRANT SELECT, UPDATE ON aura.adaptive_policy_state TO aura_app;

COMMENT ON TABLE aura.adaptive_policy_state IS
    'Authoritative global adaptive rollout epoch. Every execution gate reads it; unavailable means baseline-only.';
