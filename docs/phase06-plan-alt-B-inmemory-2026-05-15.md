# Phase 6 Alt B — IN-MEMORY HOT PATH + LAZY PERSIST

**Date:** 2026-05-15 · **Status:** alternative architecture proposal

## 1. One-sentence pitch

Tool observations live in a per-run ring buffer, briefer reads in-memory, and SQLite is touched only by an async drain at run-end.

## 2. Story breakdown (7 stories)

**US-J01B** — `ToolObservation` + 5-bucket classifier (same as Alt A US-J01).

**US-J02B** — `ObservationRing` per-run in-memory buffer. New `internal/agent/observe/ring.go`. Created at top of `runLoop`, destroyed when function returns. `executor.go` writes via `OnObservation` callback wired alongside `OnToolEnd` (`loop.go:124-125`).

**US-J03B** — `CrossRunLRU` of recent failures. New `internal/agent/observe/lru.go` — process-global, sync.Mutex-guarded, capped at 256 entries keyed by `(tool_name, error_class)`. Each entry holds ≤3 most-recent redacted observations.

**US-J04B** — Pre-LLM briefer reads in-memory only. New step at end of `governance.Apply`. Merges per-run ring + cross-run LRU. Emits ≤200 chars per tool, ≤8 tools.

**US-J05B** — In-run recoverable feedback + per-(tool, class) budgets. Counters live on the same struct that already owns `toolCallExecutions` (`loop.go:220`).

**US-J06B** — `tool_attempts` migration v10 + lazy drain goroutine. Same schema as Alt A. New `internal/agent/observe/flusher.go` owns a single per-process goroutine started in `app.go:519-521`. Trigger sources: (a) `runLoop` defer sends ring snapshot on buffered channel; (b) 5-second `time.Ticker`; (c) `App.Stop` cancels ctx, drains under 3-s deadline.

**US-J07B** — Closure docs.

## 3. Data structures

```go
// internal/agent/tools/registry/observation.go
type Outcome uint8
const ( OutcomeOK Outcome = iota; OutcomeRecoverable; OutcomeBlocked; OutcomeFatal; OutcomeCancelled )

type ToolObservation struct {
    RunID        string
    ToolName     string
    AttemptN     uint16
    Outcome      Outcome
    Class        string
    Reason       string
    ArgsHash     [32]byte
    ArgKeys      []string
    SchemaHash   [16]byte
    StartedAt    int64
    ElapsedNanos int64
}

// internal/agent/observe/ring.go
type ObservationRing struct {
    mu   sync.Mutex
    buf  [256]ToolObservation // 256 × ~120B = ~30 KB per run
    head int
    full bool
}
func (r *ObservationRing) Push(obs ToolObservation)
func (r *ObservationRing) RecentForTool(name string, k int) []ToolObservation
func (r *ObservationRing) Snapshot() []ToolObservation

// internal/agent/observe/lru.go
type CrossRunLRU struct {
    mu     sync.Mutex
    items  map[string]*lruEntry
    order  *list.List
    cap    int      // 256 entries
    perKey int      // ≤3 per entry
}

// internal/agent/observe/flusher.go
type sqlFlusher struct {
    db      *sql.DB
    in      chan flushBatch   // buffered, cap 64
    logger  *slog.Logger
    lru     *CrossRunLRU
}
func (f *sqlFlusher) Run(ctx context.Context)            // tick=5s
func (f *sqlFlusher) DrainAndClose(ctx context.Context) error
```

**Capacities:** ring 256 covers `MaxIterationsCeiling=100`-bounded runs; ~30KB × ~10 concurrent ≈ 300KB peak. LRU 256×3 ≈ 92KB resident. Channel cap 64 — at one submission per ~30s, never fills.

## 4. Hot-path cost

**Writes, N=8 tool calls:** 8 × O(1) Push under one mutex; zero allocation beyond the struct value. **Zero SQLite calls.**

**Briefer, M=6 pool tools:** 6 × ring scan + 6 × LRU map probe = ~5µs total. No I/O.

**Memory:** per active run ~30KB; process LRU ~92KB; flush backlog bounded ≤1.9MB worst case.

Compare Alt A: 8 INSERTs contending with `archive_turns` + `run_events` + `cron` + auth writers on the same DB pool. Under load Alt B keeps hot path L1-cache friendly.

## 5. Failure modes

| Scenario | Lost | Surfaced |
|---|---|---|
| Process crash mid-run | All active rings + un-flushed batches (≤5s × active runs) | Counter `tool_obs_lost_estimate` |
| App.Stop with pending flushes | None if drain ≤3s; bounded otherwise | Log `flusher: drain_deadline_exceeded` |
| Goroutine leak | Memory + held DB conn | `Run(ctx)` exits on ctx.Done; `bgWg.Wait()` blocks Stop |
| Channel saturation | Drop, not block (`select { default: log.Warn }`) | Counter `tool_obs_dropped_total` |

**Data loss honest accounting:** worst case kernel panic = up to 5s × active runs × ~30 obs of learning signal. User-visible artifacts (errors, replies) durably archived by `archive_turns.go` — Phase 6 signal is supplementary.

## 6. Pros vs Alt A and Alt C

**vs Alt A:** WIN zero SQLite write contention on hot path, sub-µs briefer, per-run ring auto-bounds. LOSE ≤5s signal loss on crash, adds goroutine to App.Stop.

**vs Alt C:** WIN strict typed schema (vs JSON blob string parsing), O(1) briefer lookup (vs full-table scan), cross-conversation signal via LRU. LOSE Alt C adds zero tables; Alt B still needs migration v10.

## 7. Risks + mitigations

1. **Data loss on crash (≤5s × active runs).** Mitigation: flush-on-every-32-obs trigger AND run-end defer-submit; bounds loss to 32 obs even under tick miss.
2. **Goroutine lifecycle bugs.** Mitigation: register via `a.startBg(f.Run)` matching the proven `toolReconciler` pattern at `app.go:578`.
3. **LRU starvation under high cardinality.** Mitigation: 256 entries cover ~50 tools × ~5 classes = 250 keys; metric to monitor eviction rate.

## 8. Estimated LOC

| Story | Prod | Tests | Total |
|---|---|---|---|
| US-J01B observation + bucket | 80 | 60 | 140 |
| US-J02B ring | 100 | 80 | 180 |
| US-J03B LRU | 100 | 70 | 170 |
| US-J04B briefer | 130 | 90 | 220 |
| US-J05B recoverable + budgets | 110 | 90 | 200 |
| US-J06B migration + flusher | 200 | 140 | 340 |
| US-J07B docs | 60 | 0 | 60 |
| **Total** | **780** | **530** | **~1310** |
