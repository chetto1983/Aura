# Phase 6 — Three Alternatives Side-by-Side

**Date:** 2026-05-15 · **Source:** 3 parallel Plan agents, each given one constraint lens.

Read the full plans at:

- `docs/phase06-plan-alt-A-storage-first-2026-05-15.md`
- `docs/phase06-plan-alt-B-inmemory-2026-05-15.md`
- `docs/phase06-plan-alt-C-piggyback-2026-05-15.md`

## Pitch

| Alt | Pitch |
|---|---|
| **A** Storage-first | `tool_attempts` SQLite table is the synchronous source of truth — every observation writes before the loop advances, every briefing reads back from disk. |
| **B** In-memory + lazy | Tool observations live in a per-run ring buffer, briefer reads in-memory, SQLite is touched only by an async drain at run-end. |
| **C** Archive piggyback | A read-only SQL view + small briefer that turns Aura's existing `conversations` + `run_events` rows into the tool-experience loop with zero new tables. |

## Side-by-side matrix

| Dimension | Alt A (Storage-first) | Alt B (In-memory + lazy) | Alt C (Piggyback) |
|---|---|---|---|
| **Stories** | 7 | 7 | 6 |
| **New tables** | 1 (`tool_attempts`) | 1 (`tool_attempts` lazy) | 0 |
| **New indexes** | 4 + 1 view | 4 + 1 view | 2 |
| **New Go packages** | 1 (`attempts`) | 1 (`observe`) with 3 files (ring, lru, flusher) | 1 (`experience`) |
| **Total LOC** | ~545 impl + ~620 tests | ~780 impl + ~530 tests | ~500 total |
| **Hot-path write cost** | 1 INSERT per tool call | 0 (in-memory only) | 0 (no new writes) |
| **Briefer read cost** | ~6µs SQL query w/ index | ~5µs in-memory | ~1ms SQL JSON-extract w/ new index |
| **Per-turn latency added** | ~0.3 ms | ~0 ms (sub-µs) | ~1 ms briefer-only |
| **Crash durability** | Zero loss (atomic writes) | ≤5s × active runs lost | Zero loss (existing tables) |
| **Cross-run learning warm-up** | Immediate | 5s+ after restart | Immediate |
| **CONV_ARCHIVE_ENABLED dep** | None | None | Soft (degrades to class-only) |
| **Schema evolution** | Easy (add columns) | Easy (add columns) | Painful (JSON-bag versioning) |
| **Phase-K (idempotency keys, durable workflow) future-fit** | Good — extend `tool_attempts` | Good — extend `tool_attempts` | Poor — would need new table later anyway |
| **Operator surface (`/api/tool-warnings`)** | One-line SQL view | View over same table | Aggregate query over `run_events` |
| **Goroutine count delta** | 0 | +1 (Flusher) | 0 |
| **Migration v10 risk** | Medium (new table + 4 indexes + view) | Medium (new table + 4 indexes + view) | Low (2 indexes only) |

## Per-PRD-gate coverage

| PRD §6 gate (in-scope subset) | Alt A | Alt B | Alt C |
|---|---|---|---|
| ToolObservation contract (5 buckets) | ✓ US-J01 | ✓ US-J01B | ✓ US-C02 |
| Recoverable error correctable same run | ✓ US-J05 | ✓ US-J05B | ✓ US-C04+C05 |
| Repeat failures visible by tool + class | ✓ US-J06 view | ✓ US-J06B view | ✓ US-C06 aggregate |
| Secrets/raw args redacted in learning records | ✓ args_hash + arg_keys | ✓ args_hash + arg_keys | ✓ already redacted in archive |
| Lessons versioned vs tool schema | ✓ tool_schema_hash column | ✓ tool_schema_hash column | ✗ payload_json bag (deferred to Phase-K refactor) |
| No auto prompt/code mutation | ✓ (non-goal) | ✓ (non-goal) | ✓ (non-goal) |

## Recommendation matrix

| User priority | Pick |
|---|---|
| Ship fastest, smallest diff, lowest maintenance footprint | **C** |
| Operator dashboard is a first-class product surface; want queryable, evolvable schema | **A** |
| Hot-path latency matters more than crash durability; you trust the existing archive | **B** |
| Phase-K (durable workflows + idempotency keys + lesson promotion) is < 6 months away | **A** (avoid double-refactor) |
| Phase-K is unclear / far / maybe not | **C** (defer the schema cost) |
| You expect heavy concurrent tool fan-out from swarm + cron | **B** (avoid SQLite contention) — but verify load assumption first |

## Honest synthesis

- **Alt A is the most defensible PRD-spirit alternative.** PRD §6 explicitly says "persist tool attempts as learning events." A typed table with `args_hash`, `tool_schema_hash`, and a 5-bucket `outcome` column is the most direct translation of the PRD text. The ~0.3ms write tax per turn is invisible next to a 1-30s LLM round.

- **Alt B is a hot-path optimization solving a problem we may not have.** Aura's current load (single Telegram user + occasional cron) doesn't stress SQLite. Alt B's complexity (ring + LRU + flusher goroutine + drain semantics) is real and adds 4 new types to maintain. Only pick Alt B if there's evidence of SQLite contention TODAY.

- **Alt C is the strongest "ship less" alternative.** Honestly assesses that Aura already persists every tool call and every failure — Phase 6 may be plumbing, not a new subsystem. The trade-off: schema brittleness. If Phase-K adds `args_hash` / `idempotency_key` / `side_effect_class` columns, we'd add the `tool_attempts` table then anyway. Whether that's a problem depends on the Phase-K timeline.

## Recommended pick

Given Aura's pattern of preferring tight slices + the user's `feedback_one_module_per_slice` memory + the open question of Phase-K timeline, **Alt A is the safest default**. Schema cost is paid once and amortizes; Alt C's payload-bag JSON-extract path is a maintenance burden that compounds.

If the user states Phase-K is unlikely / >6 months out, switch to **Alt C** for the smaller LOC + zero-new-table footprint.

**Alt B is not recommended** unless there is concrete evidence of SQLite write contention under current load — and the audit shows there isn't (Phase-G's removal of `agent.Runner` simplified the write surface; current load is well within SQLite's single-writer capacity).
