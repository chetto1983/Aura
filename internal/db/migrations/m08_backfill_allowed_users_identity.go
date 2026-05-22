package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func backfillAllowedUsersIdentity(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
WITH allowlisted AS (
  SELECT
    user_id,
    source,
    created_at,
    CASE
      WHEN source IN ('telegram_bootstrap', 'manual') THEN 'owner'
      ELSE 'human'
    END AS principal_kind
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
)
INSERT OR IGNORE INTO principals (
  id, kind, display_name, status, created_at, metadata_json
)
SELECT
  'principal:telegram:' || user_id,
  principal_kind,
  'Telegram ' || user_id,
  'active',
  created_at,
  '{}'
FROM allowlisted;

WITH allowlisted AS (
  SELECT user_id, created_at
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
)
INSERT OR IGNORE INTO channel_accounts (
  id, principal_id, provider, external_id, display_name, created_at,
  metadata_json
)
SELECT
  'acct:telegram:' || user_id,
  'principal:telegram:' || user_id,
  'telegram',
  user_id,
  'Telegram ' || user_id,
  created_at,
  '{}'
FROM allowlisted;

WITH allowlisted AS (
  SELECT user_id, created_at
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
)
INSERT OR IGNORE INTO actors (
  id, principal_id, actor_type, channel_account_id, run_id, created_at,
  metadata_json
)
SELECT
  'actor:telegram:session:' || user_id,
  'principal:telegram:' || user_id,
  'session',
  'acct:telegram:' || user_id,
  '',
  created_at,
  '{}'
FROM allowlisted;

WITH allowlisted AS (
  SELECT
    user_id,
    source,
    created_at,
    CASE
      WHEN source IN ('telegram_bootstrap', 'manual') THEN 1
      ELSE 0
    END AS is_owner
  FROM allowed_users
  WHERE source <> 'e2e_bootstrap'
),
caps(capability, owner_only) AS (
  VALUES
    ('api.chat', 0),
    ('dashboard.read', 0),
    ('dashboard.write', 0),
    ('tool.execute', 0),
    ('memory.user.write', 0),
    ('wiki.write', 0),
    ('skills.install', 1),
    ('skills.delete', 1),
    ('settings.write', 1),
    ('cron.create', 1),
    ('cron.run', 1),
    ('swarm.spawn', 1)
)
INSERT OR IGNORE INTO capability_grants (
  id, subject_type, subject_id, capability, resource_type, resource_id,
  constraints_json, granted_by_actor_id, created_at
)
SELECT
  'grant:telegram:' || allowlisted.user_id || ':' || caps.capability,
  'principal',
  'principal:telegram:' || allowlisted.user_id,
  caps.capability,
  '',
  '',
  '{}',
  NULL,
  allowlisted.created_at
FROM allowlisted
JOIN caps
WHERE caps.owner_only = 0 OR allowlisted.is_owner = 1;
`)
	if err != nil {
		return fmt.Errorf("migrations: backfill allowed users identity: %w", err)
	}
	return nil
}
