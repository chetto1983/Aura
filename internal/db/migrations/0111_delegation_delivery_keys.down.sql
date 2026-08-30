DROP INDEX IF EXISTS aura.steer_queue_delivery_key_uniq;
ALTER TABLE aura.steer_queue DROP COLUMN IF EXISTS delivery_key;

DROP INDEX IF EXISTS aura.conversation_turns_delivery_key_uniq;
ALTER TABLE aura.conversation_turns DROP COLUMN IF EXISTS delivery_key;
