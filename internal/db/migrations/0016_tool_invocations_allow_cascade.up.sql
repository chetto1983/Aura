-- Fix: /clear (and `aura chat delete`) failed for any conversation that used a tool.
--
-- 0011 gave aura.tool_invocations BOTH `conversation_id ... ON DELETE CASCADE` AND a
-- BEFORE UPDATE OR DELETE trigger that rejected EVERY delete. Those are mutually
-- exclusive: deleting a conversation cascades a DELETE onto its tool_invocations rows,
-- the reject-all trigger raised, and the whole conversation delete aborted. A chat
-- that never called a tool had no rows to cascade, so the bug only bit real chats.
--
-- This permits the cascade teardown of a parent-conversation delete while still
-- rejecting standalone DELETE/UPDATE and TRUNCATE (the append-only / anti-tampering
-- guarantee). Detection is self-contained: during the ON DELETE CASCADE the parent
-- conversation row is already deleted within the same transaction, so a DELETE whose
-- conversation no longer exists IS the cascade (allowed); a DELETE whose conversation
-- still exists is surgical tampering (rejected). UPDATE and TRUNCATE never reference a
-- live parent and stay forbidden.

CREATE OR REPLACE FUNCTION aura.reject_tool_invocations_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE'
        AND NOT EXISTS (SELECT 1 FROM aura.conversations c WHERE c.id = OLD.conversation_id) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'aura.tool_invocations is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
