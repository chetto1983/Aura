-- Where reminders and scheduled results go when a request names no channel
-- (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md, Part 1).
-- '' is "not set": the task tool then asks, as before. A setting, not a memory (0097).
ALTER TABLE aura.identity_profiles
    ADD COLUMN default_notify text NOT NULL DEFAULT ''
    CHECK (default_notify IN ('', 'none', 'stdout', 'telegram', 'whatsapp', 'email'));
