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

-- name: ClaimPIMWatchLabelDescriptions :execrows
UPDATE aura.pim_watch_settings
SET label_descriptions_seeded = true
WHERE identity_id = $1 AND NOT label_descriptions_seeded;

-- name: FillPIMWatchLabelDescription :exec
-- Only a label with that exact name and no description of its own: what the person wrote stays.
UPDATE aura.pim_watch_labels
SET description = $3
WHERE identity_id = $1 AND name = $2 AND description = '';

-- name: SeedPIMWatchLabel :exec
INSERT INTO aura.pim_watch_labels (identity_id, name, tier, position, description, discard)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (identity_id, name) DO NOTHING;

-- name: ListPIMWatchLabels :many
SELECT * FROM aura.pim_watch_labels
WHERE identity_id = $1
ORDER BY position, name;

-- name: InsertPIMWatchLabel :one
INSERT INTO aura.pim_watch_labels (identity_id, name, tier, position, description, discard)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPIMWatchLabelForUpdate :one
SELECT * FROM aura.pim_watch_labels
WHERE identity_id = $1 AND id = $2
FOR UPDATE;

-- name: UpdatePIMWatchLabel :one
UPDATE aura.pim_watch_labels
SET name = $3, tier = $4, description = $5, discard = $6
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

-- name: PIMWatchRuleExists :one
SELECT EXISTS (
    SELECT 1 FROM aura.pim_watch_rules
    WHERE identity_id = $1 AND key_kind = $2 AND key = $3
);

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
-- The switch: a cursor from now, or a resource the provider lacks or the account refuses (its
-- last_error then says which). Nothing from a past watch of this account survives it.
INSERT INTO aura.pim_watch_cursors (identity_id, account_id, resource, cursor, status, last_error)
VALUES ($1, $2, $3, $4, $5, sqlc.narg(last_error))
ON CONFLICT (identity_id, account_id, resource) DO UPDATE
SET cursor = EXCLUDED.cursor, status = EXCLUDED.status, last_error = EXCLUDED.last_error, failing_since = NULL,
    alarm_kind = NULL, alarm_since = NULL, alarm_run_id = NULL, advanced_at = now(),
    alerts_day_start = NULL, alerts_told = 0;

-- name: CountPIMWatchAlertsOfDay :one
-- What the account pushed since this local midnight, mail and calendar together.
SELECT COALESCE(sum(alerts_told), 0)::integer FROM aura.pim_watch_cursors
WHERE identity_id = $1 AND account_id = $2 AND alerts_day_start = @day_start;

-- name: TallyPIMWatchAlerts :exec
-- A row still on an earlier day starts over at what this page told.
UPDATE aura.pim_watch_cursors
SET alerts_told = CASE WHEN alerts_day_start = @day_start THEN alerts_told + @told ELSE @told END,
    alerts_day_start = @day_start
WHERE identity_id = $1 AND account_id = $2 AND resource = $3;

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

-- name: ListPIMWatchPriorEvents :many
SELECT item_id, fingerprint, event_start, event_all_day, event_self, event_change FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = ANY(@item_ids::text[]);

-- name: InsertPIMWatchVerdict :execrows
-- A mail is decided once: a replayed change leaves the first verdict and is told to nobody. A label
-- deleted since the Book was read is written as NULL: the page commits, and is never replayed for it.
INSERT INTO aura.pim_watch_verdicts (
    identity_id, account_id, resource, item_id, calendar_id, tier, label_id, decided_by,
    auth_failed, fingerprint, decided_at, ladder_tier, ladder_by, centroid_tier, centroid_margin,
    sender_memory_tier, written_to, provider_important, bulk,
    model_label_id, model_probability, model_name, model_tier, model_discarded, model_options, model_skipped, discarded
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(account_id), sqlc.arg(resource), sqlc.arg(item_id), sqlc.arg(calendar_id),
    sqlc.arg(tier), (SELECT l.id FROM aura.pim_watch_labels l WHERE l.id = sqlc.narg(label_id)), sqlc.arg(decided_by),
    sqlc.arg(auth_failed), sqlc.arg(fingerprint), sqlc.arg(decided_at), sqlc.narg(ladder_tier), sqlc.narg(ladder_by),
    sqlc.narg(centroid_tier), sqlc.narg(centroid_margin), sqlc.narg(sender_memory_tier), sqlc.arg(written_to),
    sqlc.arg(provider_important), sqlc.arg(bulk),
    (SELECT l.id FROM aura.pim_watch_labels l WHERE l.id = sqlc.narg(model_label_id)),
    sqlc.narg(model_probability), sqlc.narg(model_name), sqlc.narg(model_tier), sqlc.arg(model_discarded),
    sqlc.narg(model_options), sqlc.arg(model_skipped), sqlc.arg(discarded)
)
ON CONFLICT (identity_id, account_id, resource, item_id) DO NOTHING;

-- name: UpsertPIMWatchEventVerdict :execrows
-- An event is decided again only when its fingerprint moved.
INSERT INTO aura.pim_watch_verdicts (
    identity_id, account_id, resource, item_id, calendar_id, tier, decided_by, fingerprint, decided_at,
    event_start, event_all_day, event_self, event_change
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (identity_id, account_id, resource, item_id) DO UPDATE
SET calendar_id = EXCLUDED.calendar_id, tier = EXCLUDED.tier, decided_by = EXCLUDED.decided_by,
    decided_at = EXCLUDED.decided_at, fingerprint = EXCLUDED.fingerprint,
    event_start = EXCLUDED.event_start, event_all_day = EXCLUDED.event_all_day,
    event_self = EXCLUDED.event_self,
    event_change = EXCLUDED.event_change,
    alert_run_id = NULL, alerted_at = NULL, notified = false
WHERE aura.pim_watch_verdicts.fingerprint <> EXCLUDED.fingerprint;

-- name: MarkPIMWatchAlert :exec
UPDATE aura.pim_watch_verdicts
SET alert_run_id = $5, alerted_at = $6, notified = false
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = $4;

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
  AND NOT discarded
ORDER BY decided_at, item_id;

-- name: CountPIMWatchDiscardedInWindow :one
-- The discarded mail of a digest's window, counted on the window the digest lists.
SELECT count(*) FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND decided_at > @since AND decided_at <= @until AND discarded;

-- name: ListPIMWatchUndeliveredRuns :many
-- The runs of a window whose alert has not reached the person. A run completes before it notifies,
-- so `notified` says the run ended, and only the queue knows the push did not arrive.
SELECT DISTINCT p.run_id FROM aura.pending_notifications p
JOIN aura.pim_watch_verdicts v ON v.alert_run_id = p.run_id
WHERE v.identity_id = $1 AND v.decided_at > @since AND v.decided_at <= @until AND p.status <> 'delivered';

-- name: ListPIMWatchAccountVerdicts :many
SELECT * FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND decided_at >= @since AND decided_at < @until
  AND discarded = @discarded
ORDER BY decided_at DESC, item_id
LIMIT @row_limit;

-- name: CountPIMWatchDiscardedOfDay :one
-- The discarded mail of a cockpit day, which the day's list leaves out and its toggle shows.
SELECT count(*) FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND account_id = $2 AND decided_at >= @since AND decided_at < @until AND discarded;

-- name: CorrectPIMWatchVerdict :one
UPDATE aura.pim_watch_verdicts
SET tier = $5, label_id = $6, decided_by = 'person', discarded = $7
WHERE identity_id = $1 AND account_id = $2 AND resource = $3 AND item_id = $4
RETURNING *;

-- name: DeletePIMWatchVerdicts :exec
DELETE FROM aura.pim_watch_verdicts WHERE identity_id = $1 AND account_id = $2;

-- name: DeleteExpiredPIMWatchVerdicts :execrows
-- One bounded batch of what has outlived the identity's retention, for the retention sweep. An
-- event still ahead keeps its row: when it moves, the row is what tells a move from a first sight.
DELETE FROM aura.pim_watch_verdicts AS v
USING (
    SELECT o.identity_id, o.account_id, o.resource, o.item_id
    FROM aura.pim_watch_verdicts AS o
    JOIN aura.pim_watch_settings AS s ON s.identity_id = o.identity_id
    WHERE o.identity_id = $1
      AND o.decided_at + s.verdict_retention_days * interval '1 day' < sqlc.arg(now)::timestamptz
      AND (o.event_start IS NULL OR o.event_start <= sqlc.arg(now)::timestamptz)
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

-- name: ListPIMWatchSenderMemory :many
-- The per-sender memory, §7 G2b's baseline: each sender's last corrected tier.
SELECT DISTINCT ON (sender_key) sender_key, tier FROM aura.pim_watch_corrections
WHERE identity_id = $1 AND sender_key = ANY(@sender_keys::text[]) AND tier IS NOT NULL
ORDER BY sender_key, created_at DESC;

-- name: ListPIMWatchCorrectedAccounts :many
SELECT DISTINCT account_id FROM aura.pim_watch_corrections WHERE identity_id = $1;

-- name: ListPIMWatchShadowRows :many
SELECT tier, decided_by, ladder_tier, ladder_by, centroid_tier, centroid_margin, sender_memory_tier, model_tier,
    decided_at, notified
FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND resource = 'mail' AND ladder_tier IS NOT NULL AND decided_at >= @since;

-- name: ListPIMWatchModelRows :many
SELECT tier, decided_by, ladder_tier, ladder_by, label_id, model_label_id, model_probability, model_name, model_tier,
    model_discarded, model_options, model_skipped, discarded, notified
FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND resource = 'mail' AND ladder_tier IS NOT NULL AND decided_at >= @since;

-- name: GetPIMWatchAccount :one
SELECT * FROM aura.pim_watch_accounts WHERE identity_id = $1 AND account_id = $2;

-- name: ListPIMWatchAccounts :many
SELECT * FROM aura.pim_watch_accounts WHERE identity_id = $1 ORDER BY account_id;

-- name: SetPIMWatchCloudDecide :exec
-- The person's consent for the hosted decision model, kept apart from the cursors so that turning the
-- watch off and on does not take it back.
INSERT INTO aura.pim_watch_accounts (identity_id, account_id, cloud_decide) VALUES ($1, $2, $3)
ON CONFLICT (identity_id, account_id) DO UPDATE SET cloud_decide = EXCLUDED.cloud_decide, updated_at = now();

-- name: RecordPIMWatchDecideFailure :exec
-- The decision model's last failure on an account, a class with no text. The time stays the first
-- failure's until an answer clears it, so the cockpit can say since when the model has not answered.
INSERT INTO aura.pim_watch_accounts (identity_id, account_id, decide_error, decide_failed_at) VALUES ($1, $2, $3, now())
ON CONFLICT (identity_id, account_id) DO UPDATE
SET decide_error = EXCLUDED.decide_error,
    decide_failed_at = COALESCE(aura.pim_watch_accounts.decide_failed_at, EXCLUDED.decide_failed_at),
    updated_at = now();

-- name: ClearPIMWatchDecideFailure :exec
-- An answer clears the last failure. It touches only a row that holds one, so a tick with nothing
-- to clear writes nothing.
UPDATE aura.pim_watch_accounts SET decide_error = '', decide_failed_at = NULL, updated_at = now()
WHERE identity_id = $1 AND account_id = $2 AND decide_error <> '';

-- name: DeletePIMWatchAccountSettings :exec
DELETE FROM aura.pim_watch_accounts WHERE identity_id = $1 AND account_id = $2;
