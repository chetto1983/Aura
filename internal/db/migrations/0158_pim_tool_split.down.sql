-- Restores 0134's target CHECK. A draft holding the send_email shape (recipe:calendar, send_email, '')
-- has no multiplexed equivalent this migration could write back, so it is deleted first, with the
-- scheduled-message task that would send it, or the down aborts mid-chain (same shape as 0155).
-- Rolling back past 0158 loses those drafts, resolved history included.
DELETE FROM aura.scheduler_tasks WHERE id IN (
    SELECT scheduled_task_id FROM aura.message_drafts
    WHERE recipe = 'recipe:calendar' AND tool_name = 'send_email' AND action = '');
DELETE FROM aura.message_drafts WHERE recipe = 'recipe:calendar' AND tool_name = 'send_email' AND action = '';

ALTER TABLE aura.message_drafts DROP CONSTRAINT message_drafts_target_check;
ALTER TABLE aura.message_drafts ADD CHECK (
    (recipe = 'recipe:calendar' AND tool_name = 'calendar' AND action = 'send_email') OR
    (recipe = 'recipe:whatsapp' AND tool_name = 'send_message' AND action = ''));
