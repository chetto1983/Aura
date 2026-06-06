-- Durable tool invocation ledger. This is operational observability, not a
-- permission system: every dispatched tool writes an append-only start fact and
-- an append-only end fact correlated by conversation_id/request_id/tool_call_id.

CREATE TABLE aura.tool_invocations (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id      uuid        NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    request_id           uuid        NOT NULL,
    tool_call_id         text        NOT NULL,
    tool_name            text        NOT NULL,
    event_kind           text        NOT NULL CHECK (event_kind IN ('start', 'end')),
    seq                  integer     NOT NULL,
    ts                   timestamptz NOT NULL,

    started_at           timestamptz,
    ended_at             timestamptz,
    duration_ms          bigint,

    args_raw             text,
    args_bytes           integer,

    status               text        CHECK (status IS NULL OR status IN ('ok', 'error')),
    error                text,
    result_preview       text,
    preview_bytes        integer,
    result_bytes         integer,
    result_truncated     boolean,
    result_sidecar_path  text,
    exit_code            integer,
    meta                 jsonb,

    CONSTRAINT tool_invocations_event_shape CHECK (
        (event_kind = 'start'
            AND started_at IS NOT NULL
            AND ended_at IS NULL
            AND duration_ms IS NULL
            AND status IS NULL)
        OR
        (event_kind = 'end'
            AND ended_at IS NOT NULL
            AND status IS NOT NULL)
    )
);

CREATE UNIQUE INDEX tool_invocations_once_per_phase_idx
    ON aura.tool_invocations (conversation_id, request_id, tool_call_id, event_kind);

CREATE INDEX tool_invocations_conversation_seq_idx
    ON aura.tool_invocations (conversation_id, seq);

CREATE INDEX tool_invocations_tool_ts_idx
    ON aura.tool_invocations (tool_name, ts DESC);

CREATE FUNCTION aura.reject_tool_invocations_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.tool_invocations is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER tool_invocations_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.tool_invocations
    FOR EACH ROW EXECUTE FUNCTION aura.reject_tool_invocations_mutation();

CREATE TRIGGER tool_invocations_no_truncate
    BEFORE TRUNCATE ON aura.tool_invocations
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_tool_invocations_mutation();

GRANT SELECT, INSERT ON aura.tool_invocations TO aura_app;
GRANT ALL            ON aura.tool_invocations TO aura_migrate;

COMMENT ON TABLE aura.tool_invocations IS
    'Append-only tool invocation ledger: start/end facts for dispatched tools with args, timestamps, status, bytes, sidecar path, exit code, and typed metadata.';
