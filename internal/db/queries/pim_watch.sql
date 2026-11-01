-- name: EnsurePIMWatchSettings :exec
-- Every statement in this file names the identity as well as running under it: RLS is the
-- backstop, not the filter.
INSERT INTO aura.pim_watch_settings (identity_id) VALUES ($1)
ON CONFLICT (identity_id) DO NOTHING;

-- name: GetPIMWatchSettings :one
SELECT * FROM aura.pim_watch_settings WHERE identity_id = $1;

-- name: UpdatePIMWatchSettings :one
UPDATE aura.pim_watch_settings
SET digest_hour = $2, daily_alert_cap = $3, updated_at = now()
WHERE identity_id = $1
RETURNING *;

-- name: SetPIMWatchLastDigest :exec
UPDATE aura.pim_watch_settings
SET last_digest_at = $2, updated_at = now()
WHERE identity_id = $1;

-- name: ClaimPIMWatchLabelSeed :execrows
UPDATE aura.pim_watch_settings
SET labels_seeded = true
WHERE identity_id = $1 AND NOT labels_seeded;

-- name: SeedPIMWatchLabel :exec
INSERT INTO aura.pim_watch_labels (identity_id, name, tier, position)
VALUES ($1, $2, $3, $4)
ON CONFLICT (identity_id, name) DO NOTHING;

-- name: ListPIMWatchLabels :many
SELECT * FROM aura.pim_watch_labels
WHERE identity_id = $1
ORDER BY position, name;

-- name: InsertPIMWatchLabel :one
INSERT INTO aura.pim_watch_labels (identity_id, name, tier, position)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdatePIMWatchLabel :one
UPDATE aura.pim_watch_labels
SET name = $3, tier = $4
WHERE identity_id = $1 AND id = $2
RETURNING *;

-- name: DeletePIMWatchLabel :execrows
DELETE FROM aura.pim_watch_labels WHERE identity_id = $1 AND id = $2;

-- name: DeletePIMWatchRulesOfLabel :exec
-- A rule that only gave this label says nothing once the label is gone.
DELETE FROM aura.pim_watch_rules
WHERE identity_id = $1 AND label_id = $2 AND tier IS NULL;

-- name: ListPIMWatchRules :many
SELECT * FROM aura.pim_watch_rules
WHERE identity_id = $1
ORDER BY created_at, id;

-- name: UpsertPIMWatchRule :one
INSERT INTO aura.pim_watch_rules (identity_id, key, key_kind, tier, label_id, source)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (identity_id, key_kind, key) DO UPDATE
SET tier = EXCLUDED.tier, label_id = EXCLUDED.label_id, source = EXCLUDED.source
RETURNING *;

-- name: MergePIMWatchRule :one
-- What a repeated correction learned. A tier and a label are learned apart, so each keeps the
-- rule's current value when this correction says nothing about it.
INSERT INTO aura.pim_watch_rules (identity_id, key, key_kind, tier, label_id, source)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (identity_id, key_kind, key) DO UPDATE
SET tier = COALESCE(EXCLUDED.tier, aura.pim_watch_rules.tier),
    label_id = COALESCE(EXCLUDED.label_id, aura.pim_watch_rules.label_id),
    source = EXCLUDED.source
RETURNING *;

-- name: DeletePIMWatchRule :execrows
DELETE FROM aura.pim_watch_rules WHERE identity_id = $1 AND id = $2;

-- name: GetPIMWatchCursor :one
SELECT * FROM aura.pim_watch_cursors
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: ListPIMWatchCursors :many
SELECT * FROM aura.pim_watch_cursors
WHERE identity_id = $1
ORDER BY account_id, resource;

-- name: StartPIMWatchCursor :exec
-- The switch: a cursor from now, or a resource the provider lacks. Nothing from a past watch of
-- this account survives it.
INSERT INTO aura.pim_watch_cursors (identity_id, account_id, resource, cursor, status)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (identity_id, account_id, resource) DO UPDATE
SET cursor = EXCLUDED.cursor, status = EXCLUDED.status, last_error = NULL, failing_since = NULL,
    alarm_kind = NULL, alarm_since = NULL, alarm_run_id = NULL, advanced_at = now();

-- name: AdvancePIMWatchCursor :exec
-- A success ends a failing episode and its alarm. A gap's alarm stays until it is confirmed told.
UPDATE aura.pim_watch_cursors
SET cursor = $4, status = 'ok', last_error = NULL, failing_since = NULL, advanced_at = now(),
    alarm_since = CASE WHEN alarm_kind = 'failing' THEN NULL ELSE alarm_since END,
    alarm_run_id = CASE WHEN alarm_kind = 'failing' THEN NULL ELSE alarm_run_id END,
    alarm_kind = CASE WHEN alarm_kind = 'failing' THEN NULL ELSE alarm_kind END
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: ResetPIMWatchCursor :exec
-- resetRequired: the gap runs from the last success, which this statement reads before moving it.
UPDATE aura.pim_watch_cursors
SET cursor = $4, status = 'ok', last_error = NULL, failing_since = NULL, advanced_at = now(),
    alarm_kind = 'gap', alarm_since = advanced_at, alarm_run_id = $5
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: MarkPIMWatchFailing :one
UPDATE aura.pim_watch_cursors
SET status = 'failing', last_error = $4, failing_since = COALESCE(failing_since, @at)
WHERE identity_id = $1 AND account_id = $2 AND resource = $3
RETURNING *;

-- name: MarkPIMWatchUnsupported :exec
UPDATE aura.pim_watch_cursors
SET status = 'unsupported', last_error = NULL, failing_since = NULL
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: RaisePIMWatchFailingAlarm :exec
UPDATE aura.pim_watch_cursors
SET alarm_kind = 'failing', alarm_since = failing_since, alarm_run_id = $4
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: RetellPIMWatchAlarm :exec
UPDATE aura.pim_watch_cursors
SET alarm_run_id = $4
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: ConfirmPIMWatchAlarm :exec
-- A failing alarm keeps its kind until a success, which is what makes it once per episode.
UPDATE aura.pim_watch_cursors
SET alarm_since = CASE WHEN alarm_kind = 'gap' THEN NULL ELSE alarm_since END,
    alarm_kind = CASE WHEN alarm_kind = 'gap' THEN NULL ELSE alarm_kind END,
    alarm_run_id = NULL
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

-- name: DeletePIMWatchCursors :exec
DELETE FROM aura.pim_watch_cursors WHERE identity_id = $1 AND account_id = $2;

-- name: GetPIMWatchVerdict :one
SELECT * FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = $4;

-- name: ListPIMWatchFingerprints :many
SELECT item_id, fingerprint FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = ANY(@item_ids::text[]);

-- name: InsertPIMWatchVerdict :execrows
-- A mail is decided once: a replayed change leaves the first verdict and is told to nobody.
INSERT INTO aura.pim_watch_verdicts (
    identity_id, account_id, resource, item_id, calendar_id, tier, label_id, decided_by,
    auth_failed, fingerprint, decided_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (identity_id, account_id, resource, item_id) DO NOTHING;

-- name: UpsertPIMWatchEventVerdict :execrows
-- An event is decided again only when its fingerprint moved.
INSERT INTO aura.pim_watch_verdicts (
    identity_id, account_id, resource, item_id, calendar_id, tier, decided_by, fingerprint, decided_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (identity_id, account_id, resource, item_id) DO UPDATE
SET calendar_id = EXCLUDED.calendar_id, tier = EXCLUDED.tier, decided_by = EXCLUDED.decided_by,
    decided_at = EXCLUDED.decided_at, fingerprint = EXCLUDED.fingerprint,
    alert_run_id = NULL, alerted_at = NULL, notified = false
WHERE aura.pim_watch_verdicts.fingerprint <> EXCLUDED.fingerprint;

-- name: MarkPIMWatchAlert :exec
UPDATE aura.pim_watch_verdicts
SET alert_run_id = $5, alerted_at = $6, notified = false
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = $4;

-- name: CountPIMWatchAlertsSince :one
SELECT count(*) FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND alerted_at >= $3;

-- name: ListPIMWatchOwedAlerts :many
SELECT * FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND alert_run_id IS NOT NULL AND NOT notified
ORDER BY alerted_at, item_id;

-- name: ConfirmPIMWatchAlerts :exec
UPDATE aura.pim_watch_verdicts
SET notified = true
WHERE identity_id = $1 AND account_id = $2 AND alert_run_id = $3 AND NOT notified;

-- name: RetellPIMWatchAlerts :exec
UPDATE aura.pim_watch_verdicts
SET alert_run_id = @new_run
WHERE identity_id = $1 AND account_id = $2 AND alert_run_id = @old_run AND NOT notified;

-- name: ListPIMWatchDigestVerdicts :many
SELECT * FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND decided_at > @since AND decided_at <= @until AND tier IN ('now', 'today')
ORDER BY decided_at, item_id;

-- name: ListPIMWatchAccountVerdicts :many
SELECT * FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND decided_at >= @since AND decided_at < @until
ORDER BY decided_at DESC, item_id
LIMIT @row_limit;

-- name: CorrectPIMWatchVerdict :one
UPDATE aura.pim_watch_verdicts
SET tier = $5, label_id = $6, decided_by = 'person'
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = $4
RETURNING *;

-- name: DeletePIMWatchVerdicts :exec
DELETE FROM aura.pim_watch_verdicts WHERE identity_id = $1 AND account_id = $2;

-- name: DeleteExpiredPIMWatchVerdicts :execrows
-- One bounded batch of what has outlived the identity's retention, for the retention sweep.
DELETE FROM aura.pim_watch_verdicts AS v
USING (
    SELECT o.identity_id, o.account_id, o.resource, o.item_id
    FROM aura.pim_watch_verdicts AS o
    JOIN aura.pim_watch_settings AS s ON s.identity_id = o.identity_id
    WHERE o.identity_id = $1
      AND o.decided_at + s.verdict_retention_days * interval '1 day' < sqlc.arg(now)::timestamptz
    LIMIT @batch
) AS expired
WHERE v.identity_id = expired.identity_id AND v.account_id = expired.account_id
  AND v.resource = expired.resource AND v.item_id = expired.item_id;

-- name: InsertPIMWatchCorrection :exec
INSERT INTO aura.pim_watch_corrections (identity_id, account_id, sender_key, tier, label_id)
VALUES ($1, $2, $3, $4, $5);

-- name: ListPIMWatchCorrectionsBefore :many
SELECT * FROM aura.pim_watch_corrections
WHERE identity_id = $1 AND sender_key = $2 AND created_at < @before
ORDER BY created_at DESC;

-- name: DeletePIMWatchCorrections :exec
DELETE FROM aura.pim_watch_corrections WHERE identity_id = $1 AND account_id = $2;
