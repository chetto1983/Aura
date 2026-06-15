---
phase: 08-sandbox-2b-session-bound
plan: 02
subsystem: database
tags: [postgres, sqlc, migration, pgx, config, sandbox, sentinels, errors]

# Dependency graph
requires:
  - phase: 08-01
    provides: PRD amendments (FK uuid not text, migration 0008 not 0010, host-proxy egress, os.Root walks, scoring pull-forward) + 08-DECISIONS-WAVE0 OQ4 privacy-mode contract
  - phase: 04 (Slice 1.8 conversations)
    provides: aura.conversations(id uuid) — the FK parent for sandbox_sessions
provides:
  - "aura.sandbox_sessions table (migration 0008, uuid FK ON DELETE CASCADE, status CHECK, reaper index, dual GRANT)"
  - "4 sqlc queries: InsertSession / TouchLastUsed / MarkTerminated / ListActive (AuraSandboxSessions struct, ConversationID pgtype.UUID)"
  - "6 config knobs: SandboxSessionTTLSec, SandboxMaxConcurrentSessions, SandboxWorkspaceMaxBytes, SandboxNetworkAllowHosts, RiskAlertThreshold, PrivacyMode"
  - "2 typed sentinels: ErrSessionCapReached, ErrWorkspaceQuotaExceeded (errors.Is-friendly)"
affects: [08-05 SessionManager, 08-06 workspace+network proxy, 08-07 sidecar, 08-08 wiring, 08-09 db-tier validation + 08-SECURITY]

# Tech tracking
tech-stack:
  added: []  # zero new modules — sqlc-generated + existing pgx; stdlib errors
  patterns:
    - "uuid FK to conversations(id) ON DELETE CASCADE (mirrors 0005/0007, landmine #1 resolved)"
    - "one-file-one-concern sqlc query file (anti-god-class, exactly 4 queries)"
    - "non-fatal envIntDefault/envDefault config knobs (AURA_<DOMAIN>_<UNIT>, typo falls back not boot-fatal)"
    - "errors.Is-friendly typed sentinels appended to the package var block (no silent LRU on cap)"

key-files:
  created:
    - internal/db/migrations/0008_sandbox_sessions.up.sql
    - internal/db/migrations/0008_sandbox_sessions.down.sql
    - internal/db/queries/sandbox_sessions.sql
    - internal/db/sqlc/sandbox_sessions.sql.go
  modified:
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/config/config.go
    - internal/sandbox/errors.go
    - .env.example

key-decisions:
  - "FK conversation_id is uuid NOT text (landmine #1) — a text FK to the uuid PK is rejected as incompatible-types"
  - "Migration number is 0008 (landmine #2) — repo floor is 0007_cache_metrics; PRD's 0010 reference superseded"
  - "PrivacyMode is read-only in this plan; the local-only fail-fast cross-check is deferred to 08-05 per 08-DECISIONS-WAVE0 OQ4"
  - "ErrSessionCapReached returns a sentinel rather than silently LRU-evicting (D-12)"

patterns-established:
  - "Pattern: control-plane registry table with reaper index on (status, last_used_at) for sweep + boot recovery"
  - "Pattern: foundation-only plan — schema/queries/config/sentinels declared so Wave-3 plans compile against the symbols; no business logic, no goroutines"

requirements-completed: [CAP-02]

# Metrics
duration: ~14min
completed: 2026-06-03
---

# Phase 8 Plan 02: Session Control-Plane Substrate Summary

**Migration 0008 (`aura.sandbox_sessions`, uuid FK + reaper index + dual GRANT) with 4 sqlc queries, 6 non-fatal config knobs (incl. the OQ4 PrivacyMode field), and 2 errors.Is-friendly sentinels — the durable substrate every Wave-3 session plan compiles against.**

## Performance

- **Duration:** ~14 min
- **Started:** 2026-06-03T09:31Z
- **Completed:** 2026-06-03T09:45:32Z
- **Tasks:** 2
- **Files modified:** 9 (4 created, 5 modified)

## Accomplishments
- `aura.sandbox_sessions` migration 0008 lands with the landmine-#1 fix (`conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE`), a `status` CHECK over `('active','idle','terminated','evicted')`, the `(status, last_used_at)` reaper/boot-recovery index, and the aura_app-DML / aura_migrate-DDL dual GRANT. Number is 0008 (landmine #2), down drops the table.
- 4 sqlc queries (`InsertSession :one`, `TouchLastUsed :exec`, `MarkTerminated :exec`, `ListActive :many`) regenerated cleanly with sqlc v1.31.1 — the generated `AuraSandboxSessions.ConversationID` binds as `pgtype.UUID` (same type the conversations store uses).
- 6 config knobs added via the established non-fatal `envIntDefault`/`envDefault` helpers, including the `PrivacyMode` field decided in 08-01 Wave-0 (read-only here; fail-fast logic deferred to 08-05). `.env.example` documents all 6.
- 2 typed sentinels (`ErrSessionCapReached`, `ErrWorkspaceQuotaExceeded`) appended to the existing `internal/sandbox/errors.go` var block with errors.Is-friendly doc-comments.

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0008 + sqlc queries + regen** - `93ba8ef0` (feat)
2. **Task 2: 5 config knobs (+ PrivacyMode) + 2 sentinels** - `327cfd9f` (feat)

## Files Created/Modified
- `internal/db/migrations/0008_sandbox_sessions.up.sql` - control-plane registry table (uuid FK, status CHECK, reaper index, dual GRANT)
- `internal/db/migrations/0008_sandbox_sessions.down.sql` - `DROP TABLE IF EXISTS aura.sandbox_sessions`
- `internal/db/queries/sandbox_sessions.sql` - the 4 named sqlc queries
- `internal/db/sqlc/sandbox_sessions.sql.go` - generated query methods (InsertSession/ListActive/MarkTerminated/TouchLastUsed)
- `internal/db/sqlc/models.go` - generated `AuraSandboxSessions` struct
- `internal/db/sqlc/querier.go` - generated Querier interface methods
- `internal/config/config.go` - 6 new fields + loader lines (Phase 8 sandbox block)
- `internal/sandbox/errors.go` - 2 new sentinels in the var block
- `.env.example` - Slice 2b session-sandbox section (6 vars + comments)

## Decisions Made
None beyond the plan — followed the plan and the 08-01/Wave-0 contract exactly (FK uuid, number 0008, PrivacyMode read-only, sentinel-on-cap).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None. sqlc v1.31.1 (matching the in-tree generated-code version) was available on the Windows PATH, so the generated client was regenerated in-place rather than deferred to WSL.

## Verification Performed
- `go build ./...` — exit 0 (after each task).
- `go vet ./...` — exit 0.
- `go test ./internal/config/ ./internal/sandbox/` — both `ok`.
- `sqlc generate` — exit 0; `AuraSandboxSessions.ConversationID` is `pgtype.UUID`.
- Grep checks: `conversation_id uuid` = 1; the 4 query names = 4; 6 config env vars present; 2 sentinels present; 6 vars in `.env.example`.
- Pre-commit hooks (gofmt, vet, file-size ≤600 LOC) green on both commits.

**NOT run here (out of scope for this worktree):** the live `db_integration` tier (migrate up/no-op/down) requires a live Postgres not available in this worktree — that is the Gate-3 / 08-09 db-tier validation. The build-level check (sqlc generate + go build) is the bar for this foundation plan, per the plan's own verification note.

## Threat Surface
No new surface beyond the plan's `<threat_model>`: the migration keeps DDL gated to aura_migrate (T-08-02-V14-ROLE), the FK is uuid+CASCADE (T-08-02-V5-FK), the DoS-cap knobs + ErrSessionCapReached are declared (T-08-02-DOS-CAP, enforcement in 08-05), and PrivacyMode is read (T-08-02-INFO-PRIVACY, cross-check in 08-05). No new module (T-08-02-SC accept holds).

## Next Phase Readiness
- The session control plane (08-05) can now `db.InsertSession`/`ListActive`/`TouchLastUsed`/`MarkTerminated` and return `ErrSessionCapReached`/`ErrWorkspaceQuotaExceeded`.
- The proxy/workspace plans (08-06) have `SandboxNetworkAllowHosts`, `SandboxWorkspaceMaxBytes`, and the sentinels to wire against.
- The privacy-mode fail-fast cross-check (`PrivacyMode == "local-only"` AND non-empty allowlist) is owed by 08-05 per 08-DECISIONS-WAVE0 OQ4 — the field is in place.
- Orchestrator owns STATE.md / ROADMAP.md updates after the Wave-2 worktree agents complete (NOT touched here).

## Self-Check: PASSED

All created files exist on disk (4 created + SUMMARY) and all 3 commits (`93ba8ef0`, `327cfd9f`, `d935915f`) are present in the git log. STATE.md / ROADMAP.md untouched (orchestrator owns those).

---
*Phase: 08-sandbox-2b-session-bound*
*Completed: 2026-06-03*
