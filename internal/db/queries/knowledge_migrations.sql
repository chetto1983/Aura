-- name: RecordKnowledgeMigration :exec
INSERT INTO aura.knowledge_migrations (version, name, checksum)
VALUES ($1, $2, $3);

-- name: ListAppliedKnowledgeMigrations :many
SELECT version, name, checksum, applied_at
FROM aura.knowledge_migrations
ORDER BY version ASC;
