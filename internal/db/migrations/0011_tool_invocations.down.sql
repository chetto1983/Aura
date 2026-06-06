DROP TRIGGER IF EXISTS tool_invocations_no_truncate      ON aura.tool_invocations;
DROP TRIGGER IF EXISTS tool_invocations_no_update_delete ON aura.tool_invocations;
DROP TABLE   IF EXISTS aura.tool_invocations;
DROP FUNCTION IF EXISTS aura.reject_tool_invocations_mutation();
