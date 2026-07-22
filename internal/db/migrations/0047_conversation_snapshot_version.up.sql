-- Export-delete concurrency fence. snapshot_version is a monotonic token for every
-- persisted value included in conversation.json or its thread asset list. The delete
-- reservation is intentionally not part of the exported snapshot: it freezes all
-- snapshot writers before runtime teardown begins, then identifies the only delete
-- allowed to remove the reserved row.
ALTER TABLE aura.conversations
    ADD COLUMN snapshot_version bigint NOT NULL DEFAULT 1,
    ADD COLUMN delete_reservation text;

CREATE OR REPLACE FUNCTION aura.guard_conversation_snapshot_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    exported_changed boolean;
BEGIN
    exported_changed :=
        NEW.title IS DISTINCT FROM OLD.title OR
        NEW.last_active_at IS DISTINCT FROM OLD.last_active_at OR
        NEW.status IS DISTINCT FROM OLD.status OR
        NEW.model IS DISTINCT FROM OLD.model OR
        NEW.total_input_tokens IS DISTINCT FROM OLD.total_input_tokens OR
        NEW.total_output_tokens IS DISTINCT FROM OLD.total_output_tokens OR
        NEW.total_cached_tokens IS DISTINCT FROM OLD.total_cached_tokens OR
        NEW.total_cost_usd IS DISTINCT FROM OLD.total_cost_usd OR
        NEW.metadata IS DISTINCT FROM OLD.metadata;

    IF OLD.delete_reservation IS NOT NULL AND exported_changed THEN
        RAISE EXCEPTION 'conversation % is reserved for export-delete', OLD.id
            USING ERRCODE = '55006';
    END IF;
    IF exported_changed THEN
        NEW.snapshot_version := OLD.snapshot_version + 1;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER conversations_snapshot_write_guard
BEFORE UPDATE ON aura.conversations
FOR EACH ROW EXECUTE FUNCTION aura.guard_conversation_snapshot_write();

CREATE OR REPLACE FUNCTION aura.bump_conversation_snapshot(p_conversation_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE aura.conversations
    SET snapshot_version = snapshot_version + 1
    WHERE id = p_conversation_id
      AND delete_reservation IS NULL;

    IF NOT FOUND AND EXISTS (
        SELECT 1 FROM aura.conversations WHERE id = p_conversation_id
    ) THEN
        RAISE EXCEPTION 'conversation % is reserved for export-delete', p_conversation_id
            USING ERRCODE = '55006';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION aura.bump_conversation_snapshot_for_turn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM aura.bump_conversation_snapshot(NEW.conversation_id);
    RETURN NEW;
END;
$$;

CREATE TRIGGER conversation_turns_snapshot_bump
BEFORE INSERT OR UPDATE ON aura.conversation_turns
FOR EACH ROW EXECUTE FUNCTION aura.bump_conversation_snapshot_for_turn();

CREATE OR REPLACE FUNCTION aura.bump_conversation_snapshot_for_asset()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    old_conversation_id uuid;
    new_conversation_id uuid;
BEGIN
    IF TG_OP <> 'INSERT' AND OLD.thread_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        SELECT id INTO old_conversation_id
        FROM aura.conversations
        WHERE id = OLD.thread_id::uuid
          AND identity_id = OLD.identity_id;
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.thread_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        SELECT id INTO new_conversation_id
        FROM aura.conversations
        WHERE id = NEW.thread_id::uuid
          AND identity_id = NEW.identity_id;
    END IF;

    IF old_conversation_id IS NOT NULL THEN
        PERFORM aura.bump_conversation_snapshot(old_conversation_id);
    END IF;
    IF new_conversation_id IS NOT NULL AND new_conversation_id IS DISTINCT FROM old_conversation_id THEN
        PERFORM aura.bump_conversation_snapshot(new_conversation_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER assets_snapshot_bump
BEFORE INSERT OR UPDATE OR DELETE ON aura.assets
FOR EACH ROW EXECUTE FUNCTION aura.bump_conversation_snapshot_for_asset();
