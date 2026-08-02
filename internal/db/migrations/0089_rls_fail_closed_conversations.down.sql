-- Restore the permissive-on-unset policies of 0032 (conversations, conversation_turns,
-- paused_states) and 0041 (shared_links), byte-for-byte as those migrations wrote them, and
-- drop the restrictive floors this migration added.
--
-- This rollback is real, not cosmetic: it makes the four tables readable again by a
-- connection with no app.current_identity, which is exactly what a deployment needs if it
-- has to run an older binary whose conversations/askuser stores do not set the session
-- variable. Rolling back the schema without rolling back the code would otherwise leave the
-- daemon able to write turns but unable to read them.
--
-- Order matters: drop the restrictive floors FIRST. A restrictive policy AND-combines with
-- everything, so leaving one in place while the permissive policy is being swapped would
-- make the intermediate state stricter than either version.

DROP POLICY IF EXISTS conversations_requires_identity      ON aura.conversations;
DROP POLICY IF EXISTS conversation_turns_requires_identity ON aura.conversation_turns;
DROP POLICY IF EXISTS paused_states_requires_identity      ON aura.paused_states;
DROP POLICY IF EXISTS shared_links_owner_or_unrevoked      ON aura.shared_links;
DROP POLICY IF EXISTS shared_links_unrevoked_resolve       ON aura.shared_links;

DROP POLICY IF EXISTS conversations_owner_isolation ON aura.conversations;
CREATE POLICY conversations_owner_isolation ON aura.conversations
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

DROP POLICY IF EXISTS conversation_turns_owner_isolation ON aura.conversation_turns;
CREATE POLICY conversation_turns_owner_isolation ON aura.conversation_turns
    USING (EXISTS (SELECT 1 FROM aura.conversations c WHERE c.id = conversation_id));

DROP POLICY IF EXISTS paused_states_owner_isolation ON aura.paused_states;
CREATE POLICY paused_states_owner_isolation ON aura.paused_states
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

DROP POLICY IF EXISTS shared_links_owner_isolation ON aura.shared_links;
CREATE POLICY shared_links_owner_isolation ON aura.shared_links
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

COMMENT ON POLICY conversations_owner_isolation ON aura.conversations IS
    'Phase 36 D-07 owner isolation: fail-closed-on-mismatch, permissive-on-unset. Set via internal/db.WithIdentityTx SET LOCAL app.current_identity.';
COMMENT ON POLICY paused_states_owner_isolation ON aura.paused_states IS
    'Phase 36 D-07 owner isolation on the 0027 identity_id column (auto-populated from the parent conversation by paused_states_set_identity_trg).';
COMMENT ON POLICY conversation_turns_owner_isolation ON aura.conversation_turns IS
    'Phase 36 D-07 owner isolation via the parent conversation (turns carry no identity_id).';
COMMENT ON POLICY shared_links_owner_isolation ON aura.shared_links IS
    'Phase 37F-20 gap closure: owner isolation mirroring 0032_owner_rls exactly (fail-closed-on-mismatch, permissive-on-unset). Set via internal/db.WithIdentityTxRaw SET LOCAL app.current_identity. ResolveByToken/ResolveLiveByID deliberately read on the unset-var plain pool by design — do not tighten to fail-closed-on-unset.';

-- Restore 0016's SECURITY INVOKER tamper guard. It is only correct while the conversation
-- plane is readable without an identity, which the policies above have just made true again.
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
