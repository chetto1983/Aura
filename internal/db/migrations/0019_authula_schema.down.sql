-- Reverse of 0019_authula_schema.up.sql. Drops the identity link table and the isolated
-- authula schema (CASCADE so any Authula tables created inside it by the embedded
-- framework's bun migrator are dropped too). aura.* is untouched apart from removing the
-- additive link table.

DROP TABLE IF EXISTS aura.identity_auth_links;

-- CASCADE removes every Authula-owned table created in this schema (its bun migrator
-- ledger lives here too). The migration is reversible to a clean pre-Authula state.
DROP SCHEMA IF EXISTS authula CASCADE;

-- NOTE: the pgcrypto extension created by the up migration is deliberately NOT dropped
-- here — it is database-global and unrelated objects may depend on gen_random_uuid().
-- Dropping it on a routine rollback would risk breaking them; leaving it is harmless.

