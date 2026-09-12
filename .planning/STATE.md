---
gsd_state_version: "1.0"
milestone: v1.1.0
milestone_name: Production Launch — Multi-Tenant
current_phase: 02
current_phase_name: Two Roles and a Budget
status: complete
stopped_at: Completed 02-10-PLAN.md
last_updated: "2026-09-12T15:10:00.000Z"
last_activity: 2026-09-12
last_activity_desc: Phase 02 closed by the live two-role witness, which found and fixed an Authula orphan
state_head: 0ea2656c30d0dd1dcaf48991e8842267df1acd01
progress:
  total_phases: 7
  completed_phases: 2
  total_plans: 17
  completed_plans: 17
  percent: 29
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-09-07)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 02 — Two Roles and a Budget

## Current Position

Phase: 02 (Two Roles and a Budget) — COMPLETE
Plan: 10 of 10
Status: Phase 02 closed 2026-09-12 by its live two-role witness
Last activity: 2026-09-12 — the closing run measured the reverse saga plane by plane, found an
Authula account outliving every other teardown, and the fix landed with it
(.planning/phases/02-two-roles-and-a-budget/02-LIVE-RUN-EVIDENCE.md)

Progress: [###░░░░░░░] 29% (2 of 7 phases; phase 01 closed 2026-09-08 with its own live run
evidence, phase 02 today)

## Milestone Shape

Seven phases, strictly sequential. Every phase closes on a real end-to-end run against the
live stack, driven by the real agent — never on unit evidence (E2E-05). Few substantial
phases, not many thin ones.

| Phase | Closes on |
|-------|-----------|
| 1. Two Identities, Live and Separated | `musr_e2e` against a live `aura serve` + two concurrent scored conversations |
| 2. Permissions Decide What a User May Do | Live permission matrix — five call sites × two identities, denials read back from the audit trail |
| 3. The Boundary Under Attack | Adversarial suite + sandbox escape battery against the live two-identity stack |
| 4. Load, Chaos and Truthful Degradation | `make load-chaos` + `make observability-evidence` with two identities active |
| 5. Restart, Rollback, Restore | `make restore-drill` + `rollback_rehearsal.py`, bracketed by real turns per identity |
| 6. A Stranger Can Install and Operate It | Clean-machine walkthrough driven only by the written docs |
| 7. One SHA, Twelve Reports, One Window | Twelve reports on a frozen tree in one <24h window, then the release workflows |

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: —
- Total execution time: —

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 01 P07 | 7 min | 2 tasks | 2 files |
| Phase 01 P01 | 155min | 3 tasks | 14 files |
| Phase 01 P03 | ~2h | 3 tasks | 7 files |
| Phase 01 P05 | 2h20min | 3 tasks | 3 files |
| Phase 01-two-identities-live-and-separated P04 | ~55 min | 3 tasks | 7 files |
| Phase 01 P06 | ~4h | 3 tasks | 17 files |
| Phase 02 P01 | 44min | 3 tasks | 29 files |
| Phase 02 P02 | n/a (continuation) | 2 tasks | 16 files |
| Phase 02 P03 | not measured | 3 tasks | 6 files |
| Phase 02 P04 | not measured | 2 tasks | 16 files |
| Phase 02 P05 | 30min + closure | 3 tasks | 13 files |
| Phase 02 P06 | ~54min | 3 tasks | 12 files |
| Phase 02 P07 | ~88min | 3 tasks | 15 files |
| Phase 02 P09 | ~1h35m | 3 tasks | 19 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Decisions taken during roadmap
creation:

- **Roadmap**: Phase numbering restarts at 1. The previous milestone's phase directories were
  deleted and its numbering (45–54) is not carried forward.
- **Roadmap**: The twelve release reports are deliberately NOT one phase. Eight have never
  been run and are expected to break; fixing code inside the 24-hour window would reset the
  candidate SHA and void the bundle. Phases 2–5 each execute the reports their own work
  touches and close what they break; phase 7 re-runs everything on a frozen tree.
- **Roadmap**: RBAC is wiring, not a build — `github.com/Authula/authula@v1.43.0` already
  ships `plugins/access-control/` with handlers, repositories, role hierarchy and its own
  migration set. Aura imports Authula today only for jwt/totp/session/email-password/csrf/
  rate-limit.
- **Roadmap**: No UI phase. Cockpit UI for role management is explicitly out of scope; the
  API is the contract for this milestone.
- [Phase 01]: TOTP enrollment is headlessly automatable: /totp/enable's otpauth:// URI carries the plaintext base32 secret as a query parameter, and /totp/verify checks a code against the same decrypted secret — Confirmed by reading enable_usecase.go, verify_totp_usecase.go and totp_service.go directly (github.com/Authula/authula v1.43.0), not inferred from the go doc summary alone
- [Phase 01]: First-login TOTP enrollment enforcement is not wired today — EnforceFirstLogin only sets Authula user-metadata markers; the login-time redirect code is unbuilt — cmd/aura/serve_onboarding.go's own comment states this is 'wired at the cutover (plan 12)'; a repo-wide grep found no other reference, confirmed absent rather than assumed absent
- [Phase 01]: [Phase 01 Plan 01] Local verification of a musr_e2e-tagged test used a disposable Postgres container, never the live aura database - internal/dbtest.MigrateURL (commit 0fa214648) fails closed on any db_integration DSN named aura outside CI, a pre-existing repo-wide safety net this plan honored rather than bypassed. — Confirmed by reading internal/dbtest/live_target_guard.go directly; the guard applies to all thirty existing db_integration call sites, so this is standing project behavior, not something introduced or worked around here.
- [Phase 01]: [Phase 01 Plan 03] The memory plane's D-11 item 3 (a verified access token reaching arcadedb-mcp's tenant selector) needed a JWKS-serving seam on LiveMCPTokenIssuer that did not exist: no exported mechanism let a self-minted live-test Authula token be verified without the full aura daemon serving its JWKS route. Added LiveMCPTokenIssuer.JWKSHandler mirroring the existing production OAuthServer.JWKSHandler over the same cache service.
- [Phase 01]: [Phase 01 Plan 03] musr-e2e's bring-up step called `make db-migrate memory-up`, not `memory-up-core`, before this plan -- pre-existing drift between the step's own comment (postgres+ArcadeDB+embed sidecar) and what it actually started (also arcadedb-mcp, and therefore the aura daemon, racing the tagged tier's own Postgres writes -- the measured CI #1809 shape). Fixed on touch to memory-up-core.
- [Phase 01]: [Phase 01 Plan 05] Budget is per-turn by construction (runner.buildAgent's fresh agent.NewBudget), confirmed unchanged by the same-day 10:43 CEST paid-completion-critic removal refactor; the other three D-15 surfaces (sidecar path, gateway ReservationKey, steer inbox) are disjoint only by conversation-UUID uniqueness, proven by deliberately breaking each and watching the corresponding assertion go red.
- [Phase 01-two-identities-live-and-separated]: [Phase 01 Plan 04] Kept ci.yml's compose postgres+ArcadeDB+embed bring-up step (adding garage) instead of removing it per a literal reading of the plan: production_load_chaos.py and restore_drill.sh run later in the same job, target the compose postgres service directly, and make musr-e2e never touches it (its own disposable Postgres is separate). — Measured by reading scripts/production_load_chaos_support.py and scripts/restore_drill.sh before editing ci.yml; removing the bring-up would have broken those two later steps.
- [Phase 01]: internal/webauth/authula.go was missing session.auth RouteMappings wiring for /totp/enable and siblings — a real production bug (broke cockpit TOTP self-service too), fixed with authulaconfig.WithRouteMappings, not just worked around in the harness
- [Phase 01]: Forced password-change (D-15) has no headless, plan-compliant path in this build (no mailer plugin, no completed-Telegram-link path this run will fake, no admin plugin) — recorded as a limitation, not worked around or narrowed out of E2E-02
- [Phase 02]: Retired the capability_grants wildcard (migration 0121) and added a per-identity encrypted OpenRouter key store (migration 0122), proven on one live acceptance test (TestTwoRolesTracer). — Checkpoint-approved both one-way migrations as written; RBAC-01/02/04/08/09 and CRED-01/07 requirements now have code + tests, though the four live db_integration/musr_e2e tests could not be executed in this sandboxed session (no .env access) and need operator confirmation.
- [Phase 02]: TestNoEscalation rewritten (not left red): D-01/RBAC-03 retires the pre-Phase-2 subset-of-creator-grants contract it pinned; every no-write assertion kept, administrative-refusal + uniform-grant coverage added.
- [Phase 02]: TDD RED->GREEN ordering not honored for Task 2 (predecessor crashed after implementation, before tests); every new refusal assertion independently verified by temporarily removing the guard and confirming the test failed, then restoring byte-identical.
- [Phase 02]: openrouterprovision: USDCap decimal-safe money type (cents-based, fixed 2-decimal JSON, half-up rounding at admin input) replaces float64/%v for the OpenRouter spending cap
- [Phase 02]: openrouterprovision: RevokeKey is DELETE+verifying-GET in one function (CRED-08) — a caller cannot skip the verification half; a DELETE that itself 404s still converges to success provided the follow-up GET also 404s
- [Phase 02]: [Plan 02-04] The capability-denial ledger was built, tested and green while `AuthDeps.DenialRecorder` was never assigned at the composition root — `NewPgCapabilityDenialStore` had zero callers outside tests, so every real denial took the nil no-op path and recorded nothing. No later plan owned the wiring; closed on touch in `c22e7719c`. Found by checking the plan's acceptance criteria, not its tests: all sixteen tests passed throughout.
- [Phase 02]: [Plan 02-04] The recorder's nil check belongs at the composition root, not in the constructor: a `*PgCapabilityDenialStore` over a nil pool assigned to the interface field is a NON-nil interface value that slips past `RequireCapability`'s own nil guard and panics on every refusal instead of returning 403.
- [Phase 02]: [Plan 02-07] Operator approved `numeric(24, 12)` for `cache_metrics.cost_usd` and `conversations.total_cost_usd` (migration 0124) — not the plan's recommended `(20, 10)`. One-way: the down direction rounds every sub-`0.0001` value to zero and says so in the file.
- [Phase 02]: [Plan 02-07] The checkpoint's own premise "no code change needed for a wider column" was FALSE: `internal/pgnumeric.NumericFromFloat` hardcoded scale 4 (`f * 1e4`), so the widened column alone would still have written zero. A schema widening is not complete until the encoder that feeds it is checked too.
- [Phase 02]: [Plan 02-07] The plan's "micro-dollars, exact by construction" redirect was wrong at this magnitude: `0.000004158 USD` is `4.158` micro-dollars, so integer micro-dollars would round with a 3.8% per-call error. Nano-dollars would have been needed.
- [Phase 02]: [Plan 02-07] OPEN, not a defect but worth a decision: `GET .../credit` collapses spend to integer CENTS (`USDCap` is `int64` cents), so the figure a human reads stays `0.00` until roughly 2400 calls at the measured per-call cost accumulate. The storage is now exact and only the presentation rounds, so this is changeable without a migration — but the phase paid a one-way migration for precision its only reader discards. `PeriodSpend` feeds display ONLY; it does not feed CRED-05's refusal, which reads the stored cap.
- [Phase 02]: [Plan 02-06] The dark-code chain this phase repeated four times is broken: `identitykey.Store.Save` and `internal/openrouterprovision.{MintKey,RevokeKey}` now have real production callers in `cmd/aura/serve_provisioning_openrouter.go`, reachable from `serve_onboarding.go:278` (mint) and `serve_provisioning.go:389` (revoke). Verified by following the boot chain, not by reading the executor's report.
- [Phase 02]: [Plan 02-06] `Deps.IdentityLLM` is now SAFE to switch on — an identity provisioned through the saga holds a decryptable key. The one-line wiring in `assembleChatEnv` is still deliberately not done; it needs the typed-nil guard (`buildIdentityLLMResolver` returns a typed pointer, and a nil one assigned to an interface field is a NON-nil interface value).
- [Phase 02]: [Plan 02-06] The executor reported a "spurious race" in a combined six-package `-race` run. Not reproduced: two clean runs, exit 0, zero DATA RACE, plus repo-wide vet/build/file-size clean. Treated as transient, not as a standing concern.
- [Phase 02]: [Plan 02-05] CRED-05 cannot be switched on before plan 02-06: `identitykey.Store.Save` and `internal/openrouterprovision` both have ZERO production callers, so no identity holds a key and a fail-closed interactive runner would refuse every turn including the operator's. The seam is committed but inert (`da24cbf82`, `Deps.IdentityLLM` nil everywhere) — an ordering constraint neither plan states.
- [Phase 02]: [Plan 02-05] The interactive turn's one correct seam is `turnLocked`, not the HTTP layer: it resolves ONE snapshot after `scopeContextToConversation` has put the conversation owner on ctx and seeds it via `withLLMRuntimeSnapshot`, so `buildAgent`, the title worker and the tracker all inherit that decision. No other call site needs changing.
- [Phase 02]: [Plan 02-04] A compile-failure RED is structurally uncommittable in this repo — the pre-commit hook runs `go vet` and fails closed on a non-building package, and `--no-verify` is forbidden. Measured by attempting it. Task 1 used the deliberately-wrong-scaffold pattern 02-01/02-03 already established under the same gate.
- [Phase 02]: [Plan 02-09] Four UI-SPEC backstops decided server-side and pinned by tests: cache_hit_rate is request-weighted and blended $/1M re-derived from totals (a rate is never summed across buckets); a zero prior period gives no delta (nil); over-allocation triggers at strict > (equality is not over-allocated); the ranked-list tiebreak is identity id ascending. — The component renders what the server resolved; each rule has a hand-computed test (TestAggregateRateMetricAcrossBuckets, TestDeltaWhenPriorPeriodIsZero, TestSpendOverviewOverAllocationBoundary, TestSpendOverviewTiebreakIsStable).
- [Phase 02]: [Plan 02-09] OPEN for the operator: GET /api/admin/spend/overview is gated on governance.write (the 02-07 credit-route precedent), which every member holds under D-01, so a member can read every identity's name and lifetime spend plus the account-wide available credit pool. Gating it on an administrative capability instead is a one-line mount change. — The plan asked for a deliberate choice and allowed an administrative gate. The pool figure is not otherwise visible to a member, so the mount comment's "only aggregation of what an identity could infer" argument does not cover it.
- [Phase 02]: [Plan 02-09] gsd-executor subagents die on the 600s watchdog when they run the full web suite (npm run test = vitest run --coverage): four times across 02-08 and 02-09. The orchestrator runs the full suite itself; executors run targeted vitest paths only. — Measured: the suite completes in 129s from the main session and does not hang; telling the executor to background it did not prevent the stall. Recorded in aura-memory.

### Pending Todos

See `.planning/todos/`.

### Blockers/Concerns

Carried in from `.planning/codebase/CONCERNS.md` (measured 2026-09-07) — these are the
findings the roadmap's phases are expected to collide with, named so a phase does not
rediscover them:

- **Phase 3 will meet these three.** The shared package caches (`aura-uv-cache`,
  `aura-npm-cache`, `aura-pip-cache`) are mounted read-write into *every* identity's sandbox
  — a cross-tenant code-execution channel that survives container destruction
  (`internal/sandbox/usersandbox/translate.go:20-27`). `CapDrop: []string{}` with no
  `SecurityOpt` and no `User`, so the workload is root with Docker's full default caps.
  `internal/skills/installer.go:378` hands `os.Environ()` to `npx skills add`; the fix,
  `secret.InstallerEnv`, already exists at `internal/mcp/process_env.go:68` and was applied
  to the sibling MCP installer but never to skills.
- **Phase 2 will meet this.** `internal/approvalgrants` — the durable standing-approval store
  — has no test file at all (4/57 = 7.0%, the lowest in the repo), and it is exactly the
  package RBAC-07 lands on.
- **Phase 2 will meet this.** `internal/webauth` sits at 187/360 = 51.9%; the uncovered half
  is the rejection paths in session validation, OAuth token verification and identity linking.
- **Phase 5 will meet this.** There is no rotation path for `AURA_ARCADEDB_TENANT_SECRET`:
  passwords are derived by HMAC, ArcadeDB cannot re-scope an existing user, and a rotation is
  an untested `ALTER USER` sweep across all tenants.
- **Before phase 1 starts.** The working tree carries substantial uncommitted production code
  in `internal/arcadedb/` (`memory_graph_temporal.go`, `memory_mentions_read.go`, 18 modified
  tracked files, 12 untracked test files). No gate has run against it. Land or stash it before
  a phase begins — a candidate SHA cannot be frozen over ambient uncommitted state.
- **Standing.** Ten non-test Go files are within 20 lines of the 600-LOC ceiling and
  `internal/askuser/store.go` is exactly at it. Split before editing, not after the gate
  fires.
- Mutation spot-check (go-mutesting ./internal/gateway/, floor 70% killed) not completed locally for plan 01-05 — too slow for one session and the operator directed it to CI; recorded in .planning/WINDOWS.md id 27 and 01-VALIDATION.md. No score exists yet for internal/gateway.
- Plan 02-01: four live-tier tests (TestMigrate0121_RetiresWildcard, TestBootstrapGrantsExplicitSet, TestIdentityLLMKeyRLSAndCascade, TestTwoRolesTracer) compile clean under their tags but were never executed — this session's secret-read guard blocks .env access. Run them (WSL) before treating RBAC-04/RBAC-08/CRED-01 as proven.
- 02-02: db_integration tests in onboarding_provision_grants_test.go (RBAC-03 uniform grant, admin refusal, grant-step idempotency) not run — no live Postgres access in this sandboxed session. Run: go test -tags db_integration -race -count=1 -p 1 ./internal/agui/ -run 'TestProvisionGrantsUniformCapabilitySet|TestProvisionRefusesAdministrativeRequest|TestProvisionGrantStepIsIdempotent' -v

## Deferred Items

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| Isolation | ISO-11 — one process per identity, if execution separation proves insufficient in ISO-05 | Deferred to v2 | 2026-09-07 | v1.1.0 |
| Isolation | ISO-12 — per-identity resource quotas configurable by the operator | Deferred to v2 | 2026-09-07 | v1.1.0 |
| Access control | RBAC-11 — per-resource ownership delegation | Deferred to v2 | 2026-09-07 | v1.1.0 |
| Access control | RBAC-12 — role assignment through the cockpit UI | Deferred to v2 | 2026-09-07 | v1.1.0 |

## Session Continuity

Last session: 2026-09-10T08:41:10.086Z
Stopped at: Completed 02-09-PLAN.md
Resume file: None

Settled this session, each measured live on a disposable Postgres container under `-race`,
never compile-checked:

- `c81dffdde` — `internal/db`'s obsolete `'*'` assertions corrected (only the HEAD one was
  actually wrong; the two inside the ±1 straddle are correct because 0121's down migration
  synthesizes a wildcard back) and `db_test.go` split at the 600-LOC cap. Tier: 96/0/0.
- `7dea44374` — `TestProvisionSagaLive` bound to `len(identity.UserSet())` instead of the
  literal 1 that 02-02's uniform grant set superseded.
- `c22e7719c` — the capability-denial ledger wired at the composition root. Without it the
  whole of 02-04 was latent: green tests, nothing recorded in a running daemon.
- `f01c7fb91` — `02-04-SUMMARY.md`. Tier `internal/agui`: 777 passed, 0 failed, **0 skipped**.

Next: `/gsd-execute-phase 02` for plan 02-08 (cockpit: identity roster, capability
panel, credit panel). Two decisions still open for the operator, neither a task:
switching on `Deps.IdentityLLM`, and whether the credit read should show sub-cent
spend.
