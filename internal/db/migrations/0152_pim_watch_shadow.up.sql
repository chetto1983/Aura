-- The urgency centroid's shadow (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md, decision 7).
-- A correction overwrites tier and decided_by, so the ladder's own answer is kept apart and never
-- changed. The four facts are the centroid's; sender_memory_tier is the per-sender baseline's. No
-- sender and no text.
ALTER TABLE aura.pim_watch_verdicts
    ADD COLUMN ladder_tier        text CHECK (ladder_tier IN ('now', 'today', 'later')),
    ADD COLUMN ladder_by          text CHECK (ladder_by IN ('rule', 'auth_failed', 'people', 'category', 'default')),
    ADD COLUMN centroid_tier      text CHECK (centroid_tier IN ('now', 'today', 'later')),
    ADD COLUMN centroid_margin    double precision,
    ADD COLUMN sender_memory_tier text CHECK (sender_memory_tier IN ('now', 'today', 'later')),
    ADD COLUMN written_to         boolean NOT NULL DEFAULT false,
    ADD COLUMN provider_important boolean NOT NULL DEFAULT false,
    ADD COLUMN bulk               boolean NOT NULL DEFAULT false;
