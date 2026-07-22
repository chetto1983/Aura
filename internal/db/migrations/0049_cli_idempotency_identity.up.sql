-- Durable non-human owner for CLI idempotency records. The legacy `local`
-- operator identity is intentionally retired after first Authula login, so it
-- cannot be the FK owner for deployment-time commands such as `neo4j migrate`.
-- This service principal has no capability grants and is never web-loginable.
INSERT INTO aura.identities (id, name, kind)
VALUES ('00000000-0000-0000-0000-000000000039', 'aura-cli', 'service');
