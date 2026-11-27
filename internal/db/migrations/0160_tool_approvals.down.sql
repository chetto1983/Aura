DROP TRIGGER IF EXISTS paused_states_refuse_pending_tool_approval_trg ON aura.paused_states;
DROP FUNCTION IF EXISTS aura.refuse_resume_of_pending_tool_approval();
DROP TABLE IF EXISTS aura.tool_approvals;
