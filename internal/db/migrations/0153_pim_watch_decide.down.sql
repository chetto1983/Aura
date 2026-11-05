DROP TABLE aura.pim_watch_accounts;

UPDATE aura.pim_watch_verdicts SET decided_by = COALESCE(ladder_by, 'default') WHERE decided_by = 'model';

ALTER TABLE aura.pim_watch_verdicts DROP CONSTRAINT pim_watch_verdicts_decided_by_check;
ALTER TABLE aura.pim_watch_verdicts ADD CONSTRAINT pim_watch_verdicts_decided_by_check CHECK (decided_by IN
    ('rule', 'auth_failed', 'people', 'category', 'default', 'calendar', 'person'));

ALTER TABLE aura.pim_watch_verdicts
    DROP COLUMN model_label_id,
    DROP COLUMN model_probability,
    DROP COLUMN model_name,
    DROP COLUMN model_tier,
    DROP COLUMN model_discarded,
    DROP COLUMN model_options,
    DROP COLUMN model_skipped,
    DROP COLUMN discarded;
ALTER TABLE aura.pim_watch_settings DROP COLUMN label_descriptions_seeded;
ALTER TABLE aura.pim_watch_labels DROP COLUMN description, DROP COLUMN discard;
