-- name: ListLLMProviderRoutes :many
SELECT provider, base_url, model, updated_at, updated_by
FROM aura.llm_provider_routes
ORDER BY provider;

-- name: UpsertLLMProviderRoute :one
INSERT INTO aura.llm_provider_routes (provider, base_url, model, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider) DO UPDATE
SET base_url   = EXCLUDED.base_url,
    model      = EXCLUDED.model,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING provider, base_url, model, updated_at, updated_by;
