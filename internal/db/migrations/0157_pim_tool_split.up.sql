-- The PIM advertises one tool per operation (docs/superpowers/specs/2026-11-23-pim-tool-split-design.md
-- §5.5), so an email draft targets its send_email tool with no action. The multiplexed shape stays
-- allowed: resolved drafts keep it as history, and pending ones hold it until the Go upgrade step
-- rewrites them. Slot measured with `ls internal/db/migrations/ | tail -1`: 0156 was the head.
--
-- 0134 declared the target CHECK unnamed, beside three other unnamed table CHECKs, so it is found by
-- its definition: it is the only one naming send_message.
DO $$
DECLARE
    target_check text;
BEGIN
    SELECT conname INTO STRICT target_check FROM pg_constraint
    WHERE conrelid = 'aura.message_drafts'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%send_message%';
    EXECUTE format('ALTER TABLE aura.message_drafts DROP CONSTRAINT %I', target_check);
END
$$;

ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_target_check CHECK (
    (recipe = 'recipe:calendar' AND ((tool_name = 'send_email' AND action = '') OR
                                     (tool_name = 'calendar' AND action = 'send_email'))) OR
    (recipe = 'recipe:whatsapp' AND tool_name = 'send_message' AND action = ''));
