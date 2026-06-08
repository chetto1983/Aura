-- Source: PRD §Slice 9a (amendment #58) + phase 13-RESEARCH.md §"Migration 0012" +
-- 13-PATTERNS.md (mirror 0009_scheduler / 0004_identity). UX-02 (the Telegram
-- channel persists known accounts) + UX-03 (the setup wizard mints single-use
-- onboarding tokens).
--
-- The number is 0012, NOT the PRD's stale "0008_telegram" (0008 is
-- proxied_child_id_text; the shipped floor is 0011_tool_invocations — RESEARCH
-- Pitfall 3).
--
-- Role separation (T-13-01-RoleEsc): aura_app gets DML only; DDL is reserved to
-- aura_migrate (mirrors 0009). The onboarding token is a single-use credential
-- (T-13-01-TokenReplay): consumed_at marks it spent, expires_at gives it a 1h TTL,
-- and the partial active index keys the SSE-pump cleanup scan.

CREATE TABLE aura.telegram_accounts (
    telegram_user_id bigint      PRIMARY KEY,
    identity_id      uuid        NOT NULL REFERENCES aura.identities (id),  -- Slice 1.7 FK (0004)
    username         text,
    first_name       text,
    added_at         timestamptz NOT NULL DEFAULT now(),
    last_seen_at     timestamptz
);

CREATE TABLE aura.telegram_setup_pending (
    onboarding_token text        PRIMARY KEY,
    identity_id      uuid        NOT NULL REFERENCES aura.identities (id),
    generated_by     text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    consumed_at      timestamptz
);

-- Partial index: the setup SSE pump + cleanup scan only ever look at still-active
-- (unconsumed) tokens by expiry (0009 scheduler_tasks_due_idx precedent for a
-- plain non-CONCURRENT partial index inside the migration tx).
CREATE INDEX telegram_setup_pending_active
    ON aura.telegram_setup_pending (expires_at) WHERE consumed_at IS NULL;

-- DML grants (NEVER TRUNCATE/DROP/CREATE to aura_app). telegram_setup_pending needs
-- UPDATE (consume marks consumed_at) + DELETE (cleanup expired rows).
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.telegram_accounts      TO aura_app;
GRANT ALL                            ON aura.telegram_accounts      TO aura_migrate;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.telegram_setup_pending TO aura_app;
GRANT ALL                            ON aura.telegram_setup_pending TO aura_migrate;

COMMENT ON TABLE aura.telegram_accounts IS
    'Known Telegram accounts (Slice 9a / Phase 13, amendment #58). PK telegram_user_id; identity_id FKs aura.identities (single-user `local` this phase, D-07).';
COMMENT ON TABLE aura.telegram_setup_pending IS
    'Single-use onboarding tokens (Slice 9a / Phase 13, amendment #58). consumed_at marks the token spent (T-13-01-TokenReplay); expires_at is a 1h TTL; the partial active index keys the cleanup scan.';
