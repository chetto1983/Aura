-- Restore the pre-0090 state of aura.assets and aura.asset_events: NO policies and NO row
-- security, exactly as migration 0020 created them.
--
-- This down is shorter than 0089's because there is nothing to put back. 0089 rolled a
-- fail-closed predicate back to a fail-OPEN one that 0032 and 0041 had written; here the
-- prior state is the absence of row security altogether, verified on the live deployment
-- before 0090 was written (relrowsecurity=false, relforcerowsecurity=false, zero policies on
-- both tables).
--
-- The rollback is real, not cosmetic: it is what a deployment needs if it has to run a
-- binary older than the assets.Store reshape that lands with 0090 — one whose store opens no
-- identity-scoped transaction and would therefore fail 42501 on every Create against the
-- restrictive floor. Rolling the schema back without the code is the outage 0087 caused;
-- rolling the code back without the schema is the same outage in the other direction.
--
-- Order matters, for the same reason 0089 gives: drop the restrictive floors FIRST. A
-- restrictive policy AND-combines with everything, so leaving one in place while the
-- permissive policies are dropped would make the intermediate state stricter than either
-- version — with no permissive policy left to OR against, the table would deny everything.

DROP POLICY IF EXISTS assets_requires_identity       ON aura.assets;
DROP POLICY IF EXISTS asset_events_requires_identity ON aura.asset_events;

DROP POLICY IF EXISTS assets_owner_isolation       ON aura.assets;
DROP POLICY IF EXISTS asset_events_owner_isolation ON aura.asset_events;

ALTER TABLE aura.asset_events DISABLE ROW LEVEL SECURITY;
ALTER TABLE aura.assets       DISABLE ROW LEVEL SECURITY;
