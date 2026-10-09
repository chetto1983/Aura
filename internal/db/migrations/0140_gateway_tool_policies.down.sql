-- Drop the per-identity tool policies.
--
-- Rolling back costs exactly the narrowing: a tool an operator had set to `ask` falls back
-- to its tier, and one set to `deny` runs again under its tier. That is the pre-policy
-- behaviour, and it is the one direction this table's absence can move: nothing it denied
-- is granted by its removal beyond what the tiers already allowed.

DROP TABLE IF EXISTS aura.gateway_tool_policies;
