-- Source: PRD Phase 36 (D-08 per-identity Garage key isolation, OQ4) + 36-02-PLAN
-- Task 1 + 36-RESEARCH Pattern 3. The migration floor before this is 0029
-- (identity_soft_delete); the per-identity object-store key store lands at 0030.
--
-- aura.identity_object_store holds one Garage/S3 credential per identity so each
-- identity's asset bucket is accessed with its OWN access/secret key pair (storage-
-- enforced isolation, not app-enforced). The identity_id PK + ON DELETE CASCADE ties a
-- key row's lifetime to its identity: deleting the identity drops its key.
--
-- secret_key_enc is the S3 secret stored ENCRYPTED-AT-REST as bytea ciphertext, never
-- plaintext (T-36-02-I). The wrapping key is derived from an EXISTING app secret at the
-- same trust boundary as .env: AURA_AUTHULA_SECRET (the 32-byte hex secret the daemon
-- already uses for HMAC/token key derivation). This migration only fixes the at-rest
-- SHAPE (bytea ciphertext); the actual AEAD encrypt/decrypt is implemented by the
-- plan-06 persistence layer, which owns the key-derivation + cipher choice. bucket and
-- access_key are non-secret and stored as plain text.

CREATE TABLE aura.identity_object_store (
    identity_id    uuid        PRIMARY KEY REFERENCES aura.identities (id) ON DELETE CASCADE,
    bucket         text        NOT NULL,
    access_key     text        NOT NULL,
    secret_key_enc bytea       NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- aura_app reads its key on asset access, inserts on provision, and deletes on
-- de-provision — SELECT, INSERT, DELETE (no UPDATE: a rotated key is delete-then-insert).
GRANT SELECT, INSERT, DELETE ON aura.identity_object_store TO aura_app;
GRANT ALL                    ON aura.identity_object_store TO aura_migrate;

COMMENT ON TABLE aura.identity_object_store IS
    'Per-identity Garage/S3 credential store (Phase 36, D-08/OQ4). secret_key_enc is the S3 secret as encrypted-at-rest bytea ciphertext (wrapping key derived from AURA_AUTHULA_SECRET, same trust boundary as .env); the AEAD is implemented by the plan-06 persistence layer. Deleting the identity cascades its key.';
