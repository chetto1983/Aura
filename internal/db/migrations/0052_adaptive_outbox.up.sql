-- Adaptive intelligence control-plane ledger.
--
-- The application commits typed decisions and outcomes here before a projector
-- copies them to the private Neo4j adaptive graph.  Postgres is authoritative:
-- a graph outage can delay learning, but cannot lose or reorder execution facts.
CREATE TABLE aura.adaptive_aggregate_sequences (
    owner_id       uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    aggregate_id   text NOT NULL CHECK (btrim(aggregate_id) <> ''),
    next_sequence  bigint NOT NULL CHECK (next_sequence >= 1),
    PRIMARY KEY (owner_id, aggregate_id)
);

CREATE TABLE aura.adaptive_outbox (
    id                uuid PRIMARY KEY,
    owner_id          uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    aggregate_id      text NOT NULL CHECK (btrim(aggregate_id) <> ''),
    sequence          bigint NOT NULL CHECK (sequence >= 1),
    decision_id       uuid NOT NULL,
    event_kind        text NOT NULL CHECK (
        event_kind IN ('decision', 'outcome', 'correction', 'promotion', 'rollback')
    ),
    payload           jsonb NOT NULL CHECK (
        jsonb_typeof(payload) = 'object'
        AND payload ? 'schema_version'
        AND jsonb_typeof(payload->'schema_version') = 'string'
    ),
    payload_hash      bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    status            text NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'leased', 'projected', 'dead_letter')
    ),
    attempts          integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at      timestamptz NOT NULL DEFAULT now(),
    lease_owner       text,
    lease_expires_at  timestamptz,
    created_at        timestamptz NOT NULL,
    projected_at      timestamptz,
    dead_letter_at    timestamptz,
    last_error_class  text,
    UNIQUE (owner_id, aggregate_id, sequence),
    CHECK (
        (status = 'leased' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (status <> 'leased' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK ((status = 'projected') = (projected_at IS NOT NULL)),
    CHECK ((status = 'dead_letter') = (dead_letter_at IS NOT NULL))
);

CREATE INDEX adaptive_outbox_owner_idx
    ON aura.adaptive_outbox (owner_id, aggregate_id, sequence);

CREATE INDEX adaptive_outbox_claim_idx
    ON aura.adaptive_outbox (available_at, owner_id, aggregate_id, sequence)
    WHERE status IN ('pending', 'leased');

ALTER TABLE aura.adaptive_aggregate_sequences ENABLE ROW LEVEL SECURITY;
CREATE POLICY adaptive_sequences_owner_isolation ON aura.adaptive_aggregate_sequences
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

ALTER TABLE aura.adaptive_outbox ENABLE ROW LEVEL SECURITY;
CREATE POLICY adaptive_outbox_owner_isolation ON aura.adaptive_outbox
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.adaptive_aggregate_sequences TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.adaptive_outbox TO aura_app;

COMMENT ON TABLE aura.adaptive_outbox IS
    'Authoritative owner-scoped adaptive decision/outcome outbox. Neo4j is a replayable projection.';
COMMENT ON COLUMN aura.adaptive_outbox.payload_hash IS
    'SHA-256 of canonical payload JSON; the same event UUID with a different hash is a hard conflict.';
