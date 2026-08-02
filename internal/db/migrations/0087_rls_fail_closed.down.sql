-- Reverse 0087: drop the restrictive floor and the owner policies, and take the twelve
-- tables back out of row-level security. 0087 touched no pre-existing policy, so nothing
-- from 0032 or 0041 has to be restored here — after this runs, `\d` on each table below
-- must read exactly as it did at 0086.

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'documents',
        'document_versions',
        'document_chunks',
        'document_embeddings',
        'document_tags',
        'capability_grants',
        'content_parts',
        'content_part_links',
        'idempotency_operations',
        'identity_object_store'
    ]
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS %I ON aura.%I', target || '_requires_identity', target);
        EXECUTE format('DROP POLICY IF EXISTS %I ON aura.%I', target || '_owner_isolation', target);
        EXECUTE format('ALTER TABLE aura.%I DISABLE ROW LEVEL SECURITY', target);
    END LOOP;
END
$$;
