---
phase: 06-kv-cache-builder
plan: 04
subsystem: cli
tags: [cli, kv-cache, cache-audit, cache-stats, invariant-gate, cobra-free, tabwriter]
requires:
  - "06-02 (internal/agent/prompt.PrefixHash index-set hash, agenttest.FakeClient.Requests clone)"
  - "06-03 (internal/cachemetrics.Store ListSince/AggregateSince + sqlc window queries)"
  - "runner.New/Deps + runner.Turn loop (Phase 4) — the real path the audit replays"
  - "cmd/aura/db.go subcommand dispatch + tabwriter style; main.go switch"
provides:
  - "aura cache-stats --since=<dur> — validated time-windowed cache_metrics read (SC#4 CLI side)"
  - "aura cache-audit (HIDDEN) — runtime-faithful 20-turn KV-prefix invariant gate (SC#1/SC#5)"
  - "cmd/aura in-memory Store fakes (memConvStore/memPauseStore/memIdentityStore/memCacheMetricStore) — Postgres-free Runner replay"
  - "scripts/fixtures/cache_invariant/turn-{01..20}.json deterministic replay fixtures (incl. tool-call turns)"
affects:
  - "06-05 (the bash CI smoke wrapper drives `go run ./cmd/aura cache-audit` + diffs the 20 hash lines)"
tech-stack:
  added: []
  patterns:
    - "Testable CLI core: <cmd>Main(ctx,args,out,errOut) int returning an exit code; os.Exit stays in the thin runEntry wrapper"
    - "Runtime-faithful invariant gate (D-04): replay the real runner.Turn loop, never a synthetic Build() hash"
    - "Read Requests[n].Messages[0] off the FakeClient's cloned snapshot (D-05); hash with PrefixHash({0}) (D-06a)"
    - "EX_USAGE (64) on flag-parse failure BEFORE any DB work (T-06-02)"
key-files:
  created:
    - cmd/aura/cache.go
    - cmd/aura/cache_stats.go
    - cmd/aura/cache_audit.go
    - cmd/aura/cachefakes.go
    - cmd/aura/cache_test.go
    - scripts/fixtures/cache_invariant/README.md
    - scripts/fixtures/cache_invariant/turn-01.json
    - scripts/fixtures/cache_invariant/turn-02.json
    - scripts/fixtures/cache_invariant/turn-03.json
    - scripts/fixtures/cache_invariant/turn-04.json
    - scripts/fixtures/cache_invariant/turn-05.json
    - scripts/fixtures/cache_invariant/turn-06.json
    - scripts/fixtures/cache_invariant/turn-07.json
    - scripts/fixtures/cache_invariant/turn-08.json
    - scripts/fixtures/cache_invariant/turn-09.json
    - scripts/fixtures/cache_invariant/turn-10.json
    - scripts/fixtures/cache_invariant/turn-11.json
    - scripts/fixtures/cache_invariant/turn-12.json
    - scripts/fixtures/cache_invariant/turn-13.json
    - scripts/fixtures/cache_invariant/turn-14.json
    - scripts/fixtures/cache_invariant/turn-15.json
    - scripts/fixtures/cache_invariant/turn-16.json
    - scripts/fixtures/cache_invariant/turn-17.json
    - scripts/fixtures/cache_invariant/turn-18.json
    - scripts/fixtures/cache_invariant/turn-19.json
    - scripts/fixtures/cache_invariant/turn-20.json
  modified:
    - cmd/aura/main.go
decisions:
  - "Auto-title worker disabled in the audit (memConvStore.Create sets TitleSet=true): the worker fires a SECOND LLM call with a DIFFERENT system prompt, which both consumes a scripted FakeClient turn and races client.LastRequest() — it perturbs the 1:1 turn→request mapping and is irrelevant to the prefix invariant. This was a real intermittent failure the audit caught during dev."
  - "Each fixture turn maps to ≥1 scripted FakeClient response: a tool-call round needs 2 (the tool call + a terminal text_response). The audit captures one representative request per Runner.Turn (the round's last) — messages[0] is invariant across every request in a round."
  - "The SC#5 negative proof drives the reportHashes seam directly with a hand-built poisoned request list (messages[0] drift at turn 3) — a clean Go-level proof of the exit-1 + 'messages[0] mutated at turn N' contract without fabricating a real prefix bug."
  - "--since accepts both --since=1h (equals) and --since 1h (space) via a local sinceValue helper (the shared flagValue only handles the space form); ≤0 windows are rejected as usage errors."
  - "Throwaway os.MkdirTemp run dir for the audit so tool-result spillover (the tool_search round) never lands in cwd."
metrics:
  duration: "~50m"
  completed: "2026-06-02"
  tasks: 2
  files: 27
---

# Phase 6 Plan 04: cache CLI surface (cache-stats + cache-audit) Summary

The operator-facing KV-cache CLI (D-06): `aura cache-stats --since=<dur>` is a real time-windowed read of `aura.cache_metrics` (validated via `time.ParseDuration`, tabwriter per-turn + summary, divide-by-zero-safe hit-rate), and the HIDDEN `aura cache-audit` is a runtime-faithful 20-turn `runner.Turn` replay against `agenttest.FakeClient` that reads `Requests[n].Messages[0]`, hashes each with `prompt.PrefixHash({0})`, prints `turn NN: <hex>`, asserts all 20 identical, and exits 0/1/2 — Postgres-free.

## What was built

**Task 1 — `cache-stats` + dispatch (commit 7cf6ab2d):**
- `cmd/aura/cache.go` — `runCacheStats` thin wrapper (os.Exit boundary over the testable `cacheStatsMain` core).
- `cmd/aura/cache_stats.go` — `--since` parse via `time.ParseDuration` (rejects unparseable / non-positive → stderr + exit 64 EX_USAGE BEFORE any DB work, T-06-02), windowed read via 06-03's `AggregateSince`/`ListSince`, `text/tabwriter` per-turn detail + a `TOTAL` summary line carrying the client-computed hit-rate guarded for `total_prompt==0` (prints `n/a`, never a SQL/float divide, T-06-03).
- `cmd/aura/main.go` — advertised `cache-stats` switch case.

**Task 2 — hidden `cache-audit` gate (commit 64436b9c):**
- `cmd/aura/cache_audit.go` — loads `turn-{01..20}.json`, scripts a `FakeClient`, builds an in-memory `Runner` (real `buildRegistry()`), loops `Runner.Turn` once per fixture, captures `client.LastRequest()` per turn, then `reportHashes` prints + asserts every `PrefixHash(messages[0],{0})` is identical. Exit `0` pass / `1` mutation (`messages[0] mutated at turn N — diff: <prev> vs <cur>`) / `2` fixture corrupt.
- `cmd/aura/cachefakes.go` — importable NON-test in-memory `memConvStore`/`memPauseStore`/`memIdentityStore`/`memCacheMetricStore` satisfying the four narrow `runner` Store interfaces (OQ2 / D-05); shared `rebuildMessages` helper.
- `cmd/aura/cache_test.go` — `--since` parse table (equals/space/minutes/missing/empty/bogus/negative/zero), hit-rate guard, the audit exit-code contract (0 all-equal / 1 injected mutation with the SC#5 wording / 2 missing+malformed+empty-user fixtures), and a fixtures-include-≥2-tool-call-rounds assertion.
- `scripts/fixtures/cache_invariant/turn-{01..20}.json` + `README.md` — deterministic; turn-05 scripts a `current_time` round, turn-12 a `tool_search` round (each followed by a terminal `text_response`).
- `cmd/aura/main.go` — HIDDEN `cache-audit` switch case (NOT in `usage()`).

## Verification evidence

- `go build ./...` + `go vet ./cmd/aura/` green.
- `go test ./cmd/aura/ -run TestCache` PASS; full `go test ./cmd/aura/` PASS; `go test -race ./cmd/aura/` PASS (BASH_ENV toolchain).
- `golangci-lint run ./cmd/aura/` — 0 issues.
- `go run ./cmd/aura cache-audit` → exactly 20 `turn NN: <hex>` lines, all identical, exit 0, with the DB stack DOWN (Postgres-free). Ran 8× under the test binary — 8/8 deterministic after the auto-title fix.
- `go run ./cmd/aura cache-stats --since=bogus` → stderr parse error, exit 64. `--since` missing → exit 64.
- `go run ./cmd/aura` usage output contains `cache-audit` 0 times (hidden).
- All touched files ≤ 600 LOC (cache_audit.go 252, cachefakes.go 281).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Auto-title worker poisoned the audit's request capture (non-deterministic exit 1)**
- **Found during:** Task 2 (running the audit under `go test`, intermittent `messages[0] mutated at turn N`).
- **Issue:** the `Runner`'s auto-title worker fires after seq≥3, making a SECOND LLM call with a DIFFERENT system prompt ("You generate a concise 4-6 word title…"). That call is captured in `FakeClient.Requests` and, running in a background goroutine, sometimes won the `LastRequest()` race — so a chat turn's hash was occasionally read off the title call's `messages[0]`. It also silently consumed a scripted FakeClient turn.
- **Fix:** `memConvStore.Create` sets `TitleSet=true` so `maybeAutoTitle` short-circuits and the worker never fires. The audit now drives only the chat path. Made deterministic (8/8 runs).
- **Files:** cmd/aura/cachefakes.go · **Commit:** 64436b9c

**2. [Rule 3 - Blocking] `--since=1h` (equals form) was not parsed by the shared `flagValue`**
- **Found during:** Task 1 (acceptance asserts `--since=bogus`; the shared `flagValue` only handles `--since 1h`).
- **Fix:** added a local `sinceValue` helper accepting both `--since=<dur>` and `--since <dur>` (scope-controlled; did not change the shared `flagValue`).
- **Files:** cmd/aura/cache_stats.go · **Commit:** 7cf6ab2d

**3. [Rule 1 - Bug] errcheck/dupl lint on the new CLI cores**
- **Found during:** Task 2 (`golangci-lint`; the Task-1 pre-commit hook only runs gofmt+vet+file-size, so errcheck slipped through on cache_stats.go).
- **Fix:** `_, _ =` on all `fmt.Fprint*`-to-`io.Writer` calls (mirrors db.go's tabwriter style); extracted the duplicated message-reconstruction loop into a shared `rebuildMessages` helper, clearing the dupl flag against `cmdfakes_test.go`.
- **Files:** cmd/aura/cache_stats.go, cmd/aura/cache_audit.go, cmd/aura/cachefakes.go · **Commit:** 64436b9c

**4. [Rule 2 - Critical] Audit polluted cwd with a sidecar run dir**
- **Found during:** Task 2 (the `tool_search` round spilled a result file into `./conversations/...` because `RunDir` was empty).
- **Fix:** the audit creates a throwaway `os.MkdirTemp` run dir and `RemoveAll`s it on return — no cwd pollution.
- **Files:** cmd/aura/cache_audit.go · **Commit:** 64436b9c

## Threat surface

T-06-02 (mitigated): `--since` validated by `time.ParseDuration` + non-positive rejection before any query; underlying read is 06-03's sqlc-parameterized window. T-06-01 (mitigated): the runtime-faithful replay asserts the `PrefixHash(messages[0])` invariant on the REAL `runner.Turn` loop, proven not-silently-green by the SC#5 Go-level negative test. T-06-03 (mitigated): `cache-stats` prints token counts + cost + hit-rate only — no message content, no keys. No new trust boundaries introduced beyond the plan's threat model.

## Self-Check: PASSED
