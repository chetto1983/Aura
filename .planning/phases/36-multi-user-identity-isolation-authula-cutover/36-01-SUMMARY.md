---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 01
subsystem: auth
tags: [break-glass, identity, capability-grants, password-reset-tokens, recovery, cli, postgres, go, migration]

# Dependency graph
requires:
  - phase: 04-identity
    provides: "aura.identities + aura.capability_grants + seeded `local`/system identity (UUID …001) with the `*` wildcard grant; identity.Store (GetIdentityByName, GrantCapability, HasCapability)"
  - phase: 25-cockpit (0023_identity_recovery)
    provides: "aura.password_reset_challenges + aura.password_reset_tokens short-lived hashed-token infra + agui.HashLookupToken + cmd/aura serve_password_reset.go online mint shape"
provides:
  - "Migration 0026: idempotent seed of `local`'s explicit admin caps (governance.write + identity.create + agent.run)"
  - "`aura identity recover <name>` — host-only break-glass CLI that mints a short-lived hashed reset token reusing the 0023 infra"
  - "mintBreakGlassToken / breakGlassChallengeParams / breakGlassTokenParams helpers in cmd/aura/recovery.go"
  - "Neutral append-only break-glass audit event (identity_recovery_audit: break_glass_token_minted)"
affects: [36-02-provisioning-saga, 36-08-authula-cutover, 36-12-two-identity-e2e, capability-per-route-authz, admin-audit]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Break-glass = host-CLI-minted recovery (D-16): host access is the ownership proof; NO server route mints an admin bypass"
    - "Reuse the shipped two-table token scheme (challenge→token) exactly rather than inventing a new one: create+consume a parent challenge to satisfy the token FK, mirroring serve_password_reset.go VerifyChallenge"
    - "Explicit named admin caps seeded alongside the system `*` wildcard so admin-gated routes resolve on named capabilities and survive any future wildcard narrowing (D-25 / OQ3: reuse governance.write, no net-new settings.model.write)"

key-files:
  created:
    - "internal/db/migrations/0026_local_admin_caps.up.sql — idempotent seed of local's 3 explicit admin caps"
    - "internal/db/migrations/0026_local_admin_caps.down.sql — removes exactly those 3 rows, leaves `*` intact"
    - "internal/db/migrate_0026_integration_test.go — db_integration round-trip (seed + down + re-up + `*` survival)"
    - "cmd/aura/recovery.go — identityRecover + mintBreakGlassToken + param builders (143 LOC)"
    - "cmd/aura/recovery_test.go — unit tests for TTL + challenge/token param builders"
    - "cmd/aura/recovery_integration_test.go — db_integration: minted token validates + audit row + fail-closed unknown identity"
  modified:
    - "cmd/aura/identity.go — added `recover` dispatch case + usage + header doc (still 137 LOC)"

key-decisions:
  - "Break-glass token TTL = 10m (reuse serve_password_reset.go's online mint shape) rather than the plan's loose parenthetical '~1h'/'const' — there is no such const; 10m is the exact reused shape and a shorter, safer window"
  - "Reusing the 0023 token scheme REQUIRES a parent challenge row (password_reset_tokens.challenge_id has a NOT NULL FK to password_reset_challenges): the CLI creates + consumes a throwaway host-minted challenge in the same tx, exactly as VerifyChallenge does — not a new token scheme"
  - "Added an atomic neutral break-glass audit row (identity_recovery_audit) inside the mint tx (plan-suggested + Rule 2 audit-a-privileged-action)"
  - "Did NOT mark MUSR-06 complete: it is a phase-spanning requirement (Authula default cutover + provisioning + capability-per-route + no-token-in-URL E2E) that closes at 36-12; this plan ships only the break-glass + local-admin-seed slice"

patterns-established:
  - "Host-CLI break-glass mint: resolve-by-name → create+consume challenge → insert hashed token → append neutral audit, all atomic; plaintext printed to stdout ONLY, never slog"
  - "db_integration migration round-trip proves a data-only seed migration reverses correctly while preserving pre-existing seeded rows"

requirements-completed: []

coverage:
  - id: D1
    description: "Migration 0026 idempotently seeds local's 3 explicit admin caps (governance.write + identity.create + agent.run) and its down migration removes exactly those rows while the seeded `*` wildcard survives"
    requirement: "MUSR-06"
    verification:
      - kind: integration
        ref: "internal/db/migrate_0026_integration_test.go#TestMigration0026LocalAdminCapsRoundTrip"
        status: unknown
    human_judgment: false
    rationale: "Written + compiles under -tags db_integration; NOT executed on this Windows host (native db_integration needs the live PG stack in WSL/CI). Verifier must run the live tier."
  - id: D2
    description: "`aura identity recover` pure logic: token TTL is short-lived (10m) and the challenge/token param builders reuse the online mint's exact max_attempts (5/3) + expiry shape and store the hashed token, never plaintext"
    requirement: "MUSR-06"
    verification:
      - kind: unit
        ref: "cmd/aura/recovery_test.go#TestBreakGlassTokenParams / TestBreakGlassChallengeParams / TestBreakGlassTokenTTLIsShortLived"
        status: pass
    human_judgment: false
  - id: D3
    description: "Break-glass recover mints a WORKING short-lived token end-to-end (its hash validates via resolveResetTokenHash), hashes the token at rest, appends a neutral break-glass audit row, and fails closed for an unknown identity"
    requirement: "MUSR-06"
    verification:
      - kind: integration
        ref: "cmd/aura/recovery_integration_test.go#TestMintBreakGlassTokenRoundTrip"
        status: unknown
    human_judgment: false
    rationale: "Written + compiles under -tags db_integration; NOT executed on this Windows host (needs POSTGRES_PASSWORD + AURA_DB_URL + AURA_DB_MIGRATE_URL on the live stack). Verifier must run the live tier (no-skip-as-green)."

# Metrics
duration: 11min
completed: 2026-07-05
status: complete
---

# Phase 36 Plan 01: MUSR-06 Break-Glass First Summary

**Host-only `aura identity recover` that mints a short-lived hashed reset token by reusing the shipped 0023 token infra, plus migration 0026 seeding `local`'s explicit `governance.write` + `identity.create` + `agent.run` admin caps.**

## Performance

- **Duration:** 11 min (active execution; each commit +~55s for the file-size hook)
- **Started:** 2026-07-05T18:02:29Z
- **Completed:** 2026-07-05T18:14:08Z
- **Tasks:** 2
- **Files created/modified:** 7 (6 created, 1 modified)

## Accomplishments
- **Migration 0026** idempotently seeds the `local` identity (UUID …001) with three EXPLICIT admin capability grants — `governance.write`, `identity.create`, `agent.run` — mirroring the `0004_identity` seed shape (`ON CONFLICT DO NOTHING`); the down migration removes exactly those three rows and leaves the seeded `*` wildcard intact. Per 36-RESEARCH OQ3 the D-02/D-03 model-settings capability is realized as the EXISTING `governance.write` (no net-new `settings.model.write`).
- **`aura identity recover <name>`** ships as a host-only break-glass path: it resolves the identity, mints a short-lived (10m) hashed reset token by reusing the 0023 `password_reset_challenges`+`password_reset_tokens` infra exactly (create+consume a parent challenge, insert a hashed token), prints the one-time PLAINTEXT token to stdout (never logged), and appends a neutral audit row — all inside one atomic tx. Implementation lives in `cmd/aura/recovery.go` so `identity.go` stays at 137 LOC.
- **No standing bypass, no new token scheme, no Casbin** — host access is the sole ownership proof (D-16); the Casbin indirect deps stay unimported.
- **Tests:** 3 Windows-runnable unit tests (param builders + TTL, all pass) + 2 `db_integration` round-trips (migration seed/reversal; end-to-end working-token + audit + fail-closed) that gate in WSL/CI under no-skip-as-green.

## Task Commits

Each task was committed atomically (hooks green: gofmt + vet + file-size):

1. **Task 1: Seed local's explicit admin capabilities (migration 0026)** — `1cd90d0a` (feat)
2. **Task 2: Break-glass `aura identity recover` subcommand (host-only)** — `89ef6337` (feat)

**Plan metadata:** _(final docs commit — this SUMMARY + STATE.md + ROADMAP.md)_

## Files Created/Modified
- `internal/db/migrations/0026_local_admin_caps.up.sql` — idempotent seed of local's 3 explicit admin caps
- `internal/db/migrations/0026_local_admin_caps.down.sql` — removes exactly those 3 rows; `*` wildcard preserved
- `internal/db/migrate_0026_integration_test.go` — db_integration round-trip (seed → down → re-up → `*` survives → no-op re-migrate)
- `cmd/aura/recovery.go` — `identityRecover`, `mintBreakGlassToken`, `breakGlassChallengeParams`, `breakGlassTokenParams`
- `cmd/aura/recovery_test.go` — unit tests (TTL short-lived + online-shape param reuse + hashed-not-plaintext)
- `cmd/aura/recovery_integration_test.go` — db_integration end-to-end mint proof + fail-closed unknown identity
- `cmd/aura/identity.go` — `recover` dispatch case + usage string + header doc (137 LOC, under the 600 cap)

## Decisions Made
- **TTL 10m, not "~1h":** The plan's parenthetical said "reuse the existing reset-token TTL const, ~1h", but `serve_password_reset.go` has no such const — it mints 10-minute tokens inline. The authoritative directive is "reuse the exact shape from serve_password_reset.go", so `breakGlassTokenTTL = 10 * time.Minute` (a named const with a comment). Shorter is also safer for a bearer secret.
- **Parent challenge is mandatory:** `password_reset_tokens.challenge_id` is a NOT NULL FK to `password_reset_challenges (id, identity_id)`, so a token cannot be inserted standalone. Faithful reuse of the 0023 scheme means creating + consuming a throwaway host-minted challenge (random discarded `code_hash`) in the same tx, exactly as the online `VerifyChallenge` does. This is the existing two-table scheme used correctly, not a new one.
- **Atomic neutral audit:** a `break_glass_token_minted` row is appended to `identity_recovery_audit` inside the mint tx (plan-suggested + Rule 2: a privileged break-glass action must be audited). It carries no token/code/secret.

## Deviations from Plan

### Auto-fixed / Clarified

**1. [Rule 2 - Missing Critical] Added an atomic break-glass audit write**
- **Found during:** Task 2
- **Issue:** A privileged host-minted credential reset with no durable record is a repudiation gap (threat T-36-01 register expects auditing).
- **Fix:** `mintBreakGlassToken` appends a neutral `identity_recovery_audit` row (event `break_glass_token_minted`, no secrets) inside the same tx as the token insert.
- **Files modified:** cmd/aura/recovery.go
- **Verification:** `TestMintBreakGlassTokenRoundTrip` asserts an audit row exists (db_integration).
- **Committed in:** `89ef6337`

**2. [Clarification] TTL resolved to the actual reused shape (10m), not the plan's "~1h const"**
- **Found during:** Task 2
- **Issue:** The plan referenced a nonexistent "reset-token TTL const, ~1h"; the real online mint uses inline `10 * time.Minute`.
- **Fix:** Introduced `breakGlassTokenTTL = 10 * time.Minute` matching the reused shape (shorter, safer).
- **Files modified:** cmd/aura/recovery.go
- **Verification:** `TestBreakGlassTokenTTLIsShortLived` pins it to (0, 1h] and to 10m.
- **Committed in:** `89ef6337`

---

**Total deviations:** 2 (1 missing-critical audit add, 1 TTL clarification). No scope creep — both stay strictly inside the plan's declared `files_modified` (cmd/aura/recovery.go).
**Impact on plan:** Both strengthen the deliverable; neither alters the plan's intent or file scope.

## Issues Encountered
- **`must_haves` truth 3 ("`aura identity grant/revoke` writes an audited capability change") is NOT delivered by this plan and is flagged for the verifier.** This plan's two tasks (migration 0026 + `recover`) do not touch `grant`/`revoke`, and the plan's `files_modified` deliberately excludes `internal/identity/store.go`. Today `grant`/`revoke` emit a stdout confirmation but write NO dedicated DB capability-audit ledger (the `identity_audit` table is provisioning-only, `identity_recovery_audit` is recovery-only). The break-glass `recover` path IS audited. A durable grant/revoke capability-audit belongs to the D-26/D-28 admin-audit work in a later Phase-36 plan; it was left out of scope here to honor SCOPE CONTROL and the declared file set. **Recommend the phase verifier confirm this is acceptable or route it to a follow-up plan.**
- **Windows can't run the `-race`/`db_integration` tiers.** Native `db_integration` needs the live PG stack (WSL/CI per CLAUDE.md "Where to run what"). The two db_integration tests are written + compile-clean under `-tags db_integration` (verified via `go vet -tags db_integration`) but were NOT executed here — status `unknown`, to be run live (no fabricated green).

## Known Stubs
None — no placeholder values, mock data paths, or TODO/FIXME markers were introduced.

## Validation Run (this host, Windows/Git-Bash)
- `go build ./...` — clean
- `go vet ./...` — clean
- `go vet -tags db_integration ./internal/db/ ./cmd/aura/` — clean (integration tests compile)
- `go test ./cmd/aura/` — ok (unit tests, incl. the 3 break-glass tests, pass)
- **Deferred to WSL/CI (no-skip-as-green):** `go test -tags db_integration ./internal/db/ ./cmd/aura/` and the `-race` matrix.

## Next Phase Readiness
- `local` now holds named admin caps, so the capability-per-route gating built by later Phase-36 plans resolves on `governance.write`/`identity.create`/`agent.run` (not solely `*`).
- Break-glass recovery is shipped first (MUSR-06 ordering satisfied) and is exercised again by the 36-12 two-identity live E2E ("break-glass CLI mints a working reset").
- **Blocker for MUSR-06 close:** the Authula default cutover, capability-per-route enforcement, and the no-token-in-URL E2E remain (plans 36-02..36-12). MUSR-06 stays `[ ]` until 36-12.
- **Open follow-up:** durable `grant`/`revoke` capability-audit (see Issues Encountered).

## Self-Check: PASSED
- All 7 created/modified files present on disk.
- Both task commits present in git history (`1cd90d0a`, `89ef6337`).

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-05*
