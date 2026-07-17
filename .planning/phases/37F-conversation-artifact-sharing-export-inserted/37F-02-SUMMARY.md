---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 02
subsystem: database
tags: [postgres, golang-migrate, sqlc, config, envutil, share-links, audit-ledger]

# Dependency graph
requires:
  - phase: 37F-01
    provides: "PRD-amendment WEBSHARE-01..04 + ADR 0039 (sharing vs identity isolation) authorizing this schema"
provides:
  - "aura.shared_links table (tier-shape CHECK, unique partial token_hash index, owner/conversation/expiry indexes)"
  - "aura.share_audit append-only ledger (SELECT+INSERT-only grant for aura_app)"
  - "scheduler_tasks.kind widened to admit 'share_expiry_sweep'"
  - "config.ShareConfig (AURA_SHARE_PUBLIC_ENABLED default false, AURA_SHARE_MAX_EXPIRY_DAYS default 90)"
  - "migrate_0040_integration_test.go — up/down/up round-trip coverage for the new migration"
affects: [37F-03, 37F-10, share-create-handler, share-expiry-sweep-cron]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-subsystem config split (config_share.go) mirroring config_sandbox.go/config_compaction.go to keep config.go under the 600-LOC cap"
    - "Database-enforced tier-shape CHECK (public requires token_hash + expires_at) as the last line of defense behind a Go-level invariant"
    - "Append-only audit ledger via grant asymmetry only (SELECT+INSERT, no UPDATE/DELETE) — text identity_id with NO FK, matching skill_audit/mcp_audit"

key-files:
  created:
    - internal/db/migrations/0040_shared_links.up.sql
    - internal/db/migrations/0040_shared_links.down.sql
    - internal/db/migrate_0040_integration_test.go
    - internal/config/config_share.go
    - internal/config/config_share_test.go
  modified:
    - internal/config/config.go

key-decisions:
  - "Migration slot 0040 confirmed free at execute time (re-verified from disk, matching plan expectation) — no collision with the 0036-0039 phases running in parallel"
  - "share_audit uses grant-only append-only enforcement (SELECT+INSERT, no UPDATE/DELETE), NOT the trigger-based enforcement skill_audit/mcp_audit also carry — the plan's must_haves/acceptance_criteria explicitly scope only the grant check, so no trigger function was added (SCOPE CONTROL)"
  - "AURA_SHARE_* knobs were NOT registered in config_knobs.go's reparse-pass catalog — the plan's files_modified list and behavior spec call only for silent envutil-style fallback in every case, with no Fatal-under-strict-tier requirement (unlike AURA_SANDBOX_*)"
  - "Added migrate_0040_integration_test.go (Rule 2 deviation) — every prior CHECK-widening migration (0033/0034/0035/0039) ships its own round-trip test; 0040 was missing the same coverage"

requirements-completed: [WEBSHARE-02, WEBSHARE-03]

coverage:
  - id: D1
    description: "aura.shared_links + aura.share_audit land in migration 0040, tier-shape CHECK makes public-tier expiry+token mandatory at the database level"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "grep-based structural verify (CREATE TABLE, CHECK, unique index, grants) — internal/db/migrations/0040_shared_links.up.sql"
        status: pass
      - kind: integration
        ref: "internal/db/migrate_0040_integration_test.go#TestMigration0040RoundTrip (db_integration tag, live stack)"
        status: pass
    human_judgment: false
  - id: D2
    description: "scheduler_tasks.kind widened to admit 'share_expiry_sweep', reversible down migration deletes widened rows before narrowing the CHECK"
    requirement: "WEBSHARE-03"
    verification:
      - kind: integration
        ref: "internal/db/migrate_0040_integration_test.go#TestMigration0040RoundTrip (up -> down -1 -> up +1, version asserted at each step)"
        status: pass
    human_judgment: false
  - id: D3
    description: "config.ShareConfig fail-closed defaults (PublicEnabled=false, MaxExpiryDays=90) over envutil, wired into Config in 2 LOC"
    requirement: "WEBSHARE-03"
    verification:
      - kind: unit
        ref: "internal/config/config_share_test.go#TestShareConfig (7 behavior rows, table-driven)"
        status: pass
      - kind: unit
        ref: "go vet ./internal/config/, go build ./..., go test -race ./internal/config/"
        status: pass
    human_judgment: false

duration: 45min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 02: Share Storage Substrate Summary

**Migration 0040 lands `aura.shared_links` + append-only `aura.share_audit` with a database-enforced public-tier tier-shape CHECK, widens the scheduler kind CHECK for `share_expiry_sweep`, and adds `config.ShareConfig` (fail-closed `AURA_SHARE_PUBLIC_ENABLED`/`AURA_SHARE_MAX_EXPIRY_DAYS` knobs)**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-07-17T08:08Z (session start, after 37F-01)
- **Completed:** 2026-07-17T08:48Z
- **Tasks:** 3 planned (+1 deviation-driven test addition)
- **Files modified:** 6 (5 created, 1 modified)

## Accomplishments

- `aura.shared_links` with the `shared_links_tier_shape` CHECK making D-04's mandatory-expiry and D-13's hashed-token requirements database-enforced for the public tier — a Go bug can never persist a public link missing `token_hash` or `expires_at`
- Unique partial index `shared_links_token_hash_idx` serving both the duplicate-mint guard and the O(1) token-hash lookup
- `aura.share_audit` append-only ledger (`SELECT, INSERT` only for `aura_app`) with `text` `identity_id` and no FK on `share_link_id`/`conversation_id`, so the audit trail outlives the identity, link, and conversation it describes
- `scheduler_tasks.kind` widened to admit `'share_expiry_sweep'`, with a down migration that deletes widened-kind rows before narrowing the CHECK back (avoids the dirty-tracker footgun)
- `config.ShareConfig` (`AURA_SHARE_PUBLIC_ENABLED` default `false` — the D-02 kill-switch closing the R-08 loopback bypass at `auth.go:282`; `AURA_SHARE_MAX_EXPIRY_DAYS` default `90`, clamped when non-positive) wired into `config.go` in exactly 2 LOC (592 → 594, within the 600-LOC cap)
- A new `migrate_0040_integration_test.go` proving the up/down/up round trip live against a throwaway database

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0040 up — shared_links + share_audit + scheduler kind widen** - `dd083a00c` (feat)
2. **Task 2: Migration 0040 down — reverse in dependency order** - `15c8fdcde` (feat)
3. **Task 3 RED: failing test for share config knobs** - `5c06bba6c` (test)
4. **Task 3 GREEN: implement share config knobs** - `d37fa7616` (feat)
5. **Deviation: migration 0040 round-trip integration test** - `79045919b` (test)

**Plan metadata:** (this commit, docs: complete plan)

_Note: Task 3 is TDD — RED then GREEN commits, as required by `tdd="true"`._

## Files Created/Modified

- `internal/db/migrations/0040_shared_links.up.sql` - `aura.shared_links` + `aura.share_audit` + scheduler kind widen
- `internal/db/migrations/0040_shared_links.down.sql` - reverses in strict dependency order (kind rows before CHECK, children before parents)
- `internal/db/migrate_0040_integration_test.go` - live up/down/up round-trip proof (db_integration tag)
- `internal/config/config_share.go` - `ShareConfig` + `loadShare()` over `envutil`
- `internal/config/config_share_test.go` - 7-row table-driven behavior test
- `internal/config/config.go` - `Share ShareConfig` field + `Share: loadShare()` composer wiring (2 LOC delta)

## Decisions Made

- **Migration slot 0040 confirmed free** by re-reading `internal/db/migrations/` at execute time (per the plan's `read_first` mandate) — no collision with phases 36/38/39 landing in the same window.
- **share_audit append-only via grants only**, not the trigger-based enforcement `skill_audit`/`mcp_audit` also carry. The plan's `must_haves`/`acceptance_criteria` explicitly scope the check to grant absence (`GRANT SELECT, INSERT` with no `UPDATE`/`DELETE`); adding an unrequested `BEFORE UPDATE OR DELETE`/`TRUNCATE` trigger function would have been scope creep beyond what was asked (CLAUDE.md SCOPE CONTROL). Flagging this as a considered omission, not an oversight, in case a later plan wants the stronger belt-and-suspenders trigger.
- **No `config_knobs.go` registration** for `AURA_SHARE_*`. Unlike `AURA_SANDBOX_*` (which IS cataloged so a malformed value is Fatal under a strict profile), the plan's `<behavior>` spec wants silent-fallback in every single case (unset, garbage, non-positive) with no Fatal-under-strict-tier gate mentioned anywhere in `must_haves`/`acceptance_criteria`/`files_modified`. Treated as an explicit scope boundary.
- **Literal FK-reference grep style**: matched the plan's exact acceptance-criteria pattern `REFERENCES aura.conversations(id)` (no space before the parenthesis), which also matches the newest migrations (0036-0038) rather than the older space-before-paren style (0004/0005/0030) — both conventions coexist in the codebase; the newer no-space style was chosen to satisfy the plan's literal grep check.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added `migrate_0040_integration_test.go` round-trip test**
- **Found during:** Post-Task-2 verification (plan's top-level `<verification>` demands a migration round-trip check)
- **Issue:** The plan's `files_modified` list did not include a dedicated round-trip test file, but every prior migration that widens/narrows the `scheduler_tasks.kind` CHECK (0033, 0034, 0035, 0039) ships its own `migrate_NNNN_integration_test.go` proving `up -> down -1 -> up +1` leaves a clean, non-dirty tracker. 0040 performs the exact same delicate operation (delete-then-narrow) that migration 0034's down comment calls out as the failure mode to guard against (T-37F-16 in this plan's own threat register).
- **Fix:** Added `internal/db/migrate_0040_integration_test.go`, modeled byte-for-byte on `migrate_0039_integration_test.go`, asserting version 40 after up, 39 after `-1`, and 40 again after `+1`, all against a throwaway `aura_migrate0040_drill` database (never the live `aura` DB).
- **Files modified:** `internal/db/migrate_0040_integration_test.go` (new)
- **Verification:** `go test -tags db_integration ./internal/db/ -run TestMigration0040RoundTrip -v -count=1` → PASS live against the running Docker stack.
- **Committed in:** `79045919b`

---

**Total deviations:** 1 auto-added (missing critical test coverage, matching established codebase convention)
**Impact on plan:** Additive only — no scope creep on the schema/config surface itself, just closes a coverage gap the plan's own threat register (T-37F-16) flags as the exact risk this migration carries.

## Issues Encountered

- **Pre-commit hook (`go vet` via lefthook) blocks non-compiling commits**, which conflicts with a literal Go-TDD "RED = doesn't compile" commit. Resolved by using a minimal compiling stub (`loadShare()` returning a zero-value `ShareConfig`) for the RED commit — the test still genuinely fails 5/7 subtests (real assertion failures, not a compile error), satisfying both the TDD gate sequence and the hook's `go vet` requirement.
- **A shell-quoting mistake while reading `POSTGRES_PASSWORD` from `.env`** (an `awk -F=` field-splitting attempt, mangled by nested shell-quoting across the Bash-tool → WSL → `bash -lc` layering) caused the live `.env` postgres password to be printed once into this session's tool output before I caught it. No further command echoed it — a follow-up script-file-based extraction (writing the value straight into an env var, redacting any occurrence before ever `cat`-ing a log) was used for the actual round-trip test run. **Flagging this to the user: the postgres password value was exposed in this conversation transcript and should be rotated as a precaution**, even though it only gates local dev/loopback Postgres access.
- Pre-commit hooks (`gofmt`/`vet`/`lint`/`file-size`) run for ~2-3 minutes per commit on this host; every commit in this plan was run via `run_in_background` + poll to avoid the 2-minute foreground Bash timeout (matches the known `gsd_commit_wrapper_filesize_timeout` pattern in project memory).

## User Setup Required

None - no external service configuration required. Both `AURA_SHARE_*` env vars are optional with fail-closed defaults; no `.env` change is needed to keep existing deployments working.

**Security note:** see "Issues Encountered" above — please rotate `POSTGRES_PASSWORD` in `.env` (and the corresponding Postgres role password) at your convenience, since its value was inadvertently echoed once during this session.

## Next Phase Readiness

- The share storage substrate (`aura.shared_links`, `aura.share_audit`, `share_expiry_sweep` scheduler kind, `config.Share`) is ready for plan 37F-10's share-create handler and the expiry-sweep cron handler to build on.
- No blockers. The database-enforced tier-shape CHECK and the append-only audit grant asymmetry are both live and round-trip-tested.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 6 created/modified files verified present on disk; all 5 task commit hashes
(`dd083a00c`, `15c8fdcde`, `5c06bba6c`, `d37fa7616`, `79045919b`) verified present
in `git log --oneline --all`.
