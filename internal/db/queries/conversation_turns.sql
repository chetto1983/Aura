-- name: InsertConversationTurn :execrows
-- parent_seq maintains the canonical leaf->root chain the branch walk recurses on
-- (ListManagedBranchPathPage joins `t.seq = p.parent_seq`). It is derived here rather
-- than bound as a parameter so no call site can forget it — which is exactly what
-- happened: 0017 added the column, backfilled `seq - 1` for the rows present at
-- migration time, and gave it no default, so EVERY turn appended since carried NULL. A
-- NULL breaks the join, so a forked branch reconstructed to the forked turn ALONE —
-- measured 2026-08-13 against the live schema, one turn, not even the system head — and
-- the model lost the whole prior conversation on any edit/regenerate.
-- NULLIF keeps seq=1 (the root) at NULL, matching that backfill's `WHERE seq > 1`.
INSERT INTO aura.conversation_turns (
    conversation_id, seq, role, content, content_sidecar_path,
    tool_call_id, tool_calls, input_tokens, output_tokens, cached_tokens,
    reasoning, reasoning_duration_ms, parent_seq, delivery_key
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
    NULLIF($2::int - 1, 0), sqlc.narg(delivery_key)
)
ON CONFLICT (conversation_id, delivery_key) WHERE delivery_key IS NOT NULL
DO NOTHING;

-- name: LockConversationForTurnAppend :one
SELECT id
FROM aura.conversations
WHERE id = $1
FOR UPDATE;

-- name: NextConversationTurnSeq :one
SELECT (COALESCE(MAX(seq), 0) + 1)::int AS seq
FROM aura.conversation_turns
WHERE conversation_id = $1;

-- name: ListTurnsBySeq :many
SELECT conversation_id, seq, role, content, content_sidecar_path,
       tool_call_id, tool_calls, created_at, input_tokens, output_tokens, cached_tokens
FROM aura.conversation_turns
WHERE conversation_id = $1
ORDER BY seq ASC;

-- name: ListManagedHistoryHead :many
-- The protected system root is fetched once, independently of body pagination. :many
-- deliberately represents an empty conversation without pgx.ErrNoRows.
SELECT conversation_id, seq, role, content, content_sidecar_path,
       tool_call_id, tool_calls, created_at, input_tokens, output_tokens, cached_tokens
FROM aura.conversation_turns
WHERE conversation_id = $1 AND role = 'system'
ORDER BY seq ASC
LIMIT 1;

-- name: ListManagedTurnsPageBySeq :many
-- AURA_HISTORY_HARD_CAP_TURNS is a keyset page size, never a recall boundary. The
-- caller keeps paging until the exact durable watermark or the root is observed.
SELECT conversation_id, seq, role, content, content_sidecar_path,
       tool_call_id, tool_calls, created_at, input_tokens, output_tokens, cached_tokens
FROM aura.conversation_turns
WHERE conversation_id = sqlc.arg(target_conversation_id)
  AND role <> 'system'
  AND seq <= sqlc.arg(cursor_seq)
ORDER BY seq DESC
LIMIT sqlc.arg(page_size);

-- name: CountTurns :one
SELECT count(*) AS turn_count
FROM aura.conversation_turns
WHERE conversation_id = $1;

-- name: ListAssistantTurnReasoning :many
-- Amendment #91 (fix-plan 1.12) display-only read: the reasoning columns for every
-- answer-shaped assistant turn (no tool_calls payload), ordered by seq. Deliberately
-- SEPARATE from ListTurnsBySeq so the llm.Message history rebuild can never select
-- reasoning. Scope mirrors ListTurnsBySeq exactly (conversation-wide, no branch
-- filter) so the snapshot merge pairs positionally with the LoadHistory projection.
-- The tool_calls filter mirrors turnToMessage's Go semantics: rows whose tool_calls
-- decode to zero calls ('[]'/'null') count as answer-shaped there too.
SELECT seq, reasoning, reasoning_duration_ms
FROM aura.conversation_turns
WHERE conversation_id = $1
  AND role = 'assistant'
  AND (tool_calls IS NULL OR tool_calls = '[]'::jsonb OR tool_calls = 'null'::jsonb)
ORDER BY seq ASC;

-- name: ListSpilledSeqsForConversation :many
-- D-09 (LOOP-09): every seq whose content spilled to a <seq>.content sidecar
-- (content_sidecar_path IS NOT NULL) in one conversation. The crash-orphan GC
-- (orphan_scan.go) reconciles the live .content files against this referenced
-- set, so no ORDER BY is needed. Read-only — no schema change (D-07 holds).
SELECT seq
FROM aura.conversation_turns
WHERE conversation_id = $1
  AND content_sidecar_path IS NOT NULL;

-- name: SearchConversationTurns :many
-- LOCKED cross-slice contract (D-A5-03 / SPEC Req#13). Telegram /search (Phase 13)
-- reuses this EXACT query; only the excerpt rendering differs per channel.
SELECT conversation_id, seq, content, similarity(content, $1) AS sim
FROM aura.conversation_turns
WHERE content % $1
ORDER BY similarity(content, $1) DESC
LIMIT $2;

-- name: CanonicalBranchLeafSeq :one
-- D-09 (CHAT-05): the leaf (deepest) seq of a conversation's canonical branch — the
-- all-zero sentinel branch every pre-0017 turn is backfilled onto. For a non-branched
-- conversation this is just MAX(seq), so a branch-path walk from this leaf reconstructs
-- the same linear history ListTurnsBySeq returns (byte-identity, store.go:250). Returns
-- 0 when the conversation has no turns.
SELECT COALESCE(MAX(seq), 0)::int AS leaf_seq
FROM aura.conversation_turns
WHERE conversation_id = $1
  AND branch_id = '00000000-0000-0000-0000-000000000000';

-- name: ListTurnsByBranchPath :many
-- D-09 (CHAT-05): the deterministic leaf->root path walk. Given a selected leaf seq,
-- follow parent_seq up to the root, then return the turns in root->leaf (seq ASC) order
-- so the head (system seq=1 + the always-block) is byte-identical to the linear case —
-- only the body turns differ per branch (Pitfall 3 rule 1). The column list MIRRORS
-- ListTurnsBySeq exactly so turnFromRow rehydrates it unchanged. A leaf seq of 0 (or one
-- not present) yields an empty path. Walk terminates at parent_seq IS NULL (the root).
WITH RECURSIVE path AS (
    SELECT ct.conversation_id, ct.seq, ct.role, ct.content, ct.content_sidecar_path,
           ct.tool_call_id, ct.tool_calls, ct.created_at, ct.input_tokens, ct.output_tokens,
           ct.cached_tokens, ct.branch_id, ct.parent_seq
    FROM aura.conversation_turns ct
    WHERE ct.conversation_id = $1 AND ct.seq = $2
    UNION ALL
    SELECT t.conversation_id, t.seq, t.role, t.content, t.content_sidecar_path,
           t.tool_call_id, t.tool_calls, t.created_at, t.input_tokens, t.output_tokens,
           t.cached_tokens, t.branch_id, t.parent_seq
    FROM aura.conversation_turns t
    JOIN path p
      ON t.conversation_id = p.conversation_id
     AND t.seq = p.parent_seq
)
SELECT path.conversation_id, path.seq, path.role, path.content, path.content_sidecar_path,
       path.tool_call_id, path.tool_calls, path.created_at, path.input_tokens,
       path.output_tokens, path.cached_tokens
FROM path
ORDER BY path.seq ASC;

-- name: ListManagedBranchPathPage :many
-- One bounded leaf->root page. Go resumes from the last row's parent_seq, checks strict
-- monotonicity/cycles, and stops only at the exact durable watermark or NULL root.
WITH RECURSIVE path AS (
    SELECT ct.conversation_id, ct.seq, ct.role, ct.content, ct.content_sidecar_path,
           ct.tool_call_id, ct.tool_calls, ct.created_at, ct.input_tokens, ct.output_tokens,
           ct.cached_tokens, ct.branch_id, ct.parent_seq, 1::int AS depth
    FROM aura.conversation_turns ct
    WHERE ct.conversation_id = sqlc.arg(target_conversation_id)
      AND ct.seq = sqlc.arg(cursor_seq)
    UNION ALL
    SELECT t.conversation_id, t.seq, t.role, t.content, t.content_sidecar_path,
           t.tool_call_id, t.tool_calls, t.created_at, t.input_tokens, t.output_tokens,
           t.cached_tokens, t.branch_id, t.parent_seq, p.depth + 1
    FROM aura.conversation_turns t
    JOIN path p
      ON t.conversation_id = p.conversation_id
     AND t.seq = p.parent_seq
    WHERE p.depth < GREATEST(sqlc.arg(page_size)::int, 1)
)
SELECT path.conversation_id, path.seq, path.role, path.content,
       path.content_sidecar_path, path.tool_call_id, path.tool_calls,
       path.created_at, path.input_tokens, path.output_tokens, path.cached_tokens,
       path.parent_seq, path.depth
FROM path
ORDER BY path.depth ASC;

-- name: SetTurnBranchPointers :exec
-- D-09 (CHAT-05): set a turn's branch/parent pointers. The branch-write seam plan 25-07
-- uses when an edit/regenerate forks a new sibling branch off an existing parent turn.
UPDATE aura.conversation_turns
SET branch_id = $3, parent_seq = $4
WHERE conversation_id = $1 AND seq = $2;

-- name: GetTurnPointers :one
-- D-09 (CHAT-05): a turn's own branch/parent pointers, used by the fork path to read the
-- diverging turn's parent_seq (the new sibling chains off the SAME parent so it replaces
-- the diverging turn rather than appending after it). Returns pgx.ErrNoRows when the seq
-- is absent (mapped to a clean 404 at the boundary).
SELECT seq, branch_id, parent_seq, role
FROM aura.conversation_turns
WHERE conversation_id = $1 AND seq = $2;

-- name: ListBranchLeaves :many
-- D-09 (CHAT-05): the navigable branch set. A leaf is a turn that is NOT the parent of
-- any other turn (no row's parent_seq points at it) — i.e. the tip of a branch path. The
-- BranchPicker navigates among these sibling leaves; a re-run continues over the selected
-- leaf's path (ListTurnsByBranchPath). Ordered by branch_id then seq so the canonical
-- (all-zero) branch sorts first and the order is stable across calls.
SELECT leaf.seq, leaf.branch_id, leaf.parent_seq
FROM aura.conversation_turns leaf
WHERE leaf.conversation_id = $1
  AND NOT EXISTS (
      SELECT 1
      FROM aura.conversation_turns child
      WHERE child.conversation_id = leaf.conversation_id
        AND child.parent_seq = leaf.seq
  )
ORDER BY leaf.branch_id ASC, leaf.seq ASC;

-- name: GetConversationCompaction :one
-- The durable summary of this branch's earlier turns (migration 0096, HANDOFF 6.1).
-- Returns pgx.ErrNoRows when the branch has never been compacted, which is the normal
-- state of every conversation short enough to fit its window.
SELECT conversation_id, branch_id, covers_through_seq, summary, model, source_turns,
       created_at, updated_at
FROM aura.conversation_compactions
WHERE conversation_id = $1 AND branch_id = $2;

-- name: UpsertConversationCompaction :one
-- Writes the branch's summary, advancing the watermark.
--
-- The WHERE on the DO UPDATE is the whole invariant: a watermark may only move FORWARD.
-- Two turns can compact concurrently (two requests on one conversation), and the loser
-- must not replace a summary covering seq 120 with one covering seq 80 -- that would
-- silently drop forty turns out of the model's view while leaving the ladder believing
-- they were summarized. Zero rows back means "someone else already went further", which
-- the caller treats as success and re-reads.
INSERT INTO aura.conversation_compactions (
    conversation_id, branch_id, covers_through_seq, summary, model, source_turns
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (conversation_id, branch_id) DO UPDATE
   SET covers_through_seq = EXCLUDED.covers_through_seq,
       summary            = EXCLUDED.summary,
       model              = EXCLUDED.model,
       source_turns       = EXCLUDED.source_turns,
       updated_at         = now()
   WHERE conversation_compactions.covers_through_seq < EXCLUDED.covers_through_seq
RETURNING conversation_id, branch_id, covers_through_seq, summary, model, source_turns,
          created_at, updated_at;
