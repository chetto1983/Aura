-- Reverse 0008: retype proxied_from_child_id back to uuid. The USING cast fails if
-- any non-uuid flat id ("w1") was persisted, which is the intended forward-only
-- guard: a flat-id row cannot round-trip back into the uuid contract 0003 shipped.
ALTER TABLE aura.paused_states
    ALTER COLUMN proxied_from_child_id TYPE uuid USING proxied_from_child_id::uuid;
