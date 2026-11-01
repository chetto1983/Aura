-- The mail and calendar watch (docs/superpowers/specs/2026-10-31-mail-calendar-watch-design.md §5.2,
-- §5.4, §5.8; prd.md §11, "Mail: ids and verdicts, not a mail index", and §15, 2026-11-01).
--
-- Two scheduler kinds: pim_watch, one task per watched account, and pim_digest, one task per
-- identity. Neither calls a model. The member list is 0142's (the latest migration to touch this
-- constraint) plus the two.
--
-- No table here holds a subject or a body. pim_watch_verdicts holds no sender either: the only
-- sender text is the key of a rule or a correction. Sender and subject are read live from the PIM
-- by id, so a deleted mail shows as gone. An event's start and a fixed change word are kept, and
-- neither is mail text: a deleted event names only its id, so what is known of it has to be here.

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill', 'observability_check',
        'memory_mention_link', 'backup_objects', 'pim_watch', 'pim_digest'));

-- Deactivating an identity pauses its tasks, and the reason says so. 0138's list plus the one.
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_paused_reason_chk;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_paused_reason_chk
    CHECK (paused_reason IN ('operator', 'failures', 'identity_deactivated'));

-- Per-identity settings, read at use and never snapshotted at boot.
CREATE TABLE aura.pim_watch_settings (
    identity_id            uuid        PRIMARY KEY REFERENCES aura.identities (id) ON DELETE CASCADE,
    digest_hour            smallint    NOT NULL DEFAULT 18 CHECK (digest_hour BETWEEN 0 AND 23),
    daily_alert_cap        integer     NOT NULL DEFAULT 5 CHECK (daily_alert_cap BETWEEN 0 AND 100),
    interval_minutes       integer     NOT NULL DEFAULT 5 CHECK (interval_minutes >= 5),
    last_digest_at         timestamptz,
    verdict_retention_days integer     NOT NULL DEFAULT 30 CHECK (verdict_retention_days > 0),
    -- The default labels go in once: a person who removes them all does not get them back.
    labels_seeded          boolean     NOT NULL DEFAULT false,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- The person's own words for what a mail is about (§5.8). A label's tier, when it has one, acts
-- as a rule.
CREATE TABLE aura.pim_watch_labels (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    tier        text        CHECK (tier IN ('now', 'today', 'later')),
    position    integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, name)
);

-- One cursor per (identity, account, resource). `cursor` may hold a provider URL, so nothing logs
-- it. last_error is a fixed code, never the PIM's text. An alarm stays owed on its row until the
-- ledger shows the run that told it completed. alerts_told counts what this resource pushed since
-- alerts_day_start, the identity's local midnight. The daily cap is per account, so it sums the
-- account's rows on today's start. It cannot count verdicts: a moved event keeps one row however
-- many times it is told.
CREATE TABLE aura.pim_watch_cursors (
    identity_id      uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    account_id       text        NOT NULL,
    resource         text        NOT NULL CHECK (resource IN ('mail', 'calendar')),
    cursor           text        NOT NULL,
    status           text        NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'failing', 'unsupported')),
    last_error       text,
    failing_since    timestamptz,
    alarm_kind       text        CHECK (alarm_kind IN ('gap', 'failing')),
    alarm_since      timestamptz,
    alarm_run_id     uuid,
    advanced_at      timestamptz NOT NULL DEFAULT now(),
    alerts_day_start timestamptz,
    alerts_told      integer     NOT NULL DEFAULT 0 CHECK (alerts_told >= 0),
    PRIMARY KEY (identity_id, account_id, resource)
);

-- "Handled" means a row here (§5.2). alert_run_id names the run that told this verdict in a
-- notification, and notified turns true once the ledger shows that run completed, so a run that
-- died after committing tells it again and a run that finished never does. An event's row keeps
-- its start and what happened to it: event_all_day says the start is a date in no zone, and
-- event_change stays null when nothing shows what changed.
CREATE TABLE aura.pim_watch_verdicts (
    identity_id   uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    account_id    text        NOT NULL,
    resource      text        NOT NULL CHECK (resource IN ('mail', 'calendar')),
    item_id       text        NOT NULL,
    calendar_id   text        NOT NULL DEFAULT '',
    tier          text        NOT NULL CHECK (tier IN ('now', 'today', 'later')),
    label_id      uuid        REFERENCES aura.pim_watch_labels (id) ON DELETE SET NULL,
    decided_by    text        NOT NULL CHECK (decided_by IN
        ('rule', 'auth_failed', 'people', 'category', 'default', 'calendar', 'person')),
    decided_at    timestamptz NOT NULL DEFAULT now(),
    auth_failed   boolean     NOT NULL DEFAULT false,
    fingerprint   text        NOT NULL DEFAULT '',
    event_start   timestamptz,
    event_all_day boolean     NOT NULL DEFAULT false,
    event_change  text        CHECK (event_change IN ('new', 'moved', 'cancelled')),
    alert_run_id  uuid,
    alerted_at    timestamptz,
    notified      boolean     NOT NULL DEFAULT false,
    PRIMARY KEY (identity_id, account_id, resource, item_id)
);

CREATE INDEX pim_watch_verdicts_decided ON aura.pim_watch_verdicts (identity_id, decided_at);
CREATE INDEX pim_watch_verdicts_owed ON aura.pim_watch_verdicts (identity_id, account_id)
    WHERE alert_run_id IS NOT NULL AND NOT notified;

-- A rule maps a sender or a domain to a tier, a label or both (§5.3, §5.4). The person may write
-- one, and a correction repeated outside 15 minutes writes one.
CREATE TABLE aura.pim_watch_rules (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    key         text        NOT NULL CHECK (char_length(key) BETWEEN 1 AND 320),
    key_kind    text        NOT NULL CHECK (key_kind IN ('sender', 'domain')),
    tier        text        CHECK (tier IN ('now', 'today', 'later')),
    label_id    uuid        REFERENCES aura.pim_watch_labels (id) ON DELETE SET NULL,
    source      text        NOT NULL CHECK (source IN ('person', 'correction')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, key_kind, key)
);

-- What the person changed in the decisions view or in chat, the feed §5.4 says must be proven
-- non-empty. Tier and label are independent, so either may be null.
CREATE TABLE aura.pim_watch_corrections (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    account_id  text        NOT NULL,
    sender_key  text        NOT NULL,
    tier        text        CHECK (tier IN ('now', 'today', 'later')),
    label_id    uuid        REFERENCES aura.pim_watch_labels (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX pim_watch_corrections_sender ON aura.pim_watch_corrections (identity_id, sender_key, created_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.pim_watch_settings, aura.pim_watch_labels,
    aura.pim_watch_cursors, aura.pim_watch_verdicts, aura.pim_watch_rules,
    aura.pim_watch_corrections TO aura_app;
GRANT ALL ON aura.pim_watch_settings, aura.pim_watch_labels, aura.pim_watch_cursors,
    aura.pim_watch_verdicts, aura.pim_watch_rules, aura.pim_watch_corrections TO aura_migrate;

-- Fail-closed RLS, the 0087 pair, on all six: a verdict says what one person's mail was, and a
-- connection that has not said whose it means must see none.
ALTER TABLE aura.pim_watch_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.pim_watch_labels ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.pim_watch_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.pim_watch_verdicts ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.pim_watch_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.pim_watch_corrections ENABLE ROW LEVEL SECURITY;

CREATE POLICY pim_watch_settings_owner_isolation ON aura.pim_watch_settings
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_settings_require_identity ON aura.pim_watch_settings
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY pim_watch_labels_owner_isolation ON aura.pim_watch_labels
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_labels_require_identity ON aura.pim_watch_labels
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY pim_watch_cursors_owner_isolation ON aura.pim_watch_cursors
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_cursors_require_identity ON aura.pim_watch_cursors
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY pim_watch_verdicts_owner_isolation ON aura.pim_watch_verdicts
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_verdicts_require_identity ON aura.pim_watch_verdicts
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY pim_watch_rules_owner_isolation ON aura.pim_watch_rules
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_rules_require_identity ON aura.pim_watch_rules
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY pim_watch_corrections_owner_isolation ON aura.pim_watch_corrections
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_corrections_require_identity ON aura.pim_watch_corrections
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

-- One live watch per account and one live digest per identity. A double click on the switch would
-- otherwise leave two tasks walking one cursor.
CREATE UNIQUE INDEX scheduler_tasks_one_pim_watch
    ON aura.scheduler_tasks (identity_id, (payload ->> 'account_id'))
    WHERE kind = 'pim_watch' AND status IN ('active', 'paused');
CREATE UNIQUE INDEX scheduler_tasks_one_pim_digest
    ON aura.scheduler_tasks (identity_id)
    WHERE kind = 'pim_digest' AND status IN ('active', 'paused');
