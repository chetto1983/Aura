---
phase: 06-kv-cache-builder
plan: 03
subsystem: persistence
tags: [postgres, sqlc, migration, kv-cache, metrics, runner]
requires:
  - "06-01 (Phase-6 amendments: cache_metrics persistence overrides PRD OQ2)"
  - "aura.conversations table (migration 0005, FK target)"
  - "runner.Deps/Runner/New PauseStore threading pattern (Phase 4)"
provides:
  - "aura.cache_metrics table (migration 0007, append-only, role-separated)"
  - "internal/cachemetrics.Store (Insert + ListSince + AggregateSince)"
  - "runner.CacheMetricStore narrow interface + persist seam (D-02/D-02a)"
  - "sqlc InsertCacheMetric / ListCacheMetricsSince / AggregateCacheMetricsSince"
affects:
  - "06-04 (aura cache-stats --since consumes ListSince/AggregateSince)"
tech-stack:
  added: []
  patterns:
    - "Narrow consumer-side Store interface (D-A2-02) mirroring PauseStore"
    - "Append-only role separation: aura_app SELECT,INSERT only (T-06-04)"
    - "Parameterized window query sqlc.arg(since)::timestamptz (T-06-02)"
key-files:
  created:
    - internal/db/migrations/0007_cache_metrics.up.sql
    - internal/db/migrations/0007_cache_metrics.down.sql
    - internal/db/queries/cache_metrics.sql
    - internal/db/sqlc/cache_metrics.sql.go
    - internal/cachemetrics/store.go
    - internal/cachemetrics/store_helpers.go
    - internal/cachemetrics/store_helpers_test.go
    - internal/db/cache_metrics_integration_test.go
    - internal/runner/runner_cachemetric_test.go
  modified:
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/runner/interfaces.go
    - internal/runner/runner.go
    - internal/runner/runner_persist.go
    - internal/runner/fakes_test.go
    - internal/runner/runner_test.go
    - internal/db/db_test.go
    - cmd/aura/chat.go
    - cmd/aura/chat_test.go
    - cmd/aura/cmdfakes_test.go
decisions:
  - "SQL-side aggregation (count + coalesce(sum)) — the hit-rate ratio + divide-by-zero guard stay client-side in 06-04"
  - "nil CacheMetricStore fails loud in the persist seam (no silent skip) — prod composition root MUST inject a Store (no-skip discipline)"
  - "Store.Insert takes the generated sqlc.InsertCacheMetricParams (per plan); NewInsertParams exported so the runner builds params without duplicating pgtype conversion"
metrics:
  duration: "~35m"
  completed: "2026-06-02"
  tasks: 2
  files: 19
---

# Phase 6 Plan 03: cache_metrics persistence Summary

Persist per-turn KV-cache metrics to Postgres (D-02): migration `0007` adds an append-only `aura.cache_metrics` table (mirroring `0005_conversations` conventions + `aura_app` SELECT/INSERT-only role separation), backed by three parameterized sqlc queries (INSERT + `--since` window + aggregate), a thin `internal/cachemetrics.Store`, and a sibling INSERT in the existing `runner_persist.go:persistAssistantAnswer` seam — reusing the already-computed `llm.Usage` + cost (D-02a, no wire-path touch) through a narrow `CacheMetricStore` interface threaded like `PauseStore`.

## What shipped

- **Migration 0007** (`0007_cache_metrics.up/down.sql`): `aura.cache_metrics (conversation_id uuid FK ON DELETE CASCADE, seq, ts timestamptz DEFAULT now(), prompt_tokens, cached_tokens, cost_usd numeric(10,4), PK(conversation_id, seq))` + a plain `ts DESC` index (tx-safe, Pitfall 4 — no concurrent build inside the implicit migration tx). Grants: `aura_app` SELECT,INSERT only; `aura_migrate` ALL (T-06-04 append-only role separation).
- **sqlc queries** (`cache_metrics.sql` → regenerated `cache_metrics.sql.go` + `models.go` + `querier.go`): `InsertCacheMetric :exec`, `ListCacheMetricsSince :many` (`ts >= sqlc.arg(since)::timestamptz ORDER BY ts ASC`), `AggregateCacheMetricsSince :one` (`count(*)` + `coalesce(sum(...),0)`). Parameterized window — no string concat (T-06-02).
- **`internal/cachemetrics.Store`**: `New(pool)`, `Insert(sqlc.InsertCacheMetricParams)`, `ListSince(time.Time)`, `AggregateSince(time.Time)`. Helpers mirror `conversations` (`numericFromFloat`/`floatFromNumeric`/`uuidFrom`) plus `anyInt64`/`anyNumericFloat` to coerce the sqlc `interface{}` sum results across PG decode shapes.
- **Runner wiring**: `CacheMetricStore` narrow Insert-only interface in `interfaces.go`; threaded through `Deps`/`Runner`/`New` exactly like `Pause PauseStore`. `persistAssistantAnswer` now writes one metric row per completed assistant turn via `persistCacheMetric`, reusing the existing `u`/`cost`. Composition root `cmd/aura/chat.go` injects `cachemetrics.New(pool)`.

## Verification evidence

- `go build ./...` + `go vet ./...` green.
- `go test -race ./internal/runner/ ./internal/cachemetrics/ ./cmd/aura/` all pass; cachemetrics unit tier coverage 70.9% (Store CRUD covered by the integration tier).
- `golangci-lint run` on touched packages: **0 issues**. All touched files < 600 LOC (largest: runner.go 284).
- **Live db_integration run** (Postgres up, composed DSNs): `go test -tags db_integration -race -run TestCacheMetrics ./internal/db/` → `TestCacheMetrics_WindowAndAggregate PASS (0.27s)` + `TestCacheMetrics_StoreInsert PASS (0.11s)`. Non-trivial runtime + real migration through 0007 + real INSERT/window/aggregate — not a skip-as-green tell. Full `db_integration` suite green (3.0s), including the fresh-DB migrate-count assertion (bumped 6→7) and the `aura_app` DDL-denied role-separation test.
- `grep CONCURRENTLY 0007_cache_metrics.up.sql` → no match. `aura_app` grant is exactly `SELECT, INSERT`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fresh-DB migration-count assertion was stale after adding 0007**
- **Found during:** Task 2 (running the full db_integration suite)
- **Issue:** `internal/db/db_test.go:TestMigrate_Phase4_AppliesAndSeeds` asserted `const phase4Migrations = 6` (0001..0006) on a fresh DB; adding migration 0007 makes a fresh migrate apply 7, so the test would fail.
- **Fix:** Bumped the constant to `shippedMigrations = 7` and updated the message/comment to `0001..0007 (0007 = Phase-6 cache_metrics)`. The idempotency re-run (0 newly applied) and role-separation assertions are unchanged.
- **Files modified:** internal/db/db_test.go
- **Commit:** 4a555457

### Design choices recorded (within plan latitude)

- **nil-store fails loud, not skip:** The plan offered "guard against nil OR require a fake (prefer requiring)". Chosen: `persistCacheMetric` returns a loud error on `nil cacheMetrics` so the prod path can never silently drop a metric (no-skip discipline). All test/composition call sites inject a Store/fake.
- **Insert signature:** Kept `Store.Insert(sqlc.InsertCacheMetricParams)` per the plan's exact interface spec, and exported `cachemetrics.NewInsertParams(MetricParams)` so the runner builds params without duplicating the pgtype/uuid/numeric conversion (D-A4-01 un-duplication).

## Threat surface

No new surface beyond the plan's `<threat_model>`. T-06-02 (parameterized `since`) and T-06-04 (append-only role separation) are enforced in the migration + queries; T-06-03 (no message content / no API key in cache_metrics) holds — the table stores token counts + cost only.

## Commits

- b3590df5: feat(06-03): add cache_metrics migration 0007 + parameterized sqlc queries
- 4a555457: feat(06-03): persist per-turn cache metrics via narrow CacheMetricStore seam
