# Phase 6 Alt A — STORAGE-FIRST Tool Experience Loop

**Date:** 2026-05-15 · **Status:** SELECTED architecture (with MCP considerations §9)

## 1. One-sentence pitch

The `tool_attempts` SQLite table is the synchronous source of truth — every observation writes before the loop advances, every briefing reads back from disk.

## 2. Story breakdown (Phase-J Ralph queue, 7 commits)

**US-J01** — `ToolObservation` contract + 10→5 bucket classifier. Add `internal/agent/tools/registry/observation.go` with `Outcome` enum (`ok`/`recoverable`/`blocked`/`fatal`/`cancelled`) and `BucketOf(class string) Outcome` mapping the 10 labels at `internal/agent/tools/registry/error.go:13-41` into the 5 buckets. `recoverable = {validation, not_found, rate_limited, io}`; `blocked = {permission, blocked}`; `fatal = {error, timeout}`; `cancelled = {cancelled}`. Plumb a `ToolObservation{RunID, ToolName, AttemptN, Outcome, Class, Reason, ArgsHash, ArgKeys, ToolSchemaHash, StartedAt, EndedAt, ErrorRedacted}` through `executor.executeOneTool` (`internal/agent/executor.go:123-151`).

**US-J02** — Migration v10 `tool_attempts` + synchronous `Repo.Record`. New `internal/agent/tools/attempts/` package, single hot-path method `Record(ctx, ToolObservation) error` with `INSERT ... RETURNING id` inside `BEGIN IMMEDIATE`. SHA-256 hashing of args at write time; no raw value ever stored.

**US-J03** — Wire `Record` into executor hot path. Modify `agentExecutor.executeOneTool` and `ExecuteToolCalls`. On Record failure, log + continue — never block user-visible result.

**US-J04** — Pre-LLM briefer reads from `tool_attempts`. New governance-style step before `client.Chat`. For each tool in the per-turn pool, query `SELECT outcome, class, reason FROM tool_attempts WHERE run_id IN (recent_run_ids_for_thread, limit 5) AND tool_name = ? AND outcome IN ('recoverable','blocked','fatal') ORDER BY ended_at DESC LIMIT 3`. Render as synthetic system message; cap ≤ 800 chars total.

**US-J05** — Per-(tool,class) retry budget enforced via `tool_attempts` count. `Options.RetryBudgets map[string]int` default `{recoverable: 2, blocked: 0, fatal: 0, cancelled: 0}`. Before executor dispatch, `SELECT COUNT(*) WHERE run_id=? AND tool_name=? AND outcome=?`. If over budget, divert with stub error + record refusal as a row.

**US-J06** — `tool_warnings` aggregation view + operator API. `CREATE VIEW tool_warnings AS SELECT tool_name, class, COUNT(*) AS n, MAX(ended_at) AS last_seen FROM tool_attempts WHERE outcome IN ('recoverable','blocked','fatal') AND ended_at > datetime('now','-7 days') GROUP BY tool_name, class`. New endpoint `GET /api/tool-warnings` admin-gated.

**US-J07** — Phase 6 closure docs + benchmark probes.

## 3. Schema

```sql
CREATE TABLE IF NOT EXISTS tool_attempts (
  id                TEXT PRIMARY KEY,              -- ULID
  run_id            TEXT NOT NULL,                 -- FK runs.id
  tool_call_id      TEXT NOT NULL DEFAULT '',     -- joins conversations.tool_call_id
  attempt_n         INTEGER NOT NULL DEFAULT 1,
  tool_name         TEXT NOT NULL,                 -- includes `mcp_<server>_<tool>` for MCP; NO FK (MCP servers come/go)
  tool_kind         TEXT NOT NULL DEFAULT 'native', -- 'native' | 'mcp' (derived from tool_name prefix at write time)
  tool_schema_hash  TEXT NOT NULL DEFAULT '',     -- SHA-256 of ToolDefinition.Parameters — survives MCP schema rev
  outcome           TEXT NOT NULL,                 -- enum: ok|recoverable|blocked|fatal|cancelled
  class             TEXT NOT NULL DEFAULT '',     -- classifyToolError 10-label vocab
  reason            TEXT NOT NULL DEFAULT '',     -- closed-set refusal/retry-skip code
  args_hash         TEXT NOT NULL DEFAULT '',     -- SHA-256(canonical-json(args))
  arg_keys_json     TEXT NOT NULL DEFAULT '[]',   -- JSON array of KEY NAMES only
  error_redacted    TEXT NOT NULL DEFAULT '',
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  started_at        TEXT NOT NULL,                 -- RFC3339Nano
  ended_at          TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);

CREATE INDEX idx_tool_attempts_run_tool     ON tool_attempts(run_id, tool_name, ended_at DESC);
CREATE INDEX idx_tool_attempts_tool_outcome ON tool_attempts(tool_name, outcome, ended_at DESC);
CREATE INDEX idx_tool_attempts_run_outcome  ON tool_attempts(run_id, outcome);
CREATE INDEX idx_tool_attempts_signature    ON tool_attempts(tool_name, args_hash, outcome);

CREATE VIEW tool_warnings AS
  SELECT tool_name, class, COUNT(*) AS n, MAX(ended_at) AS last_seen,
         SUM(CASE WHEN outcome='recoverable' THEN 1 ELSE 0 END) AS n_recoverable,
         SUM(CASE WHEN outcome='blocked'     THEN 1 ELSE 0 END) AS n_blocked,
         SUM(CASE WHEN outcome='fatal'       THEN 1 ELSE 0 END) AS n_fatal
  FROM tool_attempts
  WHERE outcome IN ('recoverable','blocked','fatal') AND ended_at > datetime('now','-7 days')
  GROUP BY tool_name, class;
```

## 4. Hot-path cost

- **Writes, N=8 tool calls:** 8 synchronous `INSERT INTO tool_attempts`. Each ~400 bytes. SQLite WAL: ~30µs uncontended → 8 inserts = ~240µs added latency per turn. Versus an LLM round (1-30s), this is < 0.01% of turn budget.
- **Reads, M=6 in-pool tools:** 1 single SELECT with `idx_tool_attempts_run_tool`. ~6µs at 10k-row scale. Then in-Go group-by tool_name, take top 3.
- **Budget checks:** up to 8 per turn × ~3µs each = 24µs. Covered by `idx_tool_attempts_run_outcome`.
- **Per-turn added latency: ~270µs writes + ~30µs reads = ~0.3ms.**

## 5. Failure modes

- **SQLite lock contention:** WAL mode + `database/sql` pool serializes. Worst case ~1ms wait under high parallel-tool fan-out. Mitigation: `Repo.Record` retries 3× with 10ms backoff on `SQLITE_BUSY`; on persistent failure, log + drop row, do NOT block tool result.
- **Disk full:** `INSERT` returns `SQLITE_FULL`. Repo logs + continues. Briefer next turn sees fewer rows = graceful degradation.
- **Crash mid-write:** `BEGIN IMMEDIATE` / `COMMIT` — atomic. No torn-write window.
- **Pre-deploy legacy data:** `run_events.payload_json` has only "tool_failed" string; NOT migrated. Briefer starts fresh from `tool_attempts` and gains signal over time.

## 6. Pros vs Alt B and Alt C

**vs Alt B (in-memory):** WIN durability (zero loss on crash), cross-run learning works from first restart, operator dashboard is one-line view, `args_hash` enables cross-run signature detection. LOSE ~0.3ms added latency per turn (Alt B is zero).

**vs Alt C (conversation-archive piggyback):** WIN typed columns indexable (Alt C must JSON-parse), privacy boundary cleaner, independent of `CONV_ARCHIVE_ENABLED`. LOSE Alt C reuses existing infrastructure — zero new tables.

## 7. Risks + mitigations

1. **Write contention under swarm fan-out.** Mitigation: `_busy_timeout=5000`; batch in 50ms windows if needed.
2. **Hash canonicalisation:** sorted-keys canonical JSON; test both orderings.
3. **Briefer prompt bloat:** hard cap LIMIT 3 per tool × 8 tools × ≤60 chars = ≤1.5kB; skip injection when empty.

## 8. Estimated LOC

| Story | Files | LOC impl | LOC tests |
|---|---|---|---|
| US-J01 ToolObservation | observation.go (new) | 60 | 80 |
| US-J02 Migration + Repo | migrations.go, attempts/repo.go + sql.go (new) | 180 | 150 |
| US-J03 Executor wiring | executor.go (~20 LOC delta) | 25 | 60 |
| US-J04 Pre-LLM briefer | loop.go, governance.go, attempts/briefer.go (new) | 110 | 130 |
| US-J05 Retry budget | loop.go, attempts/budget.go (new) | 80 | 100 |
| US-J06 Operator API + view | api/tool_warnings.go (new) | 90 | 70 |
| US-J07 Closure docs | docs only | 0 | 30 |
| **Total** | **8 files, 1 migration** | **~545** | **~620** |

## 9. MCP considerations (added 2026-05-15 per user directive "fai attenzione agli MCP futuri")

Phase-I added MCP-aligned `readOnlyHint/destructiveHint/idempotentHint/openWorldHint` to `ToolDefinition` so native + MCP-imported tools share one vocabulary. Phase-J must preserve that bridge.

### 9.1 Schema invariants for MCP

- `tool_name TEXT NOT NULL` has **NO FK constraint** to a registry table. MCP servers come and go (per `internal/agent/tools/registry/mcp.go:26-27` the format is `mcp_<server>_<tool>`); the briefer must handle history rows that reference a tool no longer registered. This is the same shape as `runs.thread_id` (no FK to a threads table).
- `tool_kind TEXT NOT NULL DEFAULT 'native'` is a derived column written at row insertion time: `if strings.HasPrefix(tool_name, "mcp_") { tool_kind = "mcp" }`. Operators see MCP failures separately in `/api/tool-warnings` aggregations — useful when a single MCP server is misbehaving.
- `tool_schema_hash` is load-bearing for MCP: when a server pushes a new tool schema version, the hash changes, and the briefer correctly stops applying lessons from the old schema. **No backfill needed** — the column starts populated on day one.

### 9.2 US-J04 briefer must skip stale-tool history

When the briefer fetches `Repo.Recent(tool_name)` and the tool is **no longer registered** (MCP server offline), the briefer:
1. Still surfaces the history in the synthetic system message (helps the model understand "this tool used to fail with X — now unavailable").
2. Adds a `(tool currently unavailable)` annotation so the model doesn't try to call it.
3. Does NOT crash. Test: drop the MCP `internal/agent/tools/registry/mcp.go` registration mid-test, call briefer, assert it returns without panic.

### 9.3 US-J05 retry budget interaction with MCP server-side retries

MCP servers often implement their own internal retry (e.g. fetching an HTTP resource). Aura's retry budget at `RetryBudgets[recoverable] = 2` would double-count if it doesn't know.

**Decision:** Aura's budget is **outer-loop only** — it counts the number of times the LLM proposed `mcp_X_tool` and got a recoverable result. MCP server internal retries are invisible to Aura and don't count. This matches the existing `internal/llm/retry.go` behavior where the LLM client's retry budget doesn't see provider-side retries.

**Optional future hook (NOT in scope for US-J05):** if MCP servers in 2026+ start returning a structured `retry_count_internal` field in their error result, the executor can pass that through to `ToolObservation.AttemptN` and the budget query can sum across attempts. Schema is forward-compatible (the `attempt_n` column already exists).

### 9.4 US-J06 `/api/tool-warnings` MCP grouping

When the dashboard renders the aggregate response, it can group by `tool_kind` so an operator sees:

```json
{ "kind": "native", "tool": "web_fetch",  "class": "io",       "n": 12, ... },
{ "kind": "mcp",    "tool": "mcp_github_search", "class": "rate_limited", "n": 5, ... }
```

The view definition stays the same — the column already partitions the data.

### 9.5 What we explicitly DON'T do for MCP

- **No tool-name normalization.** `mcp_github_search` and `mcp_GitHub_search` are stored as distinct tools. If an operator renames an MCP server, history is segmented by the new name (correct behavior — different server, different reality).
- **No cross-server schema sharing.** Two MCP servers both exposing a `search` tool produce two distinct rows in `tool_warnings` (`mcp_server1_search`, `mcp_server2_search`). Operators see each server's failure profile separately. This is what the `mcp_<server>_<tool>` naming convention at `internal/agent/tools/registry/mcp.go:27` already promises.
- **No automatic MCP server health check.** That's an `/api/mcp` concern, not `tool_attempts`. Keep concerns separate.

### 9.6 Test additions for US-J02 / US-J04 / US-J06

- US-J02: roundtrip test with `tool_name = "mcp_foo_bar"` confirms `tool_kind` is derived as `"mcp"` at write time.
- US-J04: briefer test with one MCP-named tool in history but the registry empty — assert the synthetic message includes the "(tool currently unavailable)" annotation.
- US-J06: aggregate test with mixed native + MCP failures, assert the response includes both `tool_kind` values.

