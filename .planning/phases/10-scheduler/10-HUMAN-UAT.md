---
status: passed
phase: 10-scheduler
source: [10-VERIFICATION.md]
started: 2026-06-04T18:42:24Z
updated: 2026-06-04T19:15:00Z
---

## Current Test

[all tests passed — post-fix live re-check complete]

## Tests

### 1. SC#1 live once-per-window firing

expected: `aura serve` running against the live stack fires a `*/5 * * * *` reminder exactly once per window (`aura task runs` shows one run per window, no double-exec). Already executed live during Gate-3 (2 fires/2 windows, post-fix of the 94-fire bug) — pre-dating the 7 review-fix commits.
result: passed — post-fix live re-check 2026-06-04: `*/1 * * * *` reminder over ~3.5min serve fired 4 windows, DB ground truth (aura.agent_job_runs grouped by minute) = exactly 1 run/window, MAX_PER_WINDOW=1, no duplicates. Also re-proved CR-01 boot catch-up: backdated next_run_at 7min → restart serve → exactly 1 catch-up run row with missed_since=18:55:53 stamped (the load-bearing fix), dispatched once.

### 2. SC#2 chaos script operator gate

expected: `scripts/scheduler_chaos.sh` (3 workers, one partitioned 60s) exits 0 with survivor pickup, `completed=1` / `distinct=1`, no duplicate side-effects. Already executed live during Gate-3 — pre-dating the review-fix commits.
result: passed — post-fix live re-check 2026-06-04 (WSL): `completed runs = 1, distinct completion hashes = 1`, survivor re-claimed the partitioned worker's task, no duplicate side-effect, exit 0. Assertions are DB count queries on aura.agent_job_runs, not stdout.

### 3. SC#3 backup artifact host read-back

expected: nightly `backup_postgres`/`backup_neo4j` produce dump artifacts readable on the host side of the `$AURA_BACKUP_DIR` bind-mount; valid pg archive proven live in Gate-3 (29069 bytes). WR-04 fix added the `os.Stat` host guard.
result: passed — post-fix live re-check 2026-06-04: with AURA_BACKUP_DIR bind-mounted host==container (CAP-02 ops precondition), `backup_postgres --at` fired via serve, the WR-04 os.Stat host guard passed (run row status=completed, "backup postgres ok"), and the dump materialized ON THE HOST: 29301-byte custom-format PGDMP archive, `pg_restore --list` shows 81 TOC entries / 11 TABLE DATA (agent_job_runs, cache_metrics, capability_grants, ...). Container restored to canonical compose state after.

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
