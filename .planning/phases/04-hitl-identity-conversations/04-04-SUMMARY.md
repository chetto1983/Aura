---
phase: 04-hitl-identity-conversations
plan: 04
subsystem: conversations
tags: [conversations, context-management, tiktoken, fts, pg_trgm, sidecar-spill, orphan-scan, auto-title, store-pattern]

# Dependency graph
requires:
  - phase: 04-hitl-identity-conversations
    plan: 01
    provides: migrations 0005/0006 (conversations + conversation_turns + context_rot_events + pg_trgm FTS) applied; sqlc surface (CreateConversation/Get/List/UpdateStatus/Rename/SetTitleIfNull/UpdateConversationAggregates/Delete + InsertConversationTurn/ListTurnsBySeq/CountTurns/SearchConversationTurns + InsertContextRotEvent); db.WithTx; llm.Config.ContextWindow/MaxOutputTokens; the four AURA_* conversation knobs; tiktoken-go@v0.1.8
  - phase: 04-hitl-identity-conversations
    plan: 02
    provides: the canonical Store{pool,q} pattern (SQLSTATE classification, sentinel errors, pgtype boundary)
  - phase: 03-llm-client-toolresult
    provides: llm.Message/Usage shapes; tools/result.go sidecar layout (sessionID == conversation_id); agenttest.FakeClient
provides:
  - "internal/conversations.Store — multi-thread persistence: Create/Get/List/UpdateStatus/Rename/SetTitleIfNull/CountTurns/AppendTurn/LoadHistory/SearchConversationTurns/Delete"
  - "AppendTurn: atomic INSERT turn + UPDATE aggregates in ONE db.WithTx (SC-2); sidecar spill > AURA_CONVERSATION_TURN_CAP_BYTES; token+USD aggregation (pgtype.Numeric SQL +=)"
  - "LoadHistory: byte-identical []llm.Message reconstruction ORDER BY seq (Req#8); sidecar-spilled content rehydrated from disk"
  - "LoadManagedHistory + the L1/L2/L2.5 deterministic context ladder (context.go) — L1 tool-clearing (never seq=1), L2 budget gate, L2.5 oldest-pair drop with one context_rot_events row; SC-1 L1-first"
  - "offline cl100k_base tiktoken encoder (cached sync.Once, VENDORED vocab + custom embedded BpeLoader — zero network, zero new dep)"
  - "SearchConversationTurns — the LOCKED cross-slice FTS query wrapper (Telegram /search Phase 13 reuses the SQL verbatim)"
  - "ScanOrphans — symlink-guarded boot reconciliation GC (orphan conversation dirs + tmp TTL sweep + audit-only size WARN)"
  - "generateTitle — the best-effort auto-title worker BODY (Runner owns the WaitGroup, 04-05)"
affects: [04-05-runner, swarm-phase-9, memory-phase-11, kv-cache-phase-6, telegram-phase-13]

# Tech tracking
tech-stack:
  added: [vendored cl100k_base.tiktoken BPE vocab (offline encoder asset)]
  patterns:
    - "offline tiktoken: //go:embed the cl100k_base.tiktoken blob + a custom one-method tiktoken.BpeLoader; SetBpeLoader BEFORE GetEncoding so the encoder is served from memory, NEVER HTTP-fetched (A6). Cached via sync.Once (no goroutine, no network) — chosen over the tiktoken-go-loader dependency to avoid a new package mid-execution"
    - "AppendTurn folds the turn INSERT + aggregates UPDATE into one db.WithTx (SC-2 crash atomicity); sidecar spill happens BEFORE the tx (file write is not part of DB atomicity — a rolled-back tx leaves an orphan file the boot scan reconciles)"
    - "total_cost_usd aggregated in SQL via a pgtype.Numeric delta at numeric(10,4) scale (big.Int mantissa, Exp -4) so total_cost_usd + $delta stays exact (Pitfall 5)"
    - "L1 microcompact rewrites a COPY of the turns (input never mutated) so LoadHistory byte-identity holds; only role='tool' turns older than the evict window, NEVER seq=1 (KV-cache poisoning, Pitfall 1)"
    - "L2.5 oldest-pair drop preserves the system L0 + keeps the non-system remainder even; emits exactly one context_rot_events row via a narrow rotEmitter interface (unit tests pass a fake, no DB)"
    - "ScanOrphans symlink guard: os.Lstat (never follows) — a symlink entry under conversations/ is unlinked, never RemoveAll'd through to an external target (EoP T-04-14)"

key-files:
  created:
    - internal/conversations/store.go
    - internal/conversations/store_helpers.go
    - internal/conversations/context.go
    - internal/conversations/tiktoken.go
    - internal/conversations/orphan_scan.go
    - internal/conversations/title.go
    - internal/conversations/cl100k/cl100k_base.tiktoken
    - internal/conversations/store_test.go
    - internal/conversations/store_unit_test.go
    - internal/conversations/context_test.go
    - internal/conversations/context_unit_test.go
    - internal/conversations/orphan_scan_test.go
    - internal/conversations/title_unit_test.go
    - internal/conversations/main_test.go
    - scripts/run_conversations_integration.sh
  modified: []

key-decisions:
  - "OFFLINE tiktoken via vendored vocab + custom embedded BpeLoader, NOT the tiktoken-go-loader companion dependency. Adding a new package mid-execution is excluded from the auto-fix rules (package-install) and 04-01 approved only tiktoken-go itself; the embedded loader (a ~15-LOC one-method impl over a //go:embed of the verbatim openaipublic cl100k_base.tiktoken blob) achieves the same zero-network result with no new go.mod entry. Proven by a unit test that initializes the encoder and tokenizes ('hello world' = 2) with no network."
  - "AppendTurn spills the sidecar file BEFORE the db.WithTx (not inside it): the file write is intentionally outside the DB atomicity boundary so a rolled-back turn leaves at most an orphan file, which the boot ScanOrphans reconciles — vs. risking a half-written DB+FS state. SC-2 (no partial turn in the DB) holds regardless."
  - "L2 WARN wired as an audit slog.Warn at the 0.75x warn-cap; the heavy reduction is L2.5's job. Over-hard + unreducible returns ErrContextWindowExceeded (mentions `aura chat new`) for the REPL to surface — NEVER the iter.Seq2 error slot."
  - "context_rot_events emission goes through a narrow rotEmitter interface so the ladder logic is unit-testable with a fake (no DB); the *Store impl is the production emitter. The DB-backed row is asserted under db_integration (SC-1 zero rows / L2.5 one row)."
  - "generateTitle is a plain llm.Client.Stream call (drains the channel) — NOT the full agent loop; the Runner (04-05) owns the WithoutCancel/WithTimeout/WaitGroup wiring. Errors never block chat (caller leaves title NULL)."

patterns-established:
  - "conversations.Store COPIES the 04-02 canonical Store pattern verbatim and extends it with: db.WithTx atomic per-turn write (SC-2), pgtype.Numeric SQL-side cost aggregation, sidecar-spill-on-write + rehydrate-on-read, the deterministic L1/L2/L2.5 ladder, and the locked FTS wrapper"
  - "offline embedded-vocab loader pattern (reusable for any future tiktoken use): vendor the blob, //go:embed it, implement the one-method BpeLoader, SetBpeLoader before GetEncoding, cache via sync.Once"
  - "test discipline: unit tier (pure ladder + projection + numeric + title-worker via FakeClient, no DB/network) + db_integration tier (goleak, envOrSkip t.Fatal-under-CI, -race) — combined coverage across the full tag matrix"

metrics:
  tasks: 3
  duration: ~75min
  completed: 2026-05-30
  coverage: "89.6% combined (unit + db_integration), floor 85%"
  files-created: 15
  files-modified: 0

requirements-completed: [CORE-04, CORE-05]
---

# Phase 4 Plan 04: Conversation Persistence + Deterministic Context Management Summary

**One-liner:** The durable conversation core — `conversations.Store` with crash-atomic `AppendTurn` (one `db.WithTx`: turn INSERT + aggregates UPDATE, SC-2), byte-identical `LoadHistory` (Req#8), sidecar spill, token+USD aggregation, and the locked `pg_trgm` FTS wrapper — plus the deterministic L1/L2/L2.5 context ladder over a fully-offline cached cl100k tiktoken encoder, a symlink-guarded boot orphan-scan GC, and the best-effort auto-title worker body.

## What Was Built

### Task 1 — conversations.Store (commit 93b68f52)
`store.go` + `store_helpers.go`: the canonical `Store{pool,q,runDir,turnCapBytes}` with `Create/Get/List/UpdateStatus/Rename/SetTitleIfNull/CountTurns/AppendTurn/LoadHistory/SearchConversationTurns/Delete`.
- **SC-2 atomicity:** `AppendTurn` folds the turn `INSERT` + aggregates `UPDATE` into one `db.WithTx`; an injected failure between them rolls back with no partial turn (live-verified by `TestAppendTurn_AtomicRollback`).
- **Req#8 byte-identity:** `LoadHistory` reconstructs `[]llm.Message` `ORDER BY seq` as a pure function of the persisted rows; two calls are byte-equal after a fresh-Store restart.
- **Sidecar spill:** content `> AURA_CONVERSATION_TURN_CAP_BYTES` writes `<run_dir>/conversations/<id>/<seq>.content` (traversal-guarded), row stores `content=NULL` + `content_sidecar_path`; `LoadHistory` rehydrates from disk.
- **Aggregation:** token columns summed; `total_cost_usd` via a `pgtype.Numeric` SQL `+=` at `numeric(10,4)` scale (exact).
- **FTS:** `SearchConversationTurns` wraps the LOCKED query verbatim (similarity-DESC verified).
- **Delete:** DB delete (turns + paused_states cascade) then `os.RemoveAll` the sidecar dir.

### Task 2 — L1/L2/L2.5 ladder + offline tiktoken (commit eb80e3c0)
`context.go` + `tiktoken.go` + vendored `cl100k/cl100k_base.tiktoken`:
- **L1** rewrites `role='tool'` turns older than the evict window to a `read_tool_output(<id>)` pointer, **NEVER seq=1** (KV-cache poisoning, Pitfall 1); operates on a copy so byte-identity holds.
- **L2** `hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000`; over `0.75×hard` → audit WARN.
- **L2.5** drops the oldest user/assistant **pair** (system L0 preserved, remainder kept even) and writes exactly one `context_rot_events {action:'hard_drop_pairs'}` row. Over-hard + unreducible → `ErrContextWindowExceeded` (suggests `aura chat new`).
- **SC-1** proven live: a tool-output-bloated history fits after L1 alone with **zero** `context_rot_events` rows.
- **Offline tiktoken:** cached cl100k_base encoder (`sync.Once`, no goroutine, no network) served from a **vendored** embedded vocab via a custom `tiktoken.BpeLoader` — `SetBpeLoader` before `GetEncoding`. No new dependency.

### Task 3 — orphan scan GC + auto-title worker (commit 9be6510f)
`orphan_scan.go` + `title.go`:
- **ScanOrphans** removes `conversations/<id>` dirs with no DB row under an `Lstat` symlink-escape guard (a symlink is unlinked, never traversed — EoP T-04-14, live-verified the external target survives), sweeps `tmp/*` >24h, and logs an **audit-only** size WARN (never purges).
- **generateTitle** is the best-effort worker body: one `llm.Client.Stream` call producing a sanitized 4-6 word title; errors never block chat (caller leaves title NULL → `(untitled ...)`). The WaitGroup/WithoutCancel wiring is the Runner's (04-05).

## Offline-tiktoken approach (RESEARCH A6)

The default `tiktoken-go` loader HTTP-GETs `cl100k_base.tiktoken` from openaipublic on first use. To guarantee zero network at runtime/CI (A6 + `feedback_minipc_cpu_budget`) **without** adding the `tiktoken-go-loader` companion dependency mid-execution, the verbatim 1.68 MB BPE blob is **vendored** at `internal/conversations/cl100k/cl100k_base.tiktoken`, `//go:embed`-ed, and served by a ~15-LOC custom `tiktoken.BpeLoader` installed via `SetBpeLoader` before `GetEncoding`. `TestOfflineEncoder_NoNetwork` exercises L2 token counting (`hello world` = 2 tokens) with no network access, proving the offline path.

## AppendTurn atomicity proof (SC-2)

`TestAppendTurn_AtomicRollback` replays the exact statements `AppendTurn` runs inside one `db.WithTx`: it `InsertConversationTurn`, then returns an injected error BEFORE the aggregates `UPDATE`. The tx rolls back and `SELECT count(*) FROM aura.conversation_turns` is unchanged — no partial turn survives. Verified live (`-race`, WSL).

## Coverage + quality

- **Combined coverage: 89.6%** (unit + `db_integration`, floor 85%) — measured live in WSL with the stack up.
- `golangci-lint run ./internal/conversations/` == **0 issues** (tightened sidecar perms to 0o750/0o600; wired the L2 warn-cap slog).
- `go build ./...` + `go vet ./...` exit 0; every file ≤ 600 LOC; `db_integration -race` suite green (3.9s — real runtime, not a skip tell).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Offline tiktoken without a new dependency**
- **Found during:** Task 2.
- **Issue:** The plan/RESEARCH A6 require an offline tiktoken encoder; the canonical offline path is the `tiktoken-go-loader` package, but adding a new dependency mid-execution is excluded from auto-fix (package-install) and 04-01 approved only `tiktoken-go`.
- **Fix:** Vendored the cl100k_base BPE blob + a custom `//go:embed` `BpeLoader` (no new go.mod entry).
- **Files:** `internal/conversations/tiktoken.go`, `internal/conversations/cl100k/cl100k_base.tiktoken`.
- **Commit:** `eb80e3c0`.

**2. [Rule 1 - Quality] golangci-lint gosec/revive/unused on first lint pass**
- **Found during:** Task 3 phase-gate lint.
- **Issue:** gosec G301/G306 (0o755/0o644 perms), a revive doc-comment form, and an unused `l2WarnRatio` const.
- **Fix:** Tightened sidecar perms to 0o750/0o600, fixed the doc comment, and wired `l2WarnRatio` into the L2 audit WARN (implementing the SPEC L2 warn-cap behavior).
- **Files:** `store_helpers.go`, `store.go`, `context.go`.
- **Commit:** `9be6510f`.

**Total deviations:** 2 auto-fixed (1 blocking-resolution, 1 quality). No scope creep — both keep the plan inside its boundary (Store + helpers; no Runner/composition root, which is 04-05).

## Scope boundary held

This plan delivered ONLY the Store + helpers the Runner consumes. NOT built (04-05): the Runner, the WaitGroup ownership, the composition root, the CLI wiring. `generateTitle` is a body the Runner's WaitGroup invokes; `ScanOrphans` is called from the 04-05 boot path.

## Next Phase Readiness (04-05 runner + FTS CLI)
- `LoadManagedHistory(ctx, convID, ContextConfig)` is the ladder-applied entry the Runner calls each turn; `LoadHistory` is the raw byte-identical reconstruction.
- `AppendTurn` is the atomic per-turn write the Runner drives from the observed Event stream; `CountTurns` gates the `seq >= 3` auto-title fire.
- `generateTitle(ctx, client, model, history)` is the worker body for the Runner's `WithoutCancel`+`WithTimeout`+`WaitGroup` (D-A5-01).
- `ScanOrphans(ctx, pool, ScanParams)` is the boot GC for the composition root (after `db.Open`, before serving).
- `SearchConversationTurns(query, limit)` + `SearchResult` feed the `aura chat search` CLI excerpt rendering (app-side, per D-A5-03).

## Self-Check: PASSED

All created files present; all three task commits in git history (`93b68f52`, `eb80e3c0`, `9be6510f`).

---
*Phase: 04-hitl-identity-conversations*
*Completed: 2026-05-30*
