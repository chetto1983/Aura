---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 13
subsystem: testing
tags: [golang-migrate, db_integration, ci, github-actions, migration-reversibility, musr-06, static-gate]

# Dependency graph
requires:
  - phase: 36-multi-user-identity-isolation-authula-cutover
    provides: "migration 0026 local-admin-caps seed (36-01) + scripts/check-no-url-tokens.sh MUSR-06 static gate (36-11)"
provides:
  - "Version-anchored migration-0026 reversibility test that isolates 0026's OWN down/up regardless of how many migrations sit above it (LIVE db_integration PASS at head>=32)"
  - "Enforced (non-bypassable) check-no-url-tokens.sh CI step in the build-and-lint job — a token-in-URL regression now fails the pipeline"
affects: [36-18, ci-green, phase-36-close]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Version-anchored migration round-trip: position to the target version (MigrateSteps(target - head)) BEFORE the +/-1 straddle, never a bare relative +/-1 from HEAD (golang-migrate Steps(n) is relative)"
    - "Static security gate wired as a blocking CI step beside check-file-size.sh (no continue-on-error / || true)"

key-files:
  created: []
  modified:
    - "internal/db/migrate_0026_integration_test.go — version-anchored the +/-1 straddle at v26"
    - ".github/workflows/ci.yml — added the blocking 'No long-lived token in URLs (MUSR-06)' step"

key-decisions:
  - "Position DOWN to exactly v26 (stepDownToV26 := 26 - head) before the +/-1 straddle so it reverses/re-applies migration 0026 SPECIFICALLY, not the current latest migration (0032)"
  - "Hoisted the 26 - head delta into a named local so the positioning is gofmt-clean (gofmt collapses spaces inside a call arg) AND greppable-before-straddle — realizes the plan's `26 - head` intent without a golangci-lint conflict"
  - "Assert the first post-round-trip Migrate re-applies exactly head-26 migrations; the trailing Migrate returns 0 (no-pending invariant) — strengthens the reversibility proof"
  - "MUSR-06 NOT marked requirements-complete — it is phase-spanning (closes at 36-18 push + green CI + provisioning); this plan closes only VERIF-1 and the VERIF-6 CI-wiring clause (matches 36-11/36-12 precedent)"

patterns-established:
  - "Migration reversibility tests target a version, not a relative step: MigrateSteps(target - head) to position, then +/-1 to straddle exactly one migration"
  - "New static gates land as blocking steps in build-and-lint, mirroring the check-file-size.sh step shape"

requirements-completed: []  # MUSR-06 stays open (phase-spanning; closes at 36-18). This plan closes VERIF-1 + the VERIF-6 CI-wiring clause only.

coverage:
  - id: D1
    description: "TestMigration0026LocalAdminCapsRoundTrip proves 0026's OWN reversibility (down removes governance.write/identity.create/agent.run, up restores them, `*` wildcard survives) regardless of how many migrations sit above 0026"
    requirement: "MUSR-06"
    verification:
      - kind: integration
        ref: "internal/db/migrate_0026_integration_test.go#TestMigration0026LocalAdminCapsRoundTrip (LIVE WSL db_integration, head>=32 Postgres)"
        status: pass
      - kind: unit
        ref: "go build -tags db_integration ./internal/db/ && go vet -tags db_integration ./internal/db/ (gofmt clean)"
        status: pass
    human_judgment: false
  - id: D2
    description: "check-no-url-tokens.sh runs as an enforced (non-bypassable) CI step so a future long-lived-token-in-URL regression fails the pipeline"
    requirement: "MUSR-06"
    verification:
      - kind: other
        ref: "bash scripts/check-no-url-tokens.sh (exit 0) + bash scripts/check-no-url-tokens.sh --self-test (exit 0)"
        status: pass
      - kind: other
        ref: "python -c yaml.safe_load('.github/workflows/ci.yml') — valid YAML; step positioned after check-file-size.sh, no continue-on-error/|| true"
        status: pass
    human_judgment: false

# Metrics
duration: 19min
completed: 2026-07-06
status: complete
---

# Phase 36 Plan 13: CI-Correctness Gap-Closure Summary

**Version-anchored the migration-0026 reversibility test at v26 (so it isolates 0026's own down/up, not 0032's) — LIVE db_integration PASS — and wired `check-no-url-tokens.sh` in as a blocking MUSR-06 CI gate.**

## Performance

- **Duration:** ~19 min
- **Started:** 2026-07-06T06:48:20Z
- **Completed:** 2026-07-06T07:07:37Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- **Fixed the one CONFIRMED-broken Phase-36 test (VERIF-1).** `TestMigration0026LocalAdminCapsRoundTrip` used a bare `MigrateSteps(ctx, url, -1)/(+1)` straddle. golang-migrate's `Steps(n)` is a RELATIVE step from the CURRENT head, so with 32 migrations present `-1` reversed `0032_owner_rls`, not `0026` — the test asserted the wrong thing and failed on GitHub Actions run 28753262579 ("governance.write present=true, want present=false"). It now positions DOWN to exactly v26 first (`stepDownToV26 := 26 - head`, reversing 0027..HEAD), so the `-1`/`+1` straddle reverses/re-applies migration 0026 SPECIFICALLY.
- **Ran the fix LIVE, not inferred.** In WSL against the live head≥32 Postgres: `TestMigration0026LocalAdminCapsRoundTrip` **PASS (1.04s)**. The confirmed-broken CI test is now green.
- **Closed the MUSR-06 CI-wiring gap (VERIF-6).** Added a blocking `bash scripts/check-no-url-tokens.sh` step to the build-and-lint job — the 36-11→36-12 handoff commitment the verifier flagged as never honored. A session/auth-token-in-URL regression now fails the pipeline.

## Task Commits

Each task was committed atomically (direct `git commit`, pre-commit hooks ran green — no `--no-verify`):

1. **Task 1: Version-anchor the migration-0026 reversibility test** - `653dfdd3` (test)
2. **Task 2: Wire scripts/check-no-url-tokens.sh into CI as an enforced gate** - `9796c326` (chore)

**Plan metadata:** this SUMMARY + STATE.md + ROADMAP.md (docs commit)

## Files Created/Modified

- `internal/db/migrate_0026_integration_test.go` — Captured `head := currentMigrationVersion(...)`; inserted a positioning `MigrateSteps(ctx, migrateURL, stepDownToV26)` (where `stepDownToV26 := 26 - head`) BEFORE the `-1`/`+1` straddle; the `-1` now reverses 26→25 (0026's own down: the three explicit caps drop, `*` survives), `+1` re-applies 25→26 (idempotent restore); tail asserts the first post-round-trip `Migrate` re-applies exactly `head-26` migrations and the trailing `Migrate` returns 0. Fresh-DB drill, `EnsureRoles`, pools, `localCapabilitySet0026`, `requireCap0026`, the `//go:build db_integration` tag + `envOrSkip` all byte-identical. 25 insertions, 5 deletions; 170 LOC (≤600).
- `.github/workflows/ci.yml` — One new blocking step `- name: No long-lived token in URLs (MUSR-06)` / `run: bash scripts/check-no-url-tokens.sh` in the build-and-lint job, immediately after the "File-size cap (600 LOC)" step and before "AG-UI boundary gate". No `continue-on-error`, no `|| true`. 3 insertions; no other job touched.

## Verification (all executed, real results)

| Check | Command | Result |
|---|---|---|
| Build (Task 1) | `go build -tags db_integration ./internal/db/` | exit 0 |
| Vet (Task 1) | `go vet -tags db_integration ./internal/db/` | exit 0 |
| Format (Task 1) | `gofmt -l internal/db/migrate_0026_integration_test.go` | empty (clean) |
| Positioning precedes straddle | grep `26 - head` (line 99) vs `-1`/`+1` (lines 105/115) | positioning precedes straddle ✓ |
| **LIVE migrate-0026 (Task 1)** | `CGO_ENABLED=1 go test -tags db_integration -run TestMigration0026LocalAdminCapsRoundTrip ./internal/db/ -count=1` (WSL, live PG head≥32) | **PASS (1.04s)** |
| Token gate (Task 2) | `bash scripts/check-no-url-tokens.sh` | exit 0 |
| Token gate self-test (Task 2) | `bash scripts/check-no-url-tokens.sh --self-test` | exit 0 (planted `?session_token=` caught) |
| YAML validity (Task 2) | `python -c "yaml.safe_load(open('.github/workflows/ci.yml'))"` | YAML OK |
| Pre-commit hooks | lefthook gofmt + vet + file-size (both commits) | all green |

## Decisions Made

- **Position-then-straddle over a bare relative step.** `MigrateSteps(26 - head)` first, then `-1`/`+1` — the only way to isolate 0026 when later migrations exist.
- **Named-variable delta hoist (`stepDownToV26 := 26 - head`).** gofmt collapses `26 - head` to `26-head` inside a call argument but keeps the spaces at statement level (see the existing `headVersion - 16` in `migrate_0017_integration_test.go`). Hoisting keeps the source gofmt-clean (CLAUDE.md golangci-lint=0 is absolute) AND keeps the greppable `26 - head` positioning ahead of the straddle. This is a formatting-driven realization of the plan's `MigrateSteps(ctx, migrateURL, 26 - head)`, not a scope change.
- **Strengthened tail assertion.** First post-round-trip `Migrate` must re-apply exactly `head-26` (asserted); the trailing `Migrate` must return 0 — proving both re-convergence to HEAD and no lingering pending.
- **MUSR-06 left open.** Phase-spanning requirement; `requirements mark-complete` intentionally NOT run (matches 36-01/02/08/10/11/12 precedent). This plan closes VERIF-1 and the VERIF-6 CI-wiring clause only.

## Deviations from Plan

None - plan executed exactly as written. (The `stepDownToV26` variable hoist is a gofmt-clean realization of the plan's `26 - head` positioning step, documented above — no behavioral or scope deviation.)

## Issues Encountered

- **gofmt vs the plan's literal `26 - head`.** gofmt reformats `MigrateSteps(ctx, migrateURL, 26 - head)` to `26-head` (no spaces) inside a call argument, which would create a golangci-lint/gofmt diff. Resolved by hoisting `stepDownToV26 := 26 - head` (statement-level, where gofmt keeps the spaces), satisfying both the gofmt gate and the acceptance grep for a `26 - head` positioning step that precedes the straddle.

## Known Stubs

None — both changes are complete. No hardcoded empty values, placeholders, or unwired data sources introduced (test edit + one CI shell step).

## Threat Surface Scan

No new security-relevant surface introduced (no network endpoints, auth paths, file access, or schema changes). Both threat-register mitigations were implemented: T-36-13-01 (version-anchor the straddle — done, live-proven) and T-36-13-02 (enforce the token gate in CI — done). T-36-13-SC (no package installs) holds: `go.mod`/`go.sum`/`package.json` byte-unchanged; zero new dependencies.

## Self-Check: PASSED

- Commit `653dfdd3` (test) — FOUND in git log; touched exactly `internal/db/migrate_0026_integration_test.go`.
- Commit `9796c326` (chore) — FOUND in git log; touched exactly `.github/workflows/ci.yml`.
- `internal/db/migrate_0026_integration_test.go` — FOUND (present + tracked).
- `.github/workflows/ci.yml` — FOUND (present + tracked).

## Next Phase Readiness

- **36-14** (next gap-closure Wave-1 plan): daemon provisioning/de-provisioning wiring + migration 0033 (scheduler kind CHECK admits `identity_purge`) + deactivation auth-gate (VERIF-3/HI-01 + HI-02).
- The two CI blockers this plan targets are cleared: the migrate-0026 test is live-green and the MUSR-06 gate is enforced. **36-18** owns the terminal push + full-CI-matrix-green + live-stack acceptance that will confirm these in the real GitHub Actions pipeline; nothing in this plan blocks that push.

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-06*
