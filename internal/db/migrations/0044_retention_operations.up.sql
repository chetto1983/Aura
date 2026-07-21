-- Durable two-phase retention plans. External bytes are removed while an item is
-- deleting; metadata is finalized only after artifact_result is removed/absent.

CREATE TABLE aura.retention_operations (
    id               uuid        PRIMARY KEY,
    token            text        NOT NULL UNIQUE,
    policy_version   text        NOT NULL,
    status           text        NOT NULL DEFAULT 'planned',
    candidate_count  integer     NOT NULL,
    planned_bytes    bigint      NOT NULL DEFAULT 0,
    completed_count  integer     NOT NULL DEFAULT 0,
    completed_bytes  bigint      NOT NULL DEFAULT 0,
    failure_count    integer     NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    completed_at     timestamptz,
    CONSTRAINT retention_operations_token_check CHECK (token ~ '^[0-9a-f]{64}$'),
    CONSTRAINT retention_operations_status_check CHECK (
        status IN ('planned', 'deleting', 'retryable', 'completed', 'failed')
    ),
    CONSTRAINT retention_operations_counts_check CHECK (
        candidate_count >= 0 AND planned_bytes >= 0 AND completed_count >= 0
        AND completed_bytes >= 0 AND failure_count >= 0
    )
);

CREATE TABLE aura.retention_operation_items (
    id                  uuid        PRIMARY KEY,
    operation_id        uuid        NOT NULL REFERENCES aura.retention_operations (id),
    item_key            text        NOT NULL,
    identity_id         text        NOT NULL,
    conversation_id     text,
    artifact_id         text,
    class               text        NOT NULL,
    action              text        NOT NULL,
    expected_version    bigint      NOT NULL,
    expected_bytes      bigint      NOT NULL DEFAULT 0,
    status              text        NOT NULL DEFAULT 'planned',
    artifact_result     text        NOT NULL DEFAULT 'pending',
    removed_bytes       bigint      NOT NULL DEFAULT 0,
    failure_class       text,
    claim_owner         text,
    lease_expires_at    timestamptz,
    attempt_count       integer     NOT NULL DEFAULT 0,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    completed_at        timestamptz,
    UNIQUE (operation_id, item_key),
    CONSTRAINT retention_items_identity_check CHECK (
        octet_length(identity_id) BETWEEN 1 AND 200 AND identity_id !~ '[[:cntrl:]/\\]'
    ),
    CONSTRAINT retention_items_status_check CHECK (
        status IN ('planned', 'deleting', 'retryable', 'completed', 'failed')
    ),
    CONSTRAINT retention_items_artifact_result_check CHECK (
        artifact_result IN ('pending', 'removed', 'absent', 'failed')
    ),
    CONSTRAINT retention_items_expected_check CHECK (
        expected_version >= 0 AND expected_bytes >= 0 AND removed_bytes >= 0 AND attempt_count >= 0
    )
);

CREATE INDEX retention_items_claim_idx
    ON aura.retention_operation_items (operation_id, status, lease_expires_at, item_key)
    WHERE status IN ('planned', 'retryable', 'deleting');

GRANT SELECT, INSERT, UPDATE ON aura.retention_operations TO aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.retention_operation_items TO aura_app;
GRANT ALL ON aura.retention_operations, aura.retention_operation_items TO aura_migrate;

COMMENT ON TABLE aura.retention_operations IS
    'Immutable retention snapshots and bounded aggregate audit results.';
COMMENT ON TABLE aura.retention_operation_items IS
    'Crash-resumable mark-remove-finalize items claimed by short renewable leases.';
