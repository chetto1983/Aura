-- Source: phase 09-swarm-minimal CR-01. The D-05 swarm-proxy plumb relays the
-- originating child's id into aura.paused_states.proxied_from_child_id. That id is
-- the deterministic FLAT worker id ("w1".."wN", D-15/D-16) — there is no uuid
-- anywhere in the swarm report a model could copy. The original uuid column (0003)
-- made the documented happy path (relay the report's child id, which is "w1")
-- fail parseUUID and abort the whole pause write. Retype the column to text so the
-- flat id persists verbatim, mirroring proxied_tool_call_id (already text).
--
-- The column is NULL for all direct (non-proxied) pauses; Phase 9 is its first
-- populating writer, so no existing rows carry a uuid value to lose precision on.

ALTER TABLE aura.paused_states ALTER COLUMN proxied_from_child_id TYPE text;
