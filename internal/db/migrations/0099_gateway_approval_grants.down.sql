-- Drop the durable approval grants.
--
-- Rolling back costs exactly the `always` scope: every withheld destructive call falls back
-- to being asked once (or once per conversation, if the operator picks the session scope),
-- which is the pre-amendment-#127 behaviour and is fail-closed. Nothing else reads it.

DROP TABLE IF EXISTS aura.gateway_approval_grants;
