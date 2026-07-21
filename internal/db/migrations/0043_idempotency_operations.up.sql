-- Identity-scoped public-operation registry. This state machine owns caller
-- intent and replay metadata; aura.tool_invocations remains the append-only
-- execution/audit ledger and is deliberately not altered here.

CREATE TABLE aura.idempotency_operations (
    identity_id          uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    operation_scope      text        NOT NULL,
    operation_key        text        NOT NULL,
    payload_hash         bytea       NOT NULL,
    state                text        NOT NULL,

    replay_body          jsonb,
    replay_preview       text,
    replay_sidecar_ref   text,
    replay_bytes         bigint      NOT NULL DEFAULT 0,
    replay_expires_at    timestamptz,

    lease_expires_at     timestamptz NOT NULL,
    retry_after          timestamptz NOT NULL,

    audit_conversation_id uuid,
    audit_request_id      uuid,
    audit_tool_call_id    text,

    version              bigint      NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL,
    updated_at           timestamptz NOT NULL,
    completed_at         timestamptz,
    indeterminate_at     timestamptz,

    PRIMARY KEY (identity_id, operation_scope, operation_key),

    CONSTRAINT idempotency_operations_scope_check CHECK (
        operation_scope IN (
            'http.mutation',
            'cli.command',
            'agent.tool',
            'scheduler.run',
            'approval.resume',
            'mcp.tool'
        )
    ),
    CONSTRAINT idempotency_operations_key_check CHECK (
        octet_length(operation_key) BETWEEN 1 AND 200
        AND operation_key !~ '[[:cntrl:]]'
    ),
    CONSTRAINT idempotency_operations_payload_hash_check CHECK (octet_length(payload_hash) = 32),
    CONSTRAINT idempotency_operations_state_check CHECK (state IN ('in_progress', 'completed', 'indeterminate')),
    CONSTRAINT idempotency_operations_replay_body_check CHECK (
        replay_body IS NULL OR octet_length(replay_body::text) <= 262144
    ),
    CONSTRAINT idempotency_operations_replay_preview_check CHECK (
        replay_preview IS NULL OR (
            octet_length(replay_preview) <= 4096
            AND replay_preview !~ '[[:cntrl:]]'
        )
    ),
    CONSTRAINT idempotency_operations_replay_sidecar_check CHECK (
        replay_sidecar_ref IS NULL OR (
            octet_length(replay_sidecar_ref) <= 512
            AND replay_sidecar_ref !~ '[[:cntrl:]]'
        )
    ),
    CONSTRAINT idempotency_operations_replay_bytes_check CHECK (replay_bytes >= 0),
    CONSTRAINT idempotency_operations_audit_tuple_check CHECK (
        (audit_conversation_id IS NULL AND audit_request_id IS NULL AND audit_tool_call_id IS NULL)
        OR
        (audit_conversation_id IS NOT NULL AND audit_request_id IS NOT NULL AND audit_tool_call_id IS NOT NULL)
    ),
    CONSTRAINT idempotency_operations_terminal_shape_check CHECK (
        (state = 'in_progress' AND completed_at IS NULL AND indeterminate_at IS NULL)
        OR
        (state = 'completed' AND completed_at IS NOT NULL AND indeterminate_at IS NULL)
        OR
        (state = 'indeterminate' AND completed_at IS NULL AND indeterminate_at IS NOT NULL)
    )
);

CREATE INDEX idempotency_operations_replay_expiry_idx
    ON aura.idempotency_operations (replay_expires_at, identity_id, operation_scope, operation_key)
    WHERE state = 'completed'
      AND replay_expires_at IS NOT NULL
      AND (replay_body IS NOT NULL OR replay_preview IS NOT NULL OR replay_sidecar_ref IS NOT NULL);

CREATE INDEX idempotency_operations_stale_in_progress_idx
    ON aura.idempotency_operations (lease_expires_at, identity_id, operation_scope, operation_key)
    WHERE state = 'in_progress';

CREATE INDEX idempotency_operations_audit_tuple_idx
    ON aura.idempotency_operations (audit_conversation_id, audit_request_id, audit_tool_call_id)
    WHERE audit_conversation_id IS NOT NULL;

GRANT SELECT, INSERT, UPDATE ON aura.idempotency_operations TO aura_app;
GRANT ALL ON aura.idempotency_operations TO aura_migrate;

COMMENT ON TABLE aura.idempotency_operations IS
    'Identity-scoped public-operation state and bounded replay metadata linked optionally to the append-only tool invocation audit tuple.';
