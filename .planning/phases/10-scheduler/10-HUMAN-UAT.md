---
status: partial
phase: 10-scheduler
source: [10-VERIFICATION.md]
started: 2026-06-04T18:42:24Z
updated: 2026-06-04T18:42:24Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. SC#1 live once-per-window firing

expected: `aura serve` running against the live stack fires a `*/5 * * * *` reminder exactly once per window (`aura task runs` shows one run per window, no double-exec). Already executed live during Gate-3 (2 fires/2 windows, post-fix of the 94-fire bug) — pre-dating the 7 review-fix commits.
result: [pending]

### 2. SC#2 chaos script operator gate

expected: `scripts/scheduler_chaos.sh` (3 workers, one partitioned 60s) exits 0 with survivor pickup, `completed=1` / `distinct=1`, no duplicate side-effects. Already executed live during Gate-3 — pre-dating the review-fix commits.
result: [pending]

### 3. SC#3 backup artifact host read-back

expected: nightly `backup_postgres`/`backup_neo4j` produce dump artifacts readable on the host side of the `$AURA_BACKUP_DIR` bind-mount; valid pg archive proven live in Gate-3 (29069 bytes). WR-04 fix added the `os.Stat` host guard.
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
