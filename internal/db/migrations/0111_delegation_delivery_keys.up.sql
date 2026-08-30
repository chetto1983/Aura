ALTER TABLE aura.conversation_turns
    ADD COLUMN delivery_key text;

CREATE UNIQUE INDEX conversation_turns_delivery_key_uniq
    ON aura.conversation_turns (conversation_id, delivery_key)
    WHERE delivery_key IS NOT NULL;

COMMENT ON COLUMN aura.conversation_turns.delivery_key IS
    'Amendment #190: optional job-level idempotency key for durable asynchronous delivery.';

ALTER TABLE aura.steer_queue
    ADD COLUMN delivery_key text;

CREATE UNIQUE INDEX steer_queue_delivery_key_uniq
    ON aura.steer_queue (identity_id, conversation_id, delivery_key)
    WHERE delivery_key IS NOT NULL;

COMMENT ON COLUMN aura.steer_queue.delivery_key IS
    'Amendment #190: optional job-level idempotency key for durable asynchronous delivery.';
