-- Restore 0011's reject-all-mutation function (rejects every UPDATE/DELETE/TRUNCATE,
-- including the conversation ON DELETE CASCADE — which re-breaks /clear for tool-using
-- conversations; that is the documented pre-0016 behavior this down migration reverts to).

CREATE OR REPLACE FUNCTION aura.reject_tool_invocations_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.tool_invocations is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
