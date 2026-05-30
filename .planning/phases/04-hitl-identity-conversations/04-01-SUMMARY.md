---
phase: 04-hitl-identity-conversations
plan: 01
subsystem: database
tags: [postgres, sqlc, pgx, golang-migrate, pg_trgm, tiktoken-go, hitl, identity, conversations]

# Dependency graph
requires:
  - phase: 01-infra-db-knowledge
    provides: db.Open pgxpool + role separation (aura_app/aura_migrate) + golang-migrate runner + sqlc surface (New/DBTX/WithTx) + EnsureRoles
  - phase: 03-llm-client-toolresult
    provides: llm.Config 4-tier load order + Usage/CostUSD + tools/result.go sidecar layout (sessionID == conversation_id)
provides:
  - "db.WithTx(ctx, pool, fn) atomic-write helper — the DRY tx seam every Phase-4 Store reuses"
  - "migrations 0003-0006 (paused_states, identity+seed, conversations+turns+context_rot_events, pg_trgm FTS) applying + idempotent under role separation"
  - "six per-table sqlc query files + regenerated internal/db/sqlc surface (paused_states/identity/capability_grants/conversations/conversation_turns/context_rot_events) incl. the locked SearchConversationTurns FTS query"
  - "llm.Config.ContextWindow + MaxOutputTokens (L2 budget inputs) via AURA_MODEL_CONTEXT_WINDOW/MAX_OUTPUT_TOKENS"
  - "four AURA_* conversation/context tuning knobs on config.Config"
  - "github.com/pkoukk/tiktoken-go@v0.1.8 dependency (token estimation for L2 gating)"
affects: [04-02-askuser-paused-states, 04-03-identity, 04-04-conversations-context, 04-05-fts-runner, swarm-phase-9, memory-phase-11, kv-cache-phase-6]

# Tech tracking
tech-stack:
  added: [github.com/pkoukk/tiktoken-go@v0.1.8, github.com/dlclark/regexp2@v1.10.0 (tiktoken transitive), pg_trgm Postgres extension]
  patterns:
    - "db.WithTx atomic-write helper (Begin/rollback-on-error/rollback-and-repanic-on-panic/Commit) — pgx.Tx satisfies sqlc.DBTX"
    - "CREATE INDEX CONCURRENTLY isolated as the SOLE statement in its migration file (golang-migrate v4.19.1 implicit-tx hazard)"
    - "FIFO total-order tiebreaker priority DESC, created_at ASC, token ASC (now() ties within one tx)"
    - "spillover = sidecar FILE via content_sidecar_path, NOT a conversation_spillover table"

key-files:
  created:
    - internal/db/tx.go
    - internal/db/migrations/0003_paused_states.{up,down}.sql
    - internal/db/migrations/0004_identity.{up,down}.sql
    - internal/db/migrations/0005_conversations.{up,down}.sql
    - internal/db/migrations/0006_conversation_turns_fts.{up,down}.sql
    - internal/db/queries/{paused_states,identity,capability_grants,conversations,conversation_turns,context_rot_events}.sql
    - internal/db/sqlc/{paused_states,identity,capability_grants,conversations,conversation_turns,context_rot_events}.sql.go
  modified:
    - prd.md
    - go.mod
    - go.sum
    - internal/llm/config.go
    - internal/config/config.go
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/db/db_test.go

key-decisions:
  - "tiktoken-go@v0.1.8 operator-approved before go get (Phase-4 package-legitimacy checkpoint), not the char/4 fallback"
  - "CREATE EXTENSION pg_trgm folded into 0005 so 0006 keeps CREATE INDEX CONCURRENTLY as its sole statement"
  - "ContextWindow default 1000000 (DeepSeek-V4 ~1M window), MaxOutputTokens default 32768"
  - "MarkResumedBatch is a Store-layer loop over MarkPausedStateResumed inside db.WithTx, not a single sqlc statement"
  - "resumed_answer is jsonb (AM-02 three-action {action,content}), added in 0005 (not 0003)"

patterns-established:
  - "db.WithTx: the single multi-statement-write seam for AppendTurn / MarkResumedBatch / Stop auto-resolve"
  - "Phase-4 migration role discipline: aura_migrate DDL, aura_app DML-only, NEVER TRUNCATE/DROP/CREATE; explicit forensic GRANTs belt-and-suspenders"
  - "sqlc pgtype boundary: total_cost_usd -> pgtype.Numeric, nullable cols -> pgtype.Text/Timestamptz/UUID, jsonb -> []byte (downstream Stores convert at the boundary)"

requirements-completed: [CORE-02, CORE-03, CORE-04, CORE-05]

# Metrics
duration: ~40min
completed: 2026-05-30
---

# Phase 4 Plan 01: HITL + Identity + Conversations Substrate Summary

**The shared Phase-4 DB substrate: db.WithTx atomic-write helper, migrations 0003-0006 (paused_states / identity+seed / conversations+turns+context_rot_events / pg_trgm FTS) applying idempotently under role separation, six regenerated sqlc query files incl. the locked FTS query, plus the L2 budget config inputs and the tiktoken-go dependency.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-05-30 (session)
- **Completed:** 2026-05-30
- **Tasks:** 3 (1 checkpoint operator-pre-approved + 2 auto)
- **Files modified/created:** 29

## Accomplishments
- `db.WithTx(ctx, pool, fn)` — the one DRY transaction seam (Begin → rollback-on-error → rollback-and-repanic-on-panic → Commit; `pgx.Tx` satisfies `sqlc.DBTX`) that `conversations.Store.AppendTurn` (SC-2), `askuser.Store.MarkResumedBatch`, and `Runner.Stop` auto-resolve will all reuse.
- Migrations `0003`–`0006` land the full Phase-4 schema: `paused_states` (conversation_id `text`, NO FK, partial pending index, proxied_* NULL), `identities` + `capability_grants` with the seeded `local`/system `...001` identity + `(...001,'*')` grant, `conversations` + `conversation_turns` + `context_rot_events`, the `paused_states.conversation_id` `text`→`uuid`+FK promotion + `resumed_answer jsonb` (AM-02), and the `pg_trgm` GIN CONCURRENTLY index. Every up has a reversing down.
- Six per-table sqlc query files generate a compiling Querier exposing the Phase-4 surface, including the locked cross-slice FTS query `content % $1 ORDER BY similarity(content,$1) DESC LIMIT $2` (`SearchConversationTurns`).
- L2 budget inputs resolved: `llm.Config.ContextWindow` (1000000) + `MaxOutputTokens` (32768) via the existing fail-fast 4-tier env chain; four `AURA_*` conversation/context knobs on `config.Config` via non-fatal `envIntDefault`.
- PRD carries the grouped §Phase 4 amendment block (AM-01 agent stays DB-free / AM-02 jsonb three-action / AM-03 Loop→Runner) + the OQ2 (L2 inputs) and OQ3 (sidecar-not-table) resolutions.
- Live-verified in WSL with the stack up: full `db_integration -race` suite green, `golangci-lint ./internal/db/... == 0`, Reset down→up round-trip green, and a new `TestMigrate_Phase4_AppliesAndSeeds` proving fresh-DB apply-4(+2 base)/re-run-0/seed/role-denial on a throwaway database.

## Task Commits

1. **Task 1: tiktoken-go package-legitimacy checkpoint** — operator pre-approved ("approved") before any `go get`; no re-pause. Recorded as a normal-flow decision (not a deviation).
2. **Task 2: PRD amendments AM-01/02/03 + tiktoken-go + L2 budget config inputs** — `078e2995` (feat)
3. **Task 3: db.WithTx + migrations 0003-0006 + 6 query files + sqlc regen** — `ce1862b9` (feat)

_The PRD-amendment prose was committed together with Task 2's code (one atomic foundation commit), per CLAUDE.md commit discipline (PRD-amendment with code at the phase head)._

## Files Created/Modified
- `internal/db/tx.go` — `WithTx` atomic-write helper (the DRY tx seam)
- `internal/db/migrations/0003_paused_states.{up,down}.sql` — HITL pause table (text conversation_id, partial pending index)
- `internal/db/migrations/0004_identity.{up,down}.sql` — identities + capability_grants + seeded local/'*'
- `internal/db/migrations/0005_conversations.{up,down}.sql` — conversations + conversation_turns + context_rot_events + paused_states FK/uuid/resumed_answer alter + CREATE EXTENSION pg_trgm
- `internal/db/migrations/0006_conversation_turns_fts.{up,down}.sql` — pg_trgm GIN CONCURRENTLY index (single-statement)
- `internal/db/queries/*.sql` (6) — per-table sqlc queries incl. locked FTS
- `internal/db/sqlc/*` — regenerated (models.go, querier.go, six *.sql.go)
- `internal/db/db_test.go` — `TestMigrate_Phase4_AppliesAndSeeds` (fresh-DB apply/idempotent/seed/role-denial)
- `prd.md` — §Phase 4 PRD Amendments block (AM-01/02/03 + OQ2/OQ3 resolutions + dep note)
- `go.mod` / `go.sum` — tiktoken-go@v0.1.8 (+ regexp2 transitive)
- `internal/llm/config.go` — ContextWindow + MaxOutputTokens (L2 inputs)
- `internal/config/config.go` — four AURA_* conversation/context tuning knobs

## Decisions Made
- **tiktoken-go over char/4:** the operator pre-approved the dependency at the Phase-4 package-legitimacy checkpoint, so the CONTEXT D-A2-06 cl100k_base path stands; the char/4 fallback (a D-A2-06 deviation) was not taken.
- **pg_trgm extension placement:** `CREATE EXTENSION IF NOT EXISTS pg_trgm` lives in `0005` (a tx-safe statement) so `0006` can keep `CREATE INDEX CONCURRENTLY` as its sole statement — the verified golang-migrate v4.19.1 behavior is that the postgres driver executes the migration body via `conn.ExecContext` (no explicit tx wrap), but a multi-statement string is still sent as an implicit tx block which CONCURRENTLY forbids.
- **Defaults:** ContextWindow 1000000 (~1M DeepSeek-V4), MaxOutputTokens 32768; documented in the const block + PRD amendment.
- **MarkResumedBatch shape:** a per-key map batch isn't expressible as one sqlc statement, so the batch resolve is a Store-layer loop over `MarkPausedStateResumed` inside `db.WithTx` (04-02 implements it); `AutoResolvePendingForConversation` covers the single-statement `Runner.Stop` path.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Throwaway-DB integration test needed CREATE on the public schema, not just the database**
- **Found during:** Task 3 (the new `TestMigrate_Phase4_AppliesAndSeeds`)
- **Issue:** The first run failed with `permission denied for schema public` when golang-migrate created its `schema_migrations` tracker in the fresh throwaway DB — `GRANT CREATE ON DATABASE` alone is insufficient on Postgres 17+ (CREATE on the `public` *schema* is default-revoked from non-owners).
- **Fix:** Added a grant on a connection *into* the fresh DB: `GRANT CREATE ON SCHEMA public TO aura_migrate` (mirrors `EnsureRoles` migrate.go:130 for the primary DB).
- **Files modified:** `internal/db/db_test.go`
- **Verification:** `TestMigrate_Phase4_AppliesAndSeeds` PASS in WSL (fresh DB, 6 applied / re-run 0 / seed present / aura_app DDL denied 42501).
- **Committed in:** `ce1862b9` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (1 test-correctness bug, scoped to this plan's own new test).
**Impact on plan:** No scope creep — the fix is a test-harness grant required to exercise the migrations on a clean database; production `EnsureRoles` already does the equivalent.

## Issues Encountered
- **WSL `$()` capture on `/mnt/d` (9p drvfs) returned empty for `.env` reads**, while the same `grep`/`cat` printed correctly to stdout, and the Bash tool's Git-Bash wrapper mangled `wsl bash /mnt/d/...` paths. Resolved by copying `.env` to the Linux fs (`cat … > /tmp/aura.env`), extracting primitives with `sed -n 's/^KEY=//p'`, invoking the runner as a script file with `MSYS_NO_PATHCONV=1`, and `set +H` for the `!`-containing password. This is environment plumbing only — no code impact. (Worth a memory note: live WSL integration runs from this Windows host need the `/tmp` copy + `MSYS_NO_PATHCONV=1` + `set +H` recipe.)

## User Setup Required
None — no external service configuration required. The single new dependency (tiktoken-go) is a Go module; it may fetch the cl100k_base vocab over the network at first *use* (the offline-encoder configuration is handled in 04-04, not here).

## Next Phase Readiness
- **04-02 (ask_user / paused_states):** `internal/db/sqlc` exposes `InsertPausedState`/`GetPausedStateByToken`/`ListPendingPausedStates` (FIFO `priority DESC, created_at ASC, token ASC`)/`MarkPausedStateResumed`/`AutoResolvePendingForConversation`/`CleanupResumedOlderThan`; `db.WithTx` is ready for `MarkResumedBatch`.
- **04-03 (identity):** `CreateIdentity`/`GetIdentityByName`/`GetIdentityByID`/`ListIdentities`/`DeleteIdentity` + `GrantCapability`/`RevokeCapability`/`ListCapabilities`/`HasCapability` (wildcard-or-exact) generated; the seeded `local`/'*' row is verified live.
- **04-04 (conversations + context):** `CreateConversation`/`GetConversation`/`ListConversations`(--archived)/`UpdateConversationStatus`/`RenameConversation`/`SetConversationTitleIfNull`/`UpdateConversationAggregates`(SQL `+=`)/`DeleteConversation` + `InsertConversationTurn`/`ListTurnsBySeq`/`CountTurns` + `InsertContextRotEvent`; `content_sidecar_path` column ready for the sidecar spill; `llm.Config.ContextWindow`/`MaxOutputTokens` + the four `AURA_*` knobs feed the L1/L2/L2.5 ladder.
- **04-05 (FTS + runner):** `SearchConversationTurns` is the locked cross-slice query (Telegram /search reuses it byte-for-byte in Phase 13).
- **pgtype boundary note for downstream Stores:** `total_cost_usd` → `pgtype.Numeric`, nullable cols → `pgtype.Text`/`pgtype.Timestamptz`/`pgtype.UUID`, jsonb → `[]byte`. Convert at the Store boundary (Pitfall 5).

## Self-Check: PASSED

All created files present (`internal/db/tx.go`, migrations 0003-0006, six query files, regenerated sqlc surface, SUMMARY). Both task commits present in git history (`078e2995`, `ce1862b9`).

---
*Phase: 04-hitl-identity-conversations*
*Completed: 2026-05-30*
