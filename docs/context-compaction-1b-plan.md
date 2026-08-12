# Context Compaction — Slice 1b: durable persistence (execution plan)

**Status:** ready-to-execute. NOT started in code. Blocked on a live Postgres +
`sqlc` + `migrate` (this plan was authored in an environment that had none, so no
untested migration/write-path was committed — per CLAUDE.md "verify locally FIRST
with the stack up").

**Prereq:** Slice 1a (`internal/conversations/compaction.go`, commit landing L2.4
in-memory compaction) is merged. 1b makes the summary **durable** so it is computed
**once** and stored, instead of recomputed on every over-budget turn.

---

## 1. Goal & why

1a summarizes over-budget history into an **ephemeral** synthetic turn, rebuilt every
turn the conversation is over budget → one extra LLM call per over-budget turn. 1b
persists the summary so:

- the summary is computed once per compaction and reused across turns (cost + latency),
- superseded rounds are excluded from the model view by a durable **watermark**,
- the summary survives restart (it is real conversation state, not a per-load artifact).

This mirrors Google ADK's `EventsCompactionConfig` "summary stored as a durable event",
adapted to Aura's turn store.

## 2. Design decision — summary as conversation-level state (NOT a turn row)

The naive approach (insert the summary as a `conversation_turns` row) hits a **seq
ordering problem**: the summary represents OLD context but a freshly-appended turn gets
the HIGHEST seq, so it would sort AFTER the recent rounds it precedes. Reusing/fractional
seqs is ugly and collides with the 0017 branch tree (`parent_seq`).

**Chosen:** store the current summary + watermark as columns on `aura.conversations`
(one current summary per conversation, overwritten on each compaction), and inject it at
load time as a synthetic turn right after the protected head — exactly the mechanism the
always-block (`injectAlwaysBlock`) and transient memory context already use. Benefits:

- no new turn rows, no seq-ordering problem, no branch-tree interaction,
- the watermark (`compacted_through_seq`) cleanly excludes superseded rounds in the
  bounded load query,
- superseded rounds stay in `conversation_turns` for audit/recovery — only hidden from
  the model view.

Trade-off vs ADK: we keep only the CURRENT merged summary, not an append-only chain of
summaries. Acceptable — the raw rounds remain in the table; a per-compaction audit row
(§7) preserves the forensics.

## 3. Migration `0094` (confirm the slot with `ls internal/db/migrations/ | tail -1` at landing)

`0094_conversation_compaction.up.sql`:
```sql
-- Durable context compaction (Slice 1b). The current merged summary of the rounds
-- BELOW the watermark, injected at load as a synthetic turn after the system head.
-- compacted_through_seq: all non-system turns with seq <= this are superseded by the
-- summary and excluded from the model view (they remain in the table for audit).
ALTER TABLE aura.conversations
    ADD COLUMN compaction_summary   text,
    ADD COLUMN compacted_through_seq integer NOT NULL DEFAULT 0;

-- Audit: one row per compaction, sibling to context_rot_events (L2.5). Reuses the same
-- shape so the cockpit context gauge can mark compactions alongside hard-drops.
ALTER TABLE aura.context_rot_events
    ALTER COLUMN pairs_dropped DROP NOT NULL;  -- a compaction row carries rounds_summarized instead
-- (Simpler alternative if the CHECK/NOT NULL churn is unwanted: add a dedicated
--  aura.context_compaction_events table mirroring context_rot_events. Pick ONE at
--  landing; the audit is not on the hot path.)
```

`0094_conversation_compaction.down.sql`:
```sql
ALTER TABLE aura.conversations
    DROP COLUMN IF EXISTS compaction_summary,
    DROP COLUMN IF EXISTS compacted_through_seq;
ALTER TABLE aura.context_rot_events
    ALTER COLUMN pairs_dropped SET NOT NULL;
```

Grants: `aura.conversations` already grants `UPDATE` to `aura_app` (0005:60) — the new
columns inherit it. No new grant needed.

## 4. sqlc query changes (`internal/db/queries/`)

**conversations.sql — add:**
```sql
-- name: GetConversationCompaction :one
SELECT compaction_summary, compacted_through_seq
FROM aura.conversations
WHERE id = $1;

-- name: SetConversationCompaction :exec
UPDATE aura.conversations
SET compaction_summary = $2, compacted_through_seq = $3
WHERE id = $1;
```

**conversation_turns.sql — modify `ListRecentTurnsBySeq` and `ListRecentTurnsByBranchPath`**
to take a `watermark` arg and exclude superseded non-system rounds. Only the `recent`
CTE predicate changes (head is untouched, so messages[0] stays byte-identical):
```sql
-- in the recent CTE of ListRecentTurnsBySeq (and the recent CTE of the branch variant):
    WHERE ct.conversation_id = sqlc.arg(target_conversation_id)
      AND ct.role <> 'system'
      AND ct.seq > sqlc.arg(watermark)::int      -- NEW: drop rounds the summary supersedes
    ORDER BY ct.seq DESC
    LIMIT GREATEST(sqlc.arg(hard_cap)::int - (SELECT count(*)::int FROM head), 0)
```
A `watermark` of 0 (the default column value, and the value passed when persistence is
disabled) is a no-op — `seq > 0` keeps every non-system turn, so the query is behaviour-
identical to today when compaction persistence is off.

Then `sqlc generate` (regenerates `internal/db/sqlc/*`; **offline, but sqlc must be
installed** — `make tools`).

## 5. Store methods (`internal/conversations/`)

- `store_append.go` / `store.go`: thread the new `watermark` arg into
  `loadRecentTurns` / `loadRecentBranchTurns` (they call the two modified queries).
- New: `GetCompaction(ctx, convID) (summary string, throughSeq int, err error)` over
  `GetConversationCompaction`.
- New: `SetCompaction(ctx, convID, summary string, throughSeq int) error` over
  `SetConversationCompaction`, inside the caller-identity tx (`db.WithCallerIdentityTx`,
  matching AppendTurn's scoping).
- `Store` satisfies a new `compactionPersister` seam (see §6).

## 6. Ladder wiring (`internal/conversations/context.go`, `compaction.go`)

Add a persister seam mirroring `rotEmitter`:
```go
type compactionPersister interface {
    persistCompaction(ctx context.Context, conversationID, summary string, throughSeq int) error
}
```
`ContextConfig` gains `CompactionSummary string` (the persisted summary read at load) and
the ladder gains the `compactionPersister` (passed alongside `emit`, wired from the
Store like `emit`).

**Load flow (LoadManagedHistory / …ForBranch):**
1. `summary, watermark := s.GetCompaction(ctx, convID)` (skip if persistence disabled).
2. Pass `watermark` to `loadRecentTurns` (superseded rounds excluded in SQL).
3. `cfg.CompactionSummary = summary`.
4. `applyContextLadder` injects the summary after the head via a new
   `injectCompactionSummary(turns, cfg.CompactionSummary)` (identical shape to
   `injectAlwaysBlock`; marker `__aura_compaction__`, already handled by `isCompaction`
   in `toMessages`). It counts toward the budget and is protected like the always-block.

**Compaction flow (inside `applyContextLadder`, the existing L2.4 hook):** when
`tryCompact` succeeds AND persistence is enabled:
1. `throughSeq` = max `Seq` of the summarized `history` segment.
2. `persist.persistCompaction(ctx, convID, summaryText, throughSeq)`.
3. write the audit row (§7).
4. Return the compacted view (as 1a does). Next load reads the stored summary and skips
   the recompute.

`tryCompact` already returns the summarized turns; expose the raw summary text + the
`throughSeq` so the persister can store them (small refactor of `tryCompact`'s return).

**Idempotency / concurrency:** the runner serializes turns per conversation
(`threadLocks`, D-23), so no two loads of one conversation race. The persist is a single
UPDATE (last-writer-wins on `(summary, watermark)`); a monotonic guard
(`WHERE compacted_through_seq < $3`) makes a stale concurrent write a no-op. Precedent for
a write during load: `insertContextRotEvent`.

## 7. Audit

One row per compaction into `context_rot_events` (action `'compaction'`,
`rounds_summarized` in the `pairs_dropped` column, before/after tokens) — OR a dedicated
`context_compaction_events` table if the NOT NULL relaxation in §3 is unwanted. The
existing `ListContextRotEvents` projection + cockpit gauge then surface compactions.

## 8. Config knob

`AURA_CONTEXT_COMPACTION_PERSIST` (bool, **default `false`** until live-verified).
- false → 1a behaviour exactly (no `GetCompaction`, `watermark=0`, no persist).
- true → the durable path above.
Add to `config.go` + `config_knobs.go` (two-place pattern) and thread to `runner.Deps` →
`Runner` → `ContextConfig`, mirroring `CompactionEnabled`. Ship dark, flip after §10.

## 9. Tests

**Offline unit (no DB, runnable anywhere):**
- `injectCompactionSummary` places the summary after head, protected by L1/L2.5.
- watermark math: `throughSeq` = max history seq; monotonic guard rejects a stale write.
- `tryCompact` returning summary text + throughSeq.
- fake `compactionPersister` (like `fakeRotEmitter`) records the persist call.

**`db_integration` (REQUIRES the stack — the part not runnable in the authoring env):**
- migration 0094 applies + rolls back clean on a disposable DB.
- persist → reload skips superseded rounds (watermark honored by
  `ListRecentTurnsBySeq`), injects the stored summary, and does NOT recompute.
- second compaction merges the prior summary + new rounds, advances the watermark,
  supersedes the old summary.
- branch path (`ListRecentTurnsByBranchPath`) honors the watermark too.
- concurrent-load monotonic guard (no lost/duplicated supersession).
- goleak clean; no `t.Skip` under `$CI` (no-skip-as-green).

## 10. Verification checklist (run ON the stack before merge)

```
make tools                       # ensure sqlc, migrate, golangci-lint present
# after writing the migration + queries:
sqlc generate                    # regenerate internal/db/sqlc; go build ./...
make db-migrate                  # apply 0094 to the dev DB
go test -tags db_integration ./internal/conversations/...   # the new integration tier
bash scripts/coverage_docker.sh  # owned-surface floor >=85% on the full matrix
make quality-full                # vet+lint+race+vuln+coverage with the stack up
# live E2E: a real over-budget conversation compacts once, reloads without recompute,
# compacts again merging the prior summary. Confirm one context_rot_events 'compaction'
# row per compaction and the watermark advancing in aura.conversations.
```
Update `docs/aura-quality-snapshot.md` (the rows whose CI-gate glob matches the changed
files) and the PRD L3 register (mark 1b LANDED with the LIVE measurement) — per
CLAUDE.md, the amendment records the measurement, so it is written AFTER the E2E, not
before.

## 11. Blast radius / rollback

- Touches the **core history load path** (both `ListRecentTurns*` queries) — the highest-
  risk surface. The `watermark=0` / `persist=false` defaults make the change **inert**
  until explicitly enabled, so a bug cannot affect production before the E2E gate.
- A wrong watermark could hide real recent turns from the model. The monotonic guard +
  the "summarize only `history`, keep `active` verbatim" invariant (shared with 1a via
  `splitHeadHistoryActive`) bound the risk; the db_integration reload tests assert the
  exact surviving set.
- Rollback: `migrate down` drops the two columns; with `persist=false` no data depends on
  them. Superseded rounds were never deleted, so nothing is lost on rollback.
