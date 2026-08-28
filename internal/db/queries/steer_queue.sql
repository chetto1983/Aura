-- The D-06/D-07/D-08 durable steer/delegation-result queue. Push and Drain satisfy a
-- LOCKED interface contract (Push(conv, source, text string) error /
-- Drain(conv string) []Message) that carries neither an identity nor a context.Context,
-- so identity is derived from conv via aura.conversation_owner() (migration 0103) rather
-- than set as an RLS session variable -- see that migration's header for why. The sweep
-- (ListDueSteerRows / MarkSteerRowExpired) DOES have an identity in hand per row (the
-- row's own identity_id column), so its conversation-trace write runs inside
-- db.WithIdentityTx exactly like every other identity-scoped write in the tree.

-- name: PushSteerRow :execrows
-- Guarded insert: 0 rows affected means EITHER conv has no resolvable owner (an unknown
-- or not-yet-created conversation_id -- Go maps this to a wiring error, never silently)
-- OR the (identity, conv) queue is already at cap (Go maps this to steer.ErrQueueFull,
-- preserving the pre-Postgres Inbox's exact per-conversation Max semantic, D-11's
-- catalogue-vs-behavior drift guard). The two are disambiguated in Go by a second,
-- cheap owner-only probe (conversationOwner) only when execrows is 0 -- never a second
-- round trip on the (overwhelmingly common) success path.
WITH owner AS (
    SELECT aura.conversation_owner(sqlc.arg(conversation_id)) AS identity_id
), capacity AS (
    SELECT count(*) AS n
    FROM aura.steer_queue q, owner
    WHERE q.conversation_id = sqlc.arg(conversation_id)
      AND q.identity_id = owner.identity_id
      AND q.drained_at IS NULL
      AND q.expired_at IS NULL
      AND (q.expires_at IS NULL OR q.expires_at > now())
)
INSERT INTO aura.steer_queue (identity_id, conversation_id, kind, source, body, expires_at)
SELECT owner.identity_id, sqlc.arg(conversation_id), sqlc.arg(kind), sqlc.arg(source),
       sqlc.arg(body), sqlc.narg(expires_at)
FROM owner, capacity
WHERE owner.identity_id IS NOT NULL
  AND capacity.n < sqlc.arg(max_queue)::int;

-- name: ConversationOwner :one
-- The disambiguation probe PushSteerRow's Go caller runs only when execrows was 0:
-- NULL means "conv has no resolvable owner" (ErrConversationUnknown-shaped wiring
-- error); a non-NULL result together with a 0-row insert means the queue was full
-- (steer.ErrQueueFull).
SELECT aura.conversation_owner(sqlc.arg(conversation_id))::uuid AS identity_id;

-- name: DrainSteerRows :many
-- The drain IS the claim (the same conditional-update-as-idempotency-key idiom as
-- MarkPausedStateResumed, not a second concurrency story): FOR UPDATE serializes two
-- concurrent Drain calls for the SAME conversation_id rather than racing them, so the
-- second sees zero remaining undrained rows once the first commits -- disjoint sets by
-- construction, matching the in-memory Inbox's mutex-guarded exact-once semantic.
-- identity_id = owner.identity_id is defense in depth (T-51-07): every row for a given
-- conv is written with that conv's TRUE owner by PushSteerRow, so this can only ever
-- exclude a row in the event of data that did not come through Push.
WITH owner AS (
    SELECT aura.conversation_owner(sqlc.arg(conversation_id)) AS identity_id
), candidates AS (
    SELECT q.id
    FROM aura.steer_queue q, owner
    WHERE q.conversation_id = sqlc.arg(conversation_id)
      AND q.identity_id = owner.identity_id
      AND q.drained_at IS NULL
      AND q.expired_at IS NULL
      AND (q.expires_at IS NULL OR q.expires_at > now())
    ORDER BY q.created_at, q.id
    FOR UPDATE
), drained AS (
    UPDATE aura.steer_queue q
    SET drained_at = now()
    FROM candidates c
    WHERE q.id = c.id
    RETURNING q.*
)
SELECT * FROM drained ORDER BY created_at, id;

-- name: ListDueSteerRows :many
-- Unscoped by identity (aura.steer_queue carries no RLS, migration 0103): the sweep is a
-- system-wide background job, not a per-identity request, exactly like
-- ListExpiredPendingApprovals's own cross-tenant caller shape but WITHOUT that method's
-- per-identity enumeration loop -- each returned row already carries its own
-- identity_id, so the Go sweep opens ONE identity-scoped transaction per row rather than
-- one unscoped pass per known identity.
SELECT * FROM aura.steer_queue
WHERE drained_at IS NULL
  AND expired_at IS NULL
  AND expires_at IS NOT NULL
  AND expires_at <= sqlc.arg(cutoff)::timestamptz
ORDER BY expires_at, id
LIMIT sqlc.arg(row_limit);

-- name: MarkSteerRowExpired :execrows
-- WHERE ... AND expired_at IS NULL is the idempotency gate: a second sweep pass over an
-- already-expired row affects 0 rows rather than re-writing a duplicate conversation
-- trace (D-07's "the sweep is idempotent").
UPDATE aura.steer_queue
SET expired_at = now(), expiry_reason = sqlc.arg(expiry_reason)
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND expired_at IS NULL;

-- name: ListUnnudgedDelegationResults :many
-- Plan 51-10's absent-operator leg: delegation_result rows the operator never drained
-- (drained_at IS NULL), not expired, not already nudged, past the nudge grace window.
-- A drained row is EXCLUDED by construction (the operator already received it inside a
-- turn -- D-04 -- so nudging it would tell them twice). Unscoped by identity like
-- ListDueSteerRows (aura.steer_queue carries no RLS, migration 0103): a system-wide
-- sweep, not a per-identity request -- each returned row carries its own identity_id.
SELECT * FROM aura.steer_queue
WHERE kind = 'delegation_result'
  AND drained_at IS NULL
  AND expired_at IS NULL
  AND nudged_at IS NULL
  AND created_at < sqlc.arg(cutoff)::timestamptz
ORDER BY created_at, id
LIMIT sqlc.arg(row_limit);

-- name: MarkSteerRowNudged :execrows
-- The conditional UPDATE ... WHERE nudged_at IS NULL IS the idempotency key (SWARM-09
-- edge): two concurrent sweep passes over the SAME row nudge exactly once -- the
-- winner sees RowsAffected==1, the loser sees 0 and skips its own push-outcome
-- bookkeeping for that row.
UPDATE aura.steer_queue
SET nudged_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND nudged_at IS NULL;
