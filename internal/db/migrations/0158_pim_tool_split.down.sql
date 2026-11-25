-- Restores 0134's target CHECK. It fails while any draft holds the send_email shape
-- (recipe:calendar, send_email, ''): such a row has no multiplexed equivalent this migration could
-- write back, so it must be removed, or rewritten by hand, before going down.
ALTER TABLE aura.message_drafts DROP CONSTRAINT message_drafts_target_check;
ALTER TABLE aura.message_drafts ADD CHECK (
    (recipe = 'recipe:calendar' AND tool_name = 'calendar' AND action = 'send_email') OR
    (recipe = 'recipe:whatsapp' AND tool_name = 'send_message' AND action = ''));
