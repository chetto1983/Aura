# Phase 29 — Deferred Items (out-of-scope discoveries)

Items found during plan execution that are OUTSIDE the touched surface (CLAUDE.md
scope boundary: only auto-fix issues DIRECTLY caused by the current task's changes).
Logged, NOT fixed.

## 29-05 (Gate-3 close)

### D-29-05-1 — `internal/cron` migration round-trip bound is stale (off-by-one at head=0022)

- **Where:** `internal/cron/dispatch_integration_test.go:328`
  (`TestDispatchPendingNotificationIdentityRoundTrip`).
- **Symptom:** `identity_id still present after 8 down steps — 0014's down did not drop it`.
  The test walks the migration DOWN one step at a time (bounded at **8**) waiting for
  0014's down to drop `pending_notifications.identity_id`. With the schema head now at
  **0022** (Phase-29 added 0022_mcp_audit on top of the 0015-0021 stack), reaching the
  version BELOW 0014 (where the column is dropped) needs **9** down steps (22→13), so the
  hardcoded bound of 8 stops one step short at version 14 (where 0014 is still applied and
  the column still exists).
- **Why deferred:** `internal/cron` is OUTSIDE this plan's touched surface (29-05 touches
  `internal/agui`, `internal/mcp/manager`, `internal/skills`, `web/`, `internal/webui/dist`).
  I did NOT modify the cron test or migration 0014 (verified: `git diff 5890a221 HEAD`
  lists no `cron`/`0014` files). The bound was correct when head was ~0021; the new
  0022 migration shifted the head→0014 distance by one and the bound was not bumped. This
  is a pre-existing head-awareness bug in an unrelated package, not a regression from 29-05.
- **Fix (for the owner of internal/cron / a follow-up):** raise the bound from `8` to a
  head-derived value (e.g. `current_head - 13`) or to a safe constant (e.g. 12) so the loop
  reaches the version below 0014 regardless of how many migrations sit on top. The loop is
  already "head-aware" in shape (walk-down-until-dropped); only the upper bound is stale.
- **Impact on 29-05:** this is the ONLY failure in the `make coverage` `-p 1` run; it is a
  test-logic bound, not a coverage shortfall. The three packages 29-05 touched each clear
  the 85% floor in isolation on the live stack (agui 85.2%, mcp/manager 94.8%, skills 86.9%),
  and the owned-surface aggregate is ~90% (CLAUDE.md baseline). A clean `make coverage`
  green requires this one-line bound bump in internal/cron first.
