---
phase: 34-agent-loop-correctness-durable-ledger
plan: 01
subsystem: database
tags: [sqlc, pgx, postgres, hitl, paused_states, conversation_turns, sidecar, roadmap]

# Dependency graph
requires: []
provides:
  - "MarkPausedStateResumed(ctx, arg) (int64, error) — :execrows regen so a rows-affected==0 claim drives ErrPauseNotFound inside a shared cross-store tx (D-03)"
  - "ListSpilledSeqsForConversation(ctx, conversationID) ([]int32, error) — read-only lookup of every seq with content_sidecar_path IS NOT NULL for the crash-orphan .content GC (D-09)"
  - "Ledger-free ROADMAP Phase-34 goal prose at both sites (D-07); four success criteria unchanged"
affects: [34-04, 34-05, 34-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "sqlc :execrows to surface RowsAffected() as (int64, error) for a conditional-update idempotency gate inside a shared tx"
    - "read-only :many referenced-seq lookup consumed by a filesystem reconcile GC (set semantics, no ORDER BY)"

key-files:
  created: []
  modified:
    - internal/db/queries/paused_states.sql
    - internal/db/queries/conversation_turns.sql
    - internal/db/sqlc/paused_states.sql.go
    - internal/db/sqlc/conversation_turns.sql.go
    - internal/db/sqlc/querier.go
    - .planning/ROADMAP.md

key-decisions:
  - "D-03: flip MarkPausedStateResumed :exec -> :execrows (SQL body byte-identical); the generated fn now returns rows-affected so 34-05's MarkResumedTx can classify 0 rows as ErrPauseNotFound from inside the cross-store tx, retiring the raw pool.Exec bypass"
  - "D-09: new read-only ListSpilledSeqsForConversation :many placed between CountTurns and the LOCKED SearchConversationTurns trigram block (which stays byte-untouched); no ORDER BY — the GC uses a set"
  - "D-07: dropped 'durable ledger state machine (migration 0025)' from BOTH Phase-34 goal sites; left the phase TITLE ('… + Durable Ledger') and the four success criteria intact — the plan scoped only the goal prose"
  - "Regenerated the three *.sql.go files verbatim via make sqlc (v1.31.1); zero hand-edits; zero migration files touched"

patterns-established:
  - "Land shared sqlc signature changes FIRST (one make sqlc run) so parallel waves build against fixed generated types without a mid-wave regen conflict on querier.go"

requirements-completed: [LOOP-02, LOOP-03, LOOP-09]  # query-layer PREREQUISITES only; runtime satisfaction lands in 34-04/34-05/34-06

coverage:
  - id: D1
    description: "MarkPausedStateResumed regenerated to return (int64, error) via :execrows (RowsAffected surfaced)"
    requirement: LOOP-02
    verification:
      - kind: automated
        ref: "wsl go build ./... green + grep 'func (q *Queries) MarkPausedStateResumed' internal/db/sqlc/paused_states.sql.go shows (int64, error)"
        status: pass
      - kind: unit
        ref: "go test ./internal/db/... (untagged + -race) ok"
        status: pass
    human_judgment: false
  - id: D2
    description: "ListSpilledSeqsForConversation read-only :many returning ([]int32, error) filtered on content_sidecar_path IS NOT NULL"
    requirement: LOOP-09
    verification:
      - kind: automated
        ref: "grep ListSpilledSeqsForConversation internal/db/sqlc/querier.go shows ([]int32, error); go build ./... green"
        status: pass
    human_judgment: false
  - id: D3
    description: "ROADMAP Phase-34 goal reconciled to the ledger-free reality (D-07) at both sites; success criteria unchanged"
    requirement: LOOP-03
    verification:
      - kind: automated
        ref: "grep -c 'durable ledger state machine' and 'migration 0025' == 0; 'single cross-store transaction' present at lines 85 and 211; success-criteria block byte-unchanged"
        status: pass
    human_judgment: false

# Metrics
duration: ~25min
completed: 2026-07-01
status: complete
---

# Phase 34 Plan 01: sqlc Query Prereqs + ROADMAP Ledger Reconciliation Summary

**Regenerated `MarkPausedStateResumed` to `(int64, error)` (:execrows) and added the read-only `ListSpilledSeqsForConversation` query — the two sqlc signatures Waves 2-3 build against — plus dropped the stale durable-ledger/migration-0025 clause from the Phase-34 ROADMAP goal (D-07), all query-only with zero new migration.**

## Performance

- **Duration:** ~25 min (includes a transient WSL service-crash recovery and a concurrent-git-index race recovery — see Issues Encountered)
- **Started:** 2026-07-01T13:12:00Z (approx)
- **Completed:** 2026-07-01T13:35:00Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments
- `MarkPausedStateResumed` now returns `(int64, error)` (annotation `:exec` -> `:execrows`, SQL body byte-identical) so a `RowsAffected()==0` claim can drive `ErrPauseNotFound` from inside the shared cross-store `db.WithTx` transaction that 34-05/34-06 build (D-03) — no hand-written caller existed, so the signature change breaks no build.
- New read-only `ListSpilledSeqsForConversation(ctx, conversationID) ([]int32, error)` returns every `seq` whose `content_sidecar_path IS NOT NULL` for one conversation, so the 34-04 crash-orphan `.content` GC can reconcile live sidecar files against committed rows (D-09).
- Both Phase-34 ROADMAP goal sites now read "single cross-store transaction … crash-orphan reconciliation" instead of "durable ledger state machine (migration 0025)" (D-07); the four success criteria and the phase title are unchanged.
- sqlc regenerated verbatim (v1.31.1); the diff is limited to the two query files + the three generated Go files; **no** `internal/db/migrations/` file changed.

## Task Commits

Each task was committed atomically:

1. **Task 1: Annotate MarkPausedStateResumed :execrows, add ListSpilledSeqsForConversation, regenerate** — `1e0d9912` (feat)
2. **Task 2: Reconcile ROADMAP Phase-34 goal (drop ledger/migration clause, D-07)** — `80c2a6ff` (docs)

**Plan metadata:** this SUMMARY commit (docs).

## Files Created/Modified
- `internal/db/queries/paused_states.sql` - `MarkPausedStateResumed` annotation `:exec` -> `:execrows`
- `internal/db/queries/conversation_turns.sql` - new `ListSpilledSeqsForConversation :many` (placed before the locked trigram block, which is untouched)
- `internal/db/sqlc/paused_states.sql.go` - regenerated `MarkPausedStateResumed` -> `(int64, error)` returning `result.RowsAffected()`
- `internal/db/sqlc/conversation_turns.sql.go` - regenerated `ListSpilledSeqsForConversation` -> `([]int32, error)`
- `internal/db/sqlc/querier.go` - regenerated `Querier` interface (both signatures)
- `.planning/ROADMAP.md` - Phase-34 goal prose at both sites (lines 85, 211)

## Decisions Made
- Followed the plan's locked decisions exactly: D-03 (`:execrows`), D-09 (read-only referenced-seq lookup, no ORDER BY / set semantics), D-07 (ledger-free goal prose).
- Placed the new query between `CountTurns` and the LOCKED `SearchConversationTurns` block so the cross-slice trigram contract stays byte-identical.
- Preserved each ROADMAP site's existing casing (lowercase after `- Goal:` in the phase-list summary; capitalized after `**Goal:**` in the detail section), matching neighboring phases 33/35.
- Left the Phase-34 TITLE ("… + Durable Ledger") and the four success criteria untouched — the plan scoped only the goal prose, not the phase name.

## Deviations from Plan

None - plan executed exactly as written. No deviation rules (1-4) were triggered; both tasks matched their specified actions and acceptance criteria.

## Issues Encountered
- **Transient WSL service crash (`Wsl/Service/E_UNEXPECTED`)** while first invoking `make sqlc`. Recovered with `wsl.exe --shutdown` + cold boot; this plan needs no live DB (sqlc reads local schema; build/vet/unit/race are DB-free), so the recovery had no impact on correctness. sqlc then regenerated cleanly.
- **Concurrent-git-index race** during the first Task-1 commit. A parallel process (a user Codex session — evidenced by newly-appeared, unrelated `internal/mcp/http_client*.go` and `.planning/STATE.md` modifications) rewrote the shared index during the ~99s file-size pre-commit hook, so the first commit captured only an unrelated staged file (`internal/channels/telegram/documents_test.go`) under my message while my five files stayed unstaged. Recovered non-destructively: `git reset --soft HEAD~1` (my own tip, parent `64717e83` — no parallel commit existed above it), unstaged the parallel worker's file to hand it back untouched, then re-committed my five files with a **race-resistant explicit-pathspec commit** (`git commit -- <5 paths>`, which builds from a temp index immune to concurrent real-index mutation). Verified via `git show --stat` that `1e0d9912` contains exactly the five intended files and that `documents_test.go` remains modified/unstaged for the parallel worker. Task 2 used the same pathspec-commit approach.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- The two fixed generated signatures are now on `master`, so Wave-2 plans build against them with no further sqlc regen:
  - **34-05** consumes `MarkPausedStateResumed(ctx, arg) (int64, error)` for `askuser.MarkResumedTx` (rows-affected==0 -> `ErrPauseNotFound`) and deletes the raw `markResumedSQL` `pool.Exec` bypass.
  - **34-04** consumes `ListSpilledSeqsForConversation` for the age-grace `.content` crash-orphan reconcile in `orphan_scan.go`.
  - **34-06** composes both inside the single cross-store `db.WithTx` `ResumeCommitter`.
- No blockers. The concurrent user Codex session remains active on unrelated files (`internal/mcp/*`, `internal/channels/telegram/*`); its work was left untouched.

## Self-Check: PASSED

- All 6 deliverable files present on disk (2 query files, 3 generated `*.sql.go`, `.planning/ROADMAP.md`) + this SUMMARY.
- Both task commits present in `git log`: `1e0d9912` (Task 1, feat), `80c2a6ff` (Task 2, docs).
- `go build ./...`, `go vet ./internal/db/...`, `go test ./internal/db/...` (untagged + `-race`) all green in WSL.
- No `internal/db/migrations/` file added or modified.

---
*Phase: 34-agent-loop-correctness-durable-ledger*
*Completed: 2026-07-01*
