# Phase 6 Alt C — CONVERSATION-ARCHIVE PIGGYBACK / NO NEW TABLE

**Date:** 2026-05-15 · **Status:** alternative architecture proposal · **Stance:** the "ship less" architecture.

## 1. One-sentence pitch

A read-only SQL view + small briefer that turns Aura's existing `conversations` + `run_events` rows into the tool-experience loop with zero new tables.

## 2. Story breakdown (6 stories)

**US-C01** — Plumb `classifyToolError` label into `run_events.payload_json`. Today `internal/chat/hub.go:435-439` writes literal `"tool_failed"`. Capture class from executor; payload key `error_class_v1`. No schema change.

**US-C02** — Add `Outcome` 5-bucket mapper. `internal/agent/tools/registry/observation.go`: pure function `OutcomeOf(class string) string`. No struct, no state.

**US-C03** — Add one composite index on `run_events`. Migration v10: `CREATE INDEX idx_run_events_tool_failure ON run_events(type, json_extract(payload_json, '$.tool'), json_extract(payload_json, '$.success'), created_at)` + `CREATE INDEX idx_conv_tcid ON conversations(tool_call_id)`. Two indexes, no tables.

**US-C04** — Read layer `internal/agent/tools/experience/`. `Repo.Recent(ctx, threadID, toolName, n)` + `Repo.Aggregate(ctx, lookback)`. SELECT against `run_events` + `conversations`. No writes.

**US-C05** — Pre-LLM briefer in `governance.Apply`. Mirrors `appendValidationNudge` shape. No injection when empty.

**US-C06** — `/api/tool-warnings` admin endpoint + closure docs.

Per-(tool, error-class) budget: in-memory `map[string]int` keyed on `tool+":"+class` reset per run — same pattern as `seenToolCalls` at `loop.go:218-221`. Not persisted.

## 3. Query layer

**(a) Recent K failures of tool X in last L runs (per-thread briefer fetch):**

```sql
SELECT re.run_id, re.created_at,
       json_extract(re.payload_json, '$.error_class_v1') AS class,
       json_extract(re.payload_json, '$.tool_call_id')   AS tcid,
       c.content AS error_text
FROM run_events re
JOIN runs r ON r.id = re.run_id
LEFT JOIN conversations c
  ON c.tool_call_id = json_extract(re.payload_json, '$.tool_call_id')
 AND c.role = 'tool'
WHERE re.type = 'tool_end'
  AND json_extract(re.payload_json, '$.tool')    = :tool_name
  AND json_extract(re.payload_json, '$.success') = 0
  AND r.thread_id = :thread_id
ORDER BY re.created_at DESC
LIMIT :K;
```

**(b) Aggregate failure counts for `/api/tool-warnings`:**

```sql
SELECT json_extract(payload_json, '$.tool')           AS tool,
       json_extract(payload_json, '$.error_class_v1') AS class,
       COUNT(*) AS n, MAX(created_at) AS last_seen
FROM run_events
WHERE type = 'tool_end'
  AND json_extract(payload_json, '$.success') = 0
  AND created_at >= :since
GROUP BY tool, class
ORDER BY n DESC;
```

**(c) Per-tool briefer fetch:** loop `:tool_name` over the pool (M=6 calls × ≤3 rows each).

## 4. Hot-path cost

**Writes:** zero new. The existing `archive_turns.go` + `hub.go` writes are unchanged.

**Briefer reads (M=6, 3 rows each):** with `idx_run_events_tool_failure` ~1ms at 100k rows; without the index ~100ms full scan. **The single new index is load-bearing.**

**Aggregate query (admin endpoint, 7d lookback):** 7-day window of ~50k tool_end rows: ≤50ms cold, ≤10ms warm. Operator-demand only.

## 5. Failure modes

- **`CONV_ARCHIVE_ENABLED=false`** → `conversations` empty. Briefer falls back to `(tool, class, created_at)` from `run_events` only. Aggregate **unaffected**.
- **Malformed `tool_calls` JSON** → `archive_turns.go:67-70` already warn-logs. Briefer joins on `role='tool'` rows, doesn't read assistant JSON.
- **`run_events` lacks fine error_class** (today's reality — only "tool_failed") → US-C01 fixes by plumbing `error_class_v1`. Legacy rows treated as fallback `fatal`.
- **Cross-thread aggregation:** query (b) intentionally cross-thread for operator view; query (a) filters via `runs.thread_id`.

## 6. Pros vs Alt A and Alt B

| Dimension | Alt C | Alt A | Alt B |
|---|---|---|---|
| Migrations | 1 index (no table) | 1 table + indexes | 1 table |
| New packages | 1 (`experience`) | 2 (`attempts`, supervisor) | 1 (`observe`) |
| LOC | ~500 | ~900-1200 | ~1310 |
| Cross-run learning across restarts | Yes (SQLite) | Yes | No (memory lost) |
| Hot-path write overhead | 0 new | 1 INSERT/call | 0 |
| Schema evolution | Painful (JSON bag) | Easy | Easy |
| CONV_ARCHIVE_ENABLED dep | Soft (degrades) | None | None |
| Future-proof Phase-K | Bad (refactor later) | Good | Good |

**Honest take:** strongest "ship less" architecture, weakest for schema evolution. If Phase-K is ≥6 months out, Alt C wins on time-to-value. If next quarter, Alt A wins on total cost.

## 7. Risks + mitigations

1. **Hard dep on archive being on for rich briefer.** Mitigation: degrade to class-only capsule when off; locked in US-C05 tests.
2. **JSON-extract cost unpredictable past ~1M rows.** Mitigation: benchmark at story acceptance against 500k-row synthetic data; escalate to Alt A if briefer >10ms p95.
3. **Schema brittleness — payload bag.** Mitigation: version key as `error_class_v1` so we can introduce `_v2` without breaking readers.

## 8. Estimated LOC

| Story | LOC |
|---|---|
| US-C01 plumb error_class_v1 | ~80 |
| US-C02 5-bucket mapper | ~60 |
| US-C03 migration v10 indexes | ~40 |
| US-C04 experience/repo.go | ~120 |
| US-C05 briefer in governance | ~120 |
| US-C06 operator API + closure | ~80 |
| **Total** | **~500 LOC** |

Roughly half of Alt A. Maintenance footprint a year from now: one index + one ~120-LOC read-only package.
