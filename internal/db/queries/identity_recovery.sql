-- name: UpsertIdentityRecovery :exec
INSERT INTO aura.identity_recovery (identity_id, question, answer_hash, answer_hash_version)
VALUES ($1, $2, $3, $4)
ON CONFLICT (identity_id) DO UPDATE
SET question = EXCLUDED.question,
    answer_hash = EXCLUDED.answer_hash,
    answer_hash_version = EXCLUDED.answer_hash_version,
    updated_at = now();

-- name: GetIdentityRecoveryByIdentity :one
SELECT identity_id, question, answer_hash, answer_hash_version, created_at, updated_at
FROM aura.identity_recovery
WHERE identity_id = $1;

-- name: LookupRecoveryByEmail :one
SELECT i.id AS identity_id,
       i.name AS email,
       ial.authula_user_id,
       ir.question,
       ir.answer_hash,
       ir.answer_hash_version,
       ta.telegram_user_id
FROM aura.identities i
JOIN aura.identity_auth_links ial ON ial.identity_id = i.id
JOIN aura.identity_recovery ir ON ir.identity_id = i.id
JOIN aura.telegram_accounts ta ON ta.identity_id = i.id
WHERE lower(i.name) = lower($1)
ORDER BY COALESCE(ta.last_seen_at, ta.added_at) DESC,
         ta.added_at DESC,
         ta.telegram_user_id DESC,
         ial.created_at DESC,
         ial.authula_user_id DESC
LIMIT 1;

-- name: InsertPasswordResetChallenge :one
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, telegram_user_id, expires_at, max_attempts,
    request_ip_hash, user_agent_hash
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, identity_id, code_hash, telegram_user_id, created_at, expires_at,
    consumed_at, attempt_count, max_attempts, request_ip_hash, user_agent_hash;

-- name: GetActivePasswordResetChallenge :one
SELECT id, identity_id, code_hash, telegram_user_id, created_at, expires_at,
    consumed_at, attempt_count, max_attempts, request_ip_hash, user_agent_hash
FROM aura.password_reset_challenges
WHERE identity_id = $1
  AND consumed_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementPasswordResetChallengeAttempts :exec
UPDATE aura.password_reset_challenges
SET attempt_count = attempt_count + 1
WHERE id = $1;

-- name: ConsumePasswordResetChallenge :one
UPDATE aura.password_reset_challenges
SET consumed_at = now()
WHERE id = $1
  AND consumed_at IS NULL
  AND expires_at > now()
  AND attempt_count < max_attempts
RETURNING id, identity_id, code_hash, telegram_user_id, created_at, expires_at,
    consumed_at, attempt_count, max_attempts, request_ip_hash, user_agent_hash;

-- name: InsertPasswordResetToken :one
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, max_attempts
)
VALUES ($1, $2, $3, $4, $5)
RETURNING token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts;

-- name: GetPasswordResetToken :one
SELECT token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts
FROM aura.password_reset_tokens
WHERE token_hash = $1;

-- name: IncrementPasswordResetTokenAttempts :exec
UPDATE aura.password_reset_tokens
SET attempt_count = attempt_count + 1
WHERE token_hash = $1;

-- name: ConsumePasswordResetToken :one
UPDATE aura.password_reset_tokens
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
  AND attempt_count < max_attempts
RETURNING token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts;

-- name: InsertIdentityRecoveryAudit :one
INSERT INTO aura.identity_recovery_audit (
    identity_id, event, request_ip_hash, user_agent_hash, metadata
)
VALUES ($1, $2, $3, $4, COALESCE(sqlc.narg('metadata')::jsonb, '{}'::jsonb))
RETURNING id, identity_id, event, created_at, request_ip_hash, user_agent_hash, metadata;
