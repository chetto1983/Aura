CREATE TABLE aura.identity_recovery (
    identity_id          uuid        PRIMARY KEY REFERENCES aura.identities (id) ON DELETE CASCADE,
    question             text        NOT NULL,
    answer_hash          text        NOT NULL,
    answer_hash_version  text        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.password_reset_challenges (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id      uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    code_hash        text        NOT NULL,
    telegram_user_id bigint,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    consumed_at      timestamptz,
    attempt_count    integer     NOT NULL DEFAULT 0,
    max_attempts     integer     NOT NULL DEFAULT 5,
    request_ip_hash  text,
    user_agent_hash  text
);

CREATE TABLE aura.password_reset_tokens (
    token_hash     text        PRIMARY KEY,
    challenge_id   uuid        NOT NULL REFERENCES aura.password_reset_challenges (id) ON DELETE CASCADE,
    identity_id    uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    consumed_at    timestamptz,
    attempt_count  integer     NOT NULL DEFAULT 0,
    max_attempts   integer     NOT NULL DEFAULT 3
);

CREATE TABLE aura.identity_recovery_audit (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id      uuid,
    event            text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    request_ip_hash  text,
    user_agent_hash  text,
    metadata         jsonb       NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX identity_recovery_updated_idx
    ON aura.identity_recovery (updated_at DESC);
CREATE INDEX password_reset_challenges_identity_active_idx
    ON aura.password_reset_challenges (identity_id, expires_at)
    WHERE consumed_at IS NULL;
CREATE INDEX password_reset_tokens_identity_active_idx
    ON aura.password_reset_tokens (identity_id, expires_at)
    WHERE consumed_at IS NULL;
CREATE INDEX identity_recovery_audit_created_idx
    ON aura.identity_recovery_audit (created_at DESC);

GRANT SELECT, INSERT, UPDATE ON aura.identity_recovery TO aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.password_reset_challenges TO aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.password_reset_tokens TO aura_app;
GRANT SELECT, INSERT ON aura.identity_recovery_audit TO aura_app;
GRANT ALL ON aura.identity_recovery TO aura_migrate;
GRANT ALL ON aura.password_reset_challenges TO aura_migrate;
GRANT ALL ON aura.password_reset_tokens TO aura_migrate;
GRANT ALL ON aura.identity_recovery_audit TO aura_migrate;

COMMENT ON TABLE aura.identity_recovery IS
    'Per-identity recovery question and hashed answer for Telegram password reset. Raw answers are never stored.';
COMMENT ON TABLE aura.password_reset_challenges IS
    'Short-lived Telegram code challenges for self-service Authula password reset.';
COMMENT ON TABLE aura.password_reset_tokens IS
    'Short-lived server-side reset tokens minted after Telegram code and security answer verification.';
COMMENT ON TABLE aura.identity_recovery_audit IS
    'Append-only recovery event audit. Contains no raw emails, answers, codes, passwords, IP addresses, or Telegram tokens.';
