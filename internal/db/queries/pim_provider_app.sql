-- name: ListPIMProviderApps :many
SELECT provider, client_id, tenant_id,
       (client_secret_ciphertext IS NOT NULL)::boolean AS secret_set,
       updated_at, updated_by
FROM aura.pim_provider_app
ORDER BY provider;

-- name: GetPIMProviderApp :one
SELECT * FROM aura.pim_provider_app WHERE provider = $1;

-- name: UpsertPIMProviderApp :exec
INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id, client_secret_ciphertext, updated_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider) DO UPDATE
SET client_id = EXCLUDED.client_id,
    tenant_id = EXCLUDED.tenant_id,
    client_secret_ciphertext = EXCLUDED.client_secret_ciphertext,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by;

-- Keeping the stored secret is its own UPDATE: an INSERT … ON CONFLICT checks the row CHECK
-- against the proposed ('google', NULL) row before conflict handling (measured 2026-09-23).
-- It matches only the client the secret belongs to, so a save that raced another admin's new
-- client updates nothing instead of pairing the old ID with the new secret.
-- name: UpdatePIMProviderAppKeepSecret :execrows
UPDATE aura.pim_provider_app
SET updated_at = now(), updated_by = $3
WHERE provider = $1 AND client_id = $2 AND client_secret_ciphertext IS NOT NULL;
