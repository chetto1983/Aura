DROP TRIGGER IF EXISTS assets_snapshot_bump ON aura.assets;
DROP FUNCTION IF EXISTS aura.bump_conversation_snapshot_for_asset();
DROP TRIGGER IF EXISTS conversation_turns_snapshot_bump ON aura.conversation_turns;
DROP FUNCTION IF EXISTS aura.bump_conversation_snapshot_for_turn();
DROP FUNCTION IF EXISTS aura.bump_conversation_snapshot(uuid);
DROP TRIGGER IF EXISTS conversations_snapshot_write_guard ON aura.conversations;
DROP FUNCTION IF EXISTS aura.guard_conversation_snapshot_write();

ALTER TABLE aura.conversations
    DROP COLUMN IF EXISTS delete_reservation,
    DROP COLUMN IF EXISTS snapshot_version;
