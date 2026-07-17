-- Reverse 0041: drop the shared_links owner-isolation policy and DISABLE row-level
-- security, restoring the pre-37F-20 (RLS-free) access shape for aura.shared_links.
-- aura.share_audit never had RLS in this migration, so there is nothing to reverse
-- there.

DROP POLICY IF EXISTS shared_links_owner_isolation ON aura.shared_links;
ALTER TABLE aura.shared_links DISABLE ROW LEVEL SECURITY;
