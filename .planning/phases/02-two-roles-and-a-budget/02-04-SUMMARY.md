---
phase: 02-two-roles-and-a-budget
plan: 04
subsystem: auth
tags: [postgres, rls, sqlc, golang-migrate, audit, rbac, net-http]

requires:
  - phase: 02-two-roles-and-a-budget
    provides: "Plan 02-01's explicit capability_grants set (migration 0121 retired the wildcard), so a denial is a real event rather than an impossibility"
provides:
  - "aura.capability_denials — an append-only, RLS-scoped ledger of every capability refusal (who, which capability, which route, when, why)"
  - "A recorder wired into RequireCapability behind a consumer-side port, covering all three refusal branches including the store-error branch that was previously invisible"
  - "A fifth UNION leg on auditActivityQuery so denials read back through the existing GET /api/admin/audit feed — no new route, no new handler, no new frontend query"
  - "The composition-root wiring that makes the ledger live in a running daemon (AuthDeps.DenialRecorder)"
affects: [02-07 admin controls, 02-10 live-run evidence, 03 boundary-under-attack]

actuals:
  tokens: 13610
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Consumer-side port for an audit write: the interface is declared where it is used (internal/agui), not where it is implemented"
    - "Detached, timeout-bounded write on a response path: context.WithoutCancel rather than a fire-and-forget goroutine"

key-files:
  created:
    - internal/db/migrations/0123_capability_denials.up.sql
    - internal/db/migrations/0123_capability_denials.down.sql
    - internal/db/queries/capability_denials.sql
    - internal/db/sqlc/capability_denials.sql.go
    - internal/db/migrate_0123_integration_test.go
    - internal/agui/capability_denial_store.go
    - internal/agui/capability_denial_store_test.go
    - internal/agui/capability_denial_integration_test.go
    - internal/agui/auth_denial_test.go
  modified:
    - internal/agui/auth.go
    - internal/agui/audit_store.go
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/db/db_unit_test.go
    - cmd/aura/serve_auth.go
    - cmd/aura/serve_auth_test.go

key-decisions:
  - "Record the matched route pattern (r.Pattern), never the raw request path — a denial on /api/admin/identities/{id}/capabilities is ONE route in the feed rather than one row per identity id (T-02-23)"
  - "A closed cause vocabulary (no_principal | store_error | not_held) enforced by a CHECK constraint at the database layer, not free text — free text is where a message carrying a path or an id accumulates (T-02-22)"
  - "The no-principal branch records under a deliberate non-UUID sentinel `(no-principal)` rather than dropping the row: that is exactly the denial RBAC-09 most wants kept"
  - "The write is synchronous on a context.WithoutCancel-derived, timeout-bounded context — the request's own context is about to be cancelled as the 403 goes out, and a goroutine would lose the write on shutdown and lose ordering (T-02-21)"
  - "A recorder failure logs at warn and leaves the 403 standing — the one deliberate errors-passing-silently exception in this plan, and the log line is what makes it not silent"
  - "Read-back is a fifth UNION leg on the existing admin feed, not a new endpoint: a second endpoint would be a second thing to secure"
  - "The recorder is nil-checked at the composition root, not inside the constructor: a *PgCapabilityDenialStore over a nil pool assigned to the interface field is a NON-nil interface value that would slip past RequireCapability's own nil guard and panic on every refusal"

patterns-established:
  - "Denial audit: record the four facts and nothing that is not one of them — no body, header, cookie, token, IP or user agent"
  - "UNION-leg extension of auditActivityQuery: column names come from the first SELECT and UNION ALL matches by POSITION, so a mis-ordered leg compiles, runs, and silently mislabels every row"
  - "Feed ordering carries a deterministic secondary key (source, target) beyond created_at DESC — a feed that reshuffles on refresh reads as data loss to a human"

requirements-completed: [RBAC-09, RBAC-10]

coverage:
  - id: D1
    description: "Every capability refusal is recorded with who, which capability, which route and when — across all three of RequireCapability's refusal branches, including the store-error branch"
    requirement: RBAC-09
    verification:
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestRequireCapabilityRecordsDenial_MissingPrincipal"
        status: pass
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestRequireCapabilityRecordsDenial_StoreError"
        status: pass
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestRequireCapabilityRecordsDenial_NotHeld"
        status: pass
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestRequireCapabilityRecordsNothingOnSuccess"
        status: pass
    human_judgment: false
  - id: D2
    description: "A failed denial write never converts a 403 into a 200 or a 500, and an unwired recorder does not panic"
    requirement: RBAC-09
    verification:
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestRequireCapabilityStillDeniesWhenRecorderFails"
        status: pass
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestRequireCapabilityWithNilRecorder"
        status: pass
    human_judgment: false
  - id: D3
    description: "The ledger is RLS-scoped like every other identity-keyed table, and the recorded route is the matched pattern rather than the raw path"
    requirement: RBAC-09
    verification:
      - kind: integration
        ref: "internal/agui/capability_denial_integration_test.go#TestCapabilityDenialsRLSAndScope"
        status: pass
      - kind: unit
        ref: "internal/agui/auth_denial_test.go#TestDenialRecordsRoutePatternNotRawPath"
        status: pass
      - kind: integration
        ref: "internal/db/migrate_0123_integration_test.go#TestMigrate0123CapabilityDenialsFreshUpDownUp"
        status: pass
    human_judgment: false
  - id: D4
    description: "Denials read back out of the existing GET /api/admin/audit feed — adjacent, empty and ordering behaviours all covered, other identities' denials invisible"
    requirement: RBAC-10
    verification:
      - kind: integration
        ref: "internal/agui/capability_denial_integration_test.go#TestAuditFeedIncludesCapabilityDenials"
        status: pass
      - kind: integration
        ref: "internal/agui/capability_denial_integration_test.go#TestAuditFeedDenialsForOtherIdentityNotVisible"
        status: pass
      - kind: integration
        ref: "internal/agui/capability_denial_integration_test.go#TestAuditFeedEmptyDenialsIsEmptyList"
        status: pass
      - kind: integration
        ref: "internal/agui/capability_denial_integration_test.go#TestAuditFeedDenialsAreDistinctRows"
        status: pass
      - kind: integration
        ref: "internal/agui/capability_denial_integration_test.go#TestAuditFeedOrderIsDeterministicAtEqualTimestamps"
        status: pass
    human_judgment: false
  - id: D5
    description: "The ledger is actually live in a running daemon: a real denial on a real aura serve lands a row and comes back out of GET /api/admin/audit"
    requirement: RBAC-10
    verification:
      - kind: unit
        ref: "cmd/aura/serve_auth_test.go#TestWithDenialRecorderWiresTheLedger"
        status: pass
    human_judgment: true
    rationale: "The wiring is proven, but no test drives the full path — HTTP request against a live aura serve, row in the real table, row out of the real admin endpoint. Every test here either injects its own recorder or queries the store directly. The end-to-end claim belongs to plan 02-10's live-run evidence."

duration: not measured
completed: 2026-09-09
status: complete
---

# Phase 02 Plan 04: Capability denial audit trail

**An append-only `aura.capability_denials` ledger written at RequireCapability's three refusal branches, read back through a fifth UNION leg on the audit feed that already exists — and wired into the composition root so it records in a running daemon, not only in tests.**

## Performance

- **Duration:** not measured — the plan was executed across a session that ended before it wrote this summary; no start/stop timestamps survive
- **Tasks:** 2
- **Commits:** 5 (4 from the plan's two TDD cycles, 1 closing an out-of-plan gap)
- **Files created/modified:** 16

## Accomplishments

- **A refusal is no longer forgotten.** Before this plan `RequireCapability` wrote `http.Error(w, "forbidden", 403)` and nothing else. All three of its refusal branches — missing principal, store error, capability not held — now append one row to `aura.capability_denials` first. The store-error branch is the one that was previously invisible and the one RBAC-09 cares about most.
- **The denial is readable from the surface that already existed.** `auditActivityQuery` gained a fifth `'capability'` leg projecting cause → action, capability → target, route → detail into the same shape the mcp/skill/tool/share legs use. `GET /api/admin/audit` already serves this feed and the cockpit already reads it, so RBAC-10's read-back cost no route, no handler and no frontend query.
- **The feed no longer reshuffles.** `ORDER BY created_at DESC` gained `source, target` as a deterministic secondary key, so two rows written inside the same timestamp tick come back in a stable order across refreshes.
- **The ledger is live, not latent.** `AuthDeps.DenialRecorder` is now assigned at the composition root. This was not in the plan — see Deviations.

## Task Commits

1. **Task 1: A denial ledger, written at the one gate every denial passes through** — `9bb200db6` (test, RED) → `2dc90be4e` (feat, GREEN)
2. **Task 2: The admin reads denials back through the feed that already exists** — `26863393e` (test, RED) → `8e9ae1a43` (feat, GREEN)
3. **Out-of-plan gap closure: composition-root wiring** — `c22e7719c` (fix)

## Files Created/Modified

- `internal/db/migrations/0123_capability_denials.{up,down}.sql` — the table: six columns, a `CHECK` closing the cause vocabulary, RLS and the `aura_app` grant in the shape migration 0100 established
- `internal/db/queries/capability_denials.sql` + `internal/db/sqlc/capability_denials.sql.go` — the single insert, sqlc-generated
- `internal/agui/capability_denial_store.go` — `PgCapabilityDenialStore`, the only writer; RLS-scoped via `db.WithIdentityTx` to the row's own identity
- `internal/agui/auth.go` — the `capabilityDenialRecorder` port on `AuthDeps`, `recordDenial`, and `denialRoute` (matched pattern, not raw path)
- `internal/agui/audit_store.go` — the fifth UNION leg, the `Source` doc comment, the secondary ordering key
- `cmd/aura/serve_auth.go` — `withDenialRecorder`, the composition-root wiring and its nil-pool guard

## Decisions Made

See `key-decisions` in the frontmatter. The two that most shape the code:

- **The route is the matched pattern.** `r.Pattern` (Go 1.22+), with a bounded `r.Method` fallback that only fires when `RequireCapability` is invoked directly off a mux in this package's own tests. In production every mount goes through `mux.Handle`, so a thousand denials against a thousand identity ids are one route in the feed rather than a thousand rows of distinct attacker-influenced text.
- **The nil check lives at the composition root, not in the constructor.** A constructor cannot express "no recorder" through a typed pointer: a `*PgCapabilityDenialStore` over a nil pool, assigned to the interface field, is a *non-nil* interface value. It would slip past `RequireCapability`'s own nil guard and panic on every refusal instead of returning 403.

## Deviations from Plan

### 1. [Rule 2 — Missing critical] The recorder was never wired at the composition root

- **Found during:** plan closure, verifying the plan's acceptance criteria rather than only its tests
- **Issue:** neither task assigned `AuthDeps.DenialRecorder`. `NewPgCapabilityDenialStore` had zero callers outside tests, and a grep across every remaining plan in the phase found no later plan that owned the wiring — an orphan gap, not a deferral. The table, the migration, the recorder and the audit leg all existed and all their tests passed, while in a running daemon every real denial took the nil-recorder no-op path and recorded nothing. RBAC-10's "readable back out of the audit trail" held only in the tests that injected a recorder themselves.
- **Fix:** `withDenialRecorder(deps, chat.pool)` in `buildAuthDeps`, with the nil-pool guard described above, plus two tests covering both branches.
- **Verification:** RED proven by neutralising the wiring and watching `TestWithDenialRecorderWiresTheLedger` fail, then restoring it byte-identical. `go vet ./...`, `go build ./...` and `check-file-size.sh` clean repo-wide; `./cmd/aura` green under `-race`.
- **Committed in:** `c22e7719c`

### 2. [Process] Task 1's RED commit could not be a compile-failure

- **Found during:** Task 1
- **Issue:** the genuine RED for Task 1 was a missing-symbol compile failure (`deps.DenialRecorder undefined`). This repo's pre-commit hook runs `go vet`/lint on every commit and fails closed on a non-building package, and `--no-verify` is forbidden by CLAUDE.md — so a "test files only, does not compile" RED is structurally uncommittable here. Measured by attempting the commit and reading the hook's own vet failure, not assumed.
- **Fix:** the same deliberately-wrong-scaffold pattern plans 02-01 and 02-03 already established under this gate — the full implementation ships alongside the tests with exactly ONE seeded defect (`denialRoute` returning `r.URL.Path`), so the package builds and the named target test fails on a real assertion rather than a build error. 8 of 9 new tests passed at the RED commit; the one failure was the target.
- **Committed in:** `9bb200db6`

---

**Total deviations:** 2 — one missing-critical gap closed, one process constraint measured and worked within.
**Impact on plan:** the gap closure is what makes the plan's own deliverable function; no scope creep beyond it.

## Issues Encountered

Two stale assertions elsewhere in the repo were red on a live run and were fixed on touch during closure. Neither belongs to this plan's files and each was committed separately:

- `internal/db` still asserted a literal `'*'` capability row survives at migrate-to-head, which migration 0121 (plan 02-01) retired. Only the HEAD assertion was actually wrong — the two inside the ±1 straddle are correct, because stepping below 0121 runs its down migration and synthesizes a wildcard back. `db_test.go` was also split at the 600-LOC cap. Commit `c81dffdde`; whole `internal/db` `db_integration` tier then green live under `-race`: 96 pass, 0 fail, 0 skip.
- `TestProvisionSagaLive`'s happy path required exactly one capability grant row; plan 02-02 made provisioning land the whole uniform user set, so a correct run writes four. Bound to `len(identity.UserSet())` rather than swapped for the literal 4. Commit `7dea44374`.

## User Setup Required

None — no external service configuration. The migration lands with `make db-migrate`; the recorder needs no env var.

## Next Phase Readiness

- **Ready:** `aura.capability_denials` is the surface plan 02-10's live run reads to prove RBAC-10 ("B attempts to create an identity and to remove one, and both are refused and readable afterwards out of the audit trail"). The wiring gap that would have made that run fail is closed.
- **Open, and owned by 02-10:** no test drives the full path — a real HTTP request against a live `aura serve`, a row in the real table, the row back out of the real admin endpoint. Every test here injects its own recorder or queries the store directly. `D5` in the coverage block carries `human_judgment: true` for exactly this reason.
- **Verification evidence:** `internal/agui` untagged plus the whole `db_integration` tier under `-race` on a disposable Postgres container — **777 pass, 0 fail, 0 skip**. All 16 of this plan's own tests are in that count.

---
*Phase: 02-two-roles-and-a-budget*
*Completed: 2026-09-09*
