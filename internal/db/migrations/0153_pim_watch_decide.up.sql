-- The decision model for labels (docs/superpowers/specs/2026-11-03-decision-model-labels-design.md).
-- A label gains the person's description, the only text of it the model reads, and keep or discard.
ALTER TABLE aura.pim_watch_labels
    ADD COLUMN description text    NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
    ADD COLUMN discard     boolean NOT NULL DEFAULT false;

-- The default descriptions reach an identity that already has its labels once (spec decision 12).
ALTER TABLE aura.pim_watch_settings
    ADD COLUMN label_descriptions_seeded boolean NOT NULL DEFAULT false;

-- What the model said, kept apart from the tier and label a correction overwrites (spec decision 11).
-- model_probability NULL: not asked. model_label_id NULL beside a probability: it chose none, or the label
-- is gone. model_tier: set only when its label was applied. model_skipped: the step was on, this mail was
-- not asked (budget, failure, no snippet).
ALTER TABLE aura.pim_watch_verdicts
    ADD COLUMN model_label_id    uuid REFERENCES aura.pim_watch_labels (id) ON DELETE SET NULL,
    ADD COLUMN model_probability double precision CHECK (model_probability BETWEEN 0 AND 1),
    ADD COLUMN model_name        text,
    ADD COLUMN model_tier        text CHECK (model_tier IN ('now', 'today', 'later')),
    ADD COLUMN model_discarded   boolean  NOT NULL DEFAULT false,
    ADD COLUMN model_options     smallint CHECK (model_options BETWEEN 2 AND 255),
    ADD COLUMN model_skipped     boolean  NOT NULL DEFAULT false,
    ADD COLUMN discarded         boolean  NOT NULL DEFAULT false;

ALTER TABLE aura.pim_watch_verdicts DROP CONSTRAINT pim_watch_verdicts_decided_by_check;
ALTER TABLE aura.pim_watch_verdicts ADD CONSTRAINT pim_watch_verdicts_decided_by_check CHECK (decided_by IN
    ('rule', 'auth_failed', 'people', 'category', 'default', 'calendar', 'person', 'model'));

-- One row per account a person set something on: the cloud decision model's consent (spec decision 9) and
-- the model's last failure, a class with no text. It survives turning the watch off and on, and leaves with
-- the account.
CREATE TABLE aura.pim_watch_accounts (
    identity_id      uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    account_id       text        NOT NULL,
    cloud_decide     boolean     NOT NULL DEFAULT false,
    decide_error     text        NOT NULL DEFAULT '',
    decide_failed_at timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (identity_id, account_id)
);
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.pim_watch_accounts TO aura_app;
GRANT ALL ON aura.pim_watch_accounts TO aura_migrate;
ALTER TABLE aura.pim_watch_accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY pim_watch_accounts_owner_isolation ON aura.pim_watch_accounts
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY pim_watch_accounts_require_identity ON aura.pim_watch_accounts
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');
